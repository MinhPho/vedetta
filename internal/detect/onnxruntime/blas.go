package onnxruntime

import "sync"

// gemmPool reuses GEMM output buffers to reduce allocation pressure.
var gemmPool = sync.Pool{
	New: func() any {
		return &[]float32{}
	},
}

func getGemmBuffer(size int) []float32 {
	bp := gemmPool.Get().(*[]float32)
	buf := *bp
	if cap(buf) >= size {
		buf = buf[:size]
		for i := range buf {
			buf[i] = 0
		}
	} else {
		buf = make([]float32, size)
	}
	*bp = buf
	return buf
}

func putGemmBuffer(buf []float32) {
	bp := &buf
	gemmPool.Put(bp)
}

// Sgemm performs single-precision general matrix multiplication:
//
//	C = A × B
//
// where A is (m × k), B is (k × n), C is (m × n).
// All matrices are in row-major order.
//
// On macOS, this dispatches to Apple's Accelerate framework (NEON SIMD)
// for large matrices, falling back to pure Go for small ones where CGo
// overhead would dominate.
func Sgemm(a []float32, b []float32, m, n, k int) []float32 {
	return sgemm(a, b, m, n, k)
}

// sgemmThreshold is the minimum total output elements (m*n) below which
// pure Go GEMM is used to avoid CGo call overhead.
const sgemmThreshold = 512

// sgemmPureGo performs matrix multiplication in pure Go with tiled loops.
func sgemmPureGo(a []float32, b []float32, c []float32, m, n, k int) {
	const tileSize = 64

	for ii := 0; ii < m; ii += tileSize {
		iEnd := min(ii+tileSize, m)
		for kk := 0; kk < k; kk += tileSize {
			kEnd := min(kk+tileSize, k)
			for jj := 0; jj < n; jj += tileSize {
				jEnd := min(jj+tileSize, n)
				for i := ii; i < iEnd; i++ {
					for p := kk; p < kEnd; p++ {
						aip := a[i*k+p]
						for j := jj; j < jEnd; j++ {
							c[i*n+j] += aip * b[p*n+j]
						}
					}
				}
			}
		}
	}
}
