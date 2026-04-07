package media

import (
	"image"
)

// scaleYCbCr scales a YCbCr I420 image to fit within (targetW, targetH) while
// preserving aspect ratio. Output dimensions are always even (required by H264).
// Uses nearest-neighbour sampling — sufficient for downscaling security footage.
func scaleYCbCr(src *image.YCbCr, targetW, targetH int) *image.YCbCr {
	srcW := src.Rect.Dx()
	srcH := src.Rect.Dy()

	// Compute scale to fit within target box, preserve aspect ratio
	scaleW := float64(targetW) / float64(srcW)
	scaleH := float64(targetH) / float64(srcH)
	scale := scaleW
	if scaleH < scaleW {
		scale = scaleH
	}

	outW := int(float64(srcW)*scale/2) * 2 // round down to even
	outH := int(float64(srcH)*scale/2) * 2

	if outW <= 0 {
		outW = 2
	}
	if outH <= 0 {
		outH = 2
	}

	dst := image.NewYCbCr(image.Rect(0, 0, outW, outH), image.YCbCrSubsampleRatio420)

	for dy := range outH {
		sy := dy * srcH / outH
		for dx := range outW {
			sx := dx * srcW / outW
			dst.Y[dy*dst.YStride+dx] = src.Y[sy*src.YStride+sx]
		}
	}
	// Chroma planes (half resolution for I420)
	for dy := range outH / 2 {
		sy := dy * (srcH / 2) / (outH / 2)
		for dx := range outW / 2 {
			sx := dx * (srcW / 2) / (outW / 2)
			dst.Cb[dy*dst.CStride+dx] = src.Cb[sy*src.CStride+sx]
			dst.Cr[dy*dst.CStride+dx] = src.Cr[sy*src.CStride+sx]
		}
	}

	return dst
}
