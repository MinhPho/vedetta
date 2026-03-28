package detect

import (
	"math"
)

// SSD/EfficientDet-Lite output format:
//
// These models produce 4 output tensors (in TFLite's standard order):
//   [0] detection_boxes:   [1, N, 4]  — normalized [ymin, xmin, ymax, xmax]
//   [1] detection_classes: [1, N]     — class indices (0-based, float32)
//   [2] detection_scores:  [1, N]     — confidence values
//   [3] detection_count:   [1]        — number of valid detections (float32)
//
// NMS is performed inside the model, so no additional suppression is needed.

// SSDOutputLayout holds the sizes of each SSD output tensor, used to split
// the flat concatenated output from TFLiteBackend.Run().
type SSDOutputLayout struct {
	BoxesSize  int // N * 4
	ClassSize  int // N
	ScoreSize  int // N
	CountSize  int // 1
}

// processSSDOutput extracts detections from a flat float32 slice containing
// all 4 SSD output tensors concatenated in order.
//
// Parameters:
//   - output: flat float32 containing boxes + classes + scores + count
//   - layout: sizes of each output tensor segment
//   - scoreThreshold: minimum confidence to keep a detection
//   - inputW, inputH: model input dimensions (for coordinate denormalization)
//   - origW, origH: original image dimensions
//   - scale, padX, padY: letterbox transform parameters (0 if no letterboxing)
func processSSDOutput(
	output []float32,
	layout SSDOutputLayout,
	scoreThreshold float32,
	origW, origH int,
	scale, padX, padY float64,
) []Detection {
	// Validate output size.
	totalExpected := layout.BoxesSize + layout.ClassSize + layout.ScoreSize + layout.CountSize
	if len(output) < totalExpected {
		return nil
	}

	// Split the concatenated output into tensor segments.
	boxes := output[:layout.BoxesSize]
	classes := output[layout.BoxesSize : layout.BoxesSize+layout.ClassSize]
	scores := output[layout.BoxesSize+layout.ClassSize : layout.BoxesSize+layout.ClassSize+layout.ScoreSize]
	countSlice := output[layout.BoxesSize+layout.ClassSize+layout.ScoreSize:]

	// detection_count is a float32 representing the number of valid detections.
	numDetections := int(countSlice[0])
	maxDetections := layout.ClassSize // N
	if numDetections > maxDetections {
		numDetections = maxDetections
	}
	if numDetections <= 0 {
		return nil
	}

	var detections []Detection

	for i := 0; i < numDetections; i++ {
		score := scores[i]
		if score < scoreThreshold {
			continue
		}

		// SSD boxes are [ymin, xmin, ymax, xmax] normalized to [0, 1].
		ymin := float64(boxes[i*4+0])
		xmin := float64(boxes[i*4+1])
		ymax := float64(boxes[i*4+2])
		xmax := float64(boxes[i*4+3])

		// Clamp to [0, 1].
		ymin = math.Max(0, math.Min(1, ymin))
		xmin = math.Max(0, math.Min(1, xmin))
		ymax = math.Max(0, math.Min(1, ymax))
		xmax = math.Max(0, math.Min(1, xmax))

		// Convert from normalized model coordinates to original image coordinates.
		// If letterboxing was applied, we need to account for padding and scale.
		var x1, y1, x2, y2 float64
		if scale > 0 {
			// Model received letterboxed input: denormalize to model input space,
			// then remove padding and rescale to original image.
			modelW := float64(origW) * scale
			modelH := float64(origH) * scale
			inputW := modelW + 2*padX
			inputH := modelH + 2*padY

			x1 = (xmin*inputW - padX) / scale
			y1 = (ymin*inputH - padY) / scale
			x2 = (xmax*inputW - padX) / scale
			y2 = (ymax*inputH - padY) / scale
		} else {
			// No letterboxing — normalized coords map directly to image.
			x1 = xmin * float64(origW)
			y1 = ymin * float64(origH)
			x2 = xmax * float64(origW)
			y2 = ymax * float64(origH)
		}

		// Class index — SSD models use 0-based indexing into COCO labels.
		classIdx := int(classes[i])
		label := "unknown"
		if classIdx >= 0 && classIdx < len(CocoLabels) {
			label = CocoLabels[classIdx]
		}

		detections = append(detections, Detection{
			Label: label,
			Score: score,
			Box:   [4]int{int(x1), int(y1), int(x2), int(y2)},
		})
	}

	return detections
}

// prepareSSDInput converts an RGB24 frame to a float32 tensor in HWC format
// normalized to [0, 1], resized to the target model input size with letterboxing.
//
// SSD/EfficientDet models expect [1, H, W, 3] HWC layout, unlike YOLO's CHW.
// Returns the tensor data and letterbox transform parameters.
func prepareSSDInput(buf []float32, data []byte, srcW, srcH, dstW, dstH int) ([]float32, float64, float64, float64) {
	origW := float64(srcW)
	origH := float64(srcH)

	scale := math.Min(float64(dstW)/origW, float64(dstH)/origH)
	newW := int(origW * scale)
	newH := int(origH * scale)

	padX := (dstW - newW) / 2
	padY := (dstH - newH) / 2

	// Fill with gray (128/255 ≈ 0.5) for letterbox padding.
	for i := range buf {
		buf[i] = 0.5
	}

	// HWC layout: pixel at (y, x) channel c = buf[(y*dstW + x)*3 + c]
	for y := 0; y < newH; y++ {
		srcY := int(float64(y) / scale)
		if srcY >= srcH {
			srcY = srcH - 1
		}
		for x := 0; x < newW; x++ {
			srcX := int(float64(x) / scale)
			if srcX >= srcW {
				srcX = srcW - 1
			}

			srcIdx := (srcY*srcW + srcX) * 3 // RGB24 stride
			dstIdx := ((y+padY)*dstW + (x + padX)) * 3

			buf[dstIdx+0] = float32(data[srcIdx+0]) / 255.0
			buf[dstIdx+1] = float32(data[srcIdx+1]) / 255.0
			buf[dstIdx+2] = float32(data[srcIdx+2]) / 255.0
		}
	}

	return buf, scale, float64(padX), float64(padY)
}

// prepareSSDInputRGBA converts an RGBA image to a float32 HWC tensor for SSD models.
func prepareSSDInputRGBA(buf []float32, pix []byte, imgW, imgH, dstW, dstH int) ([]float32, float64, float64, float64) {
	origW := float64(imgW)
	origH := float64(imgH)

	scale := math.Min(float64(dstW)/origW, float64(dstH)/origH)
	newW := int(origW * scale)
	newH := int(origH * scale)

	padX := (dstW - newW) / 2
	padY := (dstH - newH) / 2

	for i := range buf {
		buf[i] = 0.5
	}

	for y := 0; y < newH; y++ {
		srcY := int(float64(y) / scale)
		if srcY >= imgH {
			srcY = imgH - 1
		}
		for x := 0; x < newW; x++ {
			srcX := int(float64(x) / scale)
			if srcX >= imgW {
				srcX = imgW - 1
			}

			srcIdx := (srcY*imgW + srcX) * 4 // RGBA stride
			dstIdx := ((y+padY)*dstW + (x + padX)) * 3

			buf[dstIdx+0] = float32(pix[srcIdx+0]) / 255.0
			buf[dstIdx+1] = float32(pix[srcIdx+1]) / 255.0
			buf[dstIdx+2] = float32(pix[srcIdx+2]) / 255.0
		}
	}

	return buf, scale, float64(padX), float64(padY)
}
