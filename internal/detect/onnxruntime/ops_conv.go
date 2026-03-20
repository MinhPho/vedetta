package onnxruntime

import (
	"fmt"
	"math"
	"sync"
)

func init() {
	Register("Conv", opConv)
}

// colPool reuses im2col buffers to reduce allocation pressure.
var colPool = sync.Pool{
	New: func() any {
		return &[]float32{}
	},
}

func getColBuffer(size int) []float32 {
	bp := colPool.Get().(*[]float32)
	buf := *bp
	if cap(buf) >= size {
		buf = buf[:size]
		// No need to zero — im2col writes every element (including zeros for padding)
	} else {
		buf = make([]float32, size)
	}
	*bp = buf
	return buf
}

func putColBuffer(buf []float32) {
	bp := &buf
	colPool.Put(bp)
}

func opConv(inputs []*Tensor, attrs *Attributes) ([]*Tensor, error) {
	if len(inputs) < 2 {
		return nil, fmt.Errorf("Conv: need at least 2 inputs (X, W), got %d", len(inputs))
	}

	x := inputs[0] // [N, C, H, W]
	w := inputs[1] // [M, C/group, kH, kW]

	if x.Dims() != 4 || w.Dims() != 4 {
		return nil, fmt.Errorf("Conv: expected 4D inputs, got X=%dD W=%dD", x.Dims(), w.Dims())
	}

	var bias []float32
	if len(inputs) > 2 && inputs[2] != nil {
		bias = inputs[2].Data
	}

	N := int(x.Shape[0])
	C := int(x.Shape[1])
	H := int(x.Shape[2])
	W := int(x.Shape[3])

	M := int(w.Shape[0])
	kH := int(w.Shape[2])
	kW := int(w.Shape[3])

	group := int(attrs.GetInt("group", 1))

	kernelShape := attrs.GetIntList("kernel_shape")
	if kernelShape != nil {
		kH = int(kernelShape[0])
		kW = int(kernelShape[1])
	}

	strides := attrs.GetIntList("strides")
	strideH, strideW := 1, 1
	if len(strides) >= 2 {
		strideH = int(strides[0])
		strideW = int(strides[1])
	}

	dilations := attrs.GetIntList("dilations")
	dilH, dilW := 1, 1
	if len(dilations) >= 2 {
		dilH = int(dilations[0])
		dilW = int(dilations[1])
	}

	pads := attrs.GetIntList("pads")
	padTop, padLeft, padBottom, padRight := 0, 0, 0, 0
	if len(pads) >= 4 {
		padTop = int(pads[0])
		padLeft = int(pads[1])
		padBottom = int(pads[2])
		padRight = int(pads[3])
	}

	// Handle auto_pad attribute
	autoPad := attrs.GetString("auto_pad", "NOTSET")
	if autoPad == "SAME_UPPER" || autoPad == "SAME_LOWER" {
		outH := int(math.Ceil(float64(H) / float64(strideH)))
		outW := int(math.Ceil(float64(W) / float64(strideW)))
		totalPadH := (outH-1)*strideH + (kH-1)*dilH + 1 - H
		totalPadW := (outW-1)*strideW + (kW-1)*dilW + 1 - W
		if totalPadH < 0 {
			totalPadH = 0
		}
		if totalPadW < 0 {
			totalPadW = 0
		}
		if autoPad == "SAME_UPPER" {
			padTop = totalPadH / 2
			padBottom = totalPadH - padTop
			padLeft = totalPadW / 2
			padRight = totalPadW - padLeft
		} else {
			padBottom = totalPadH / 2
			padTop = totalPadH - padBottom
			padRight = totalPadW / 2
			padLeft = totalPadW - padRight
		}
	}

	effKH := (kH-1)*dilH + 1
	effKW := (kW-1)*dilW + 1
	outH := (H + padTop + padBottom - effKH) / strideH + 1
	outW := (W + padLeft + padRight - effKW) / strideW + 1

	if outH <= 0 || outW <= 0 {
		return nil, fmt.Errorf("Conv: invalid output dimensions %dx%d", outH, outW)
	}

	cPerGroup := C / group
	mPerGroup := M / group
	colSize := cPerGroup * kH * kW
	outSpatial := outH * outW

	output := newTensorUninit([]int64{int64(N), int64(M), int64(outH), int64(outW)})

	col := getColBuffer(colSize * outSpatial)
	defer putColBuffer(col)

	noPad := padTop == 0 && padLeft == 0 && padBottom == 0 && padRight == 0
	noDil := dilH == 1 && dilW == 1

	for n := range N {
		for g := range group {
			// im2col for this batch and group
			if noPad && noDil {
				im2colNoPadNoDil(
					x.Data, col,
					n, g*cPerGroup, cPerGroup,
					H, W, kH, kW,
					strideH, strideW, outH, outW,
					C,
				)
			} else {
				im2col(
					x.Data, col,
					n, g*cPerGroup, cPerGroup,
					H, W, kH, kW,
					strideH, strideW, padTop, padLeft,
					dilH, dilW, outH, outW,
					C,
				)
			}

			// Extract weight slice for this group: [mPerGroup, cPerGroup*kH*kW]
			wOffset := g * mPerGroup * colSize
			wSlice := w.Data[wOffset : wOffset+mPerGroup*colSize]

			// GEMM: [mPerGroup, colSize] x [colSize, outSpatial] = [mPerGroup, outSpatial]
			result := Sgemm(wSlice, col, mPerGroup, outSpatial, colSize)

			// Copy result into output tensor
			for m := range mPerGroup {
				outChannel := g*mPerGroup + m
				dstBase := ((n*M + outChannel) * outH) * outW
				srcBase := m * outSpatial
				copy(output.Data[dstBase:dstBase+outSpatial], result[srcBase:srcBase+outSpatial])
			}
			putGemmBuffer(result)
		}
	}

	// Add bias
	if bias != nil {
		for n := range N {
			for m := range M {
				base := ((n*M + m) * outH) * outW
				b := bias[m]
				for i := range outSpatial {
					output.Data[base+i] += b
				}
			}
		}
	}

	return []*Tensor{output}, nil
}

// im2colNoPadNoDil is an optimized version for the common case of no padding and no dilation.
// Eliminates bounds checking in the inner loop.
func im2colNoPadNoDil(
	input, col []float32,
	n, cStart, cCount int,
	H, W, kH, kW int,
	strideH, strideW, outH, outW int,
	totalC int,
) {
	colIdx := 0
	for c := range cCount {
		ch := cStart + c
		inputBase := (n*totalC + ch) * H * W
		for kh := range kH {
			for kw := range kW {
				for oh := range outH {
					ih := oh*strideH + kh
					rowBase := inputBase + ih*W
					for ow := range outW {
						col[colIdx] = input[rowBase+ow*strideW+kw]
						colIdx++
					}
				}
			}
		}
	}
}

// im2col converts input patches into columns for GEMM-based convolution.
// Splits the output spatial dimensions into interior (no bounds checks) and border regions.
func im2col(
	input, col []float32,
	n, cStart, cCount int,
	H, W, kH, kW int,
	strideH, strideW, padTop, padLeft int,
	dilH, dilW, outH, outW int,
	totalC int,
) {
	colIdx := 0
	for c := range cCount {
		ch := cStart + c
		inputBase := (n*totalC + ch) * H * W
		for kh := range kH {
			for kw := range kW {
				// Precompute the safe range of oh where ih is in bounds
				// ih = oh*strideH - padTop + kh*dilH
				// 0 <= ih < H  =>  (padTop - kh*dilH) / strideH <= oh < (H + padTop - kh*dilH) / strideH
				khOffset := kh*dilH - padTop
				kwOffset := kw*dilW - padLeft

				ohStart := 0
				if khOffset < 0 {
					ohStart = (-khOffset + strideH - 1) / strideH
				}
				ohEnd := outH
				if H-khOffset < outH*strideH {
					ohEnd = (H - khOffset + strideH - 1) / strideH
					if ohEnd > outH {
						ohEnd = outH
					}
				}
				if ohEnd < ohStart {
					ohEnd = ohStart
				}

				owStart := 0
				if kwOffset < 0 {
					owStart = (-kwOffset + strideW - 1) / strideW
				}
				owEnd := outW
				if W-kwOffset < outW*strideW {
					owEnd = (W - kwOffset + strideW - 1) / strideW
					if owEnd > outW {
						owEnd = outW
					}
				}
				if owEnd < owStart {
					owEnd = owStart
				}

				// Top border rows (ih < 0)
				for range ohStart {
					for range outW {
						col[colIdx] = 0
						colIdx++
					}
				}

				// Interior rows
				interiorW := owEnd - owStart
				if strideW == 1 && dilW == 1 {
					// Contiguous copy fast path
					for oh := ohStart; oh < ohEnd; oh++ {
						ih := oh*strideH + khOffset
						srcStart := inputBase + ih*W + owStart + kwOffset

						// Left border zeros
						for range owStart {
							col[colIdx] = 0
							colIdx++
						}

						// Contiguous copy
						copy(col[colIdx:colIdx+interiorW], input[srcStart:srcStart+interiorW])
						colIdx += interiorW

						// Right border zeros
						for range outW - owEnd {
							col[colIdx] = 0
							colIdx++
						}
					}
				} else {
					for oh := ohStart; oh < ohEnd; oh++ {
						ih := oh*strideH + khOffset
						rowBase := inputBase + ih*W

						// Left border
						for range owStart {
							col[colIdx] = 0
							colIdx++
						}

						// Interior (no bounds checks)
						for ow := owStart; ow < owEnd; ow++ {
							iw := ow*strideW + kwOffset
							col[colIdx] = input[rowBase+iw]
							colIdx++
						}

						// Right border
						for range outW - owEnd {
							col[colIdx] = 0
							colIdx++
						}
					}
				}

				// Bottom border rows (ih >= H)
				for range outH - ohEnd {
					for range outW {
						col[colIdx] = 0
						colIdx++
					}
				}
			}
		}
	}
}
