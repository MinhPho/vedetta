package media

import (
	"image"
	"testing"
)

func TestScaleYCbCr_PreservesAspectRatio(t *testing.T) {
	// 1920x800 source, target 1280x720 → should output 1280x532
	// scale = min(1280/1920, 720/800) = min(0.666, 0.9) = 0.666
	// out_width  = floor(1920 * 0.6666 / 2) * 2 = floor(639.99) * 2 = 639*2 = 1278 → hmm
	// Actually: scale = 1280/1920 = 0.6666...
	// out_width  = floor(1920 * 0.6666 / 2) * 2 = floor(639.99) * 2 = 639*2 = 1278
	// Wait, let me recalculate per spec:
	// scale = min(1280/1920, 720/800) = min(0.6666, 0.9) = 0.6666
	// out_width = floor(1920 * 0.6666 / 2) * 2
	// 1920 * 0.6666... = 1280 exactly → floor(1280/2)*2 = 640*2 = 1280
	// out_height = floor(800 * 0.6666 / 2) * 2
	// 800 * 0.6666... = 533.33 → floor(533.33/2)*2 = floor(266.66)*2 = 266*2 = 532
	src := image.NewYCbCr(image.Rect(0, 0, 1920, 800), image.YCbCrSubsampleRatio420)
	got := scaleYCbCr(src, 1280, 720)
	if got.Rect.Dx() != 1280 {
		t.Errorf("width = %d, want 1280", got.Rect.Dx())
	}
	if got.Rect.Dy() != 532 {
		t.Errorf("height = %d, want 532", got.Rect.Dy())
	}
}

func TestScaleYCbCr_EvenDimensions(t *testing.T) {
	// Output must always have even width and height (H264 requirement)
	src := image.NewYCbCr(image.Rect(0, 0, 1921, 1081), image.YCbCrSubsampleRatio420)
	got := scaleYCbCr(src, 1280, 720)
	if got.Rect.Dx()%2 != 0 {
		t.Errorf("width %d is not even", got.Rect.Dx())
	}
	if got.Rect.Dy()%2 != 0 {
		t.Errorf("height %d is not even", got.Rect.Dy())
	}
}

func TestScaleYCbCr_SameSize(t *testing.T) {
	src := image.NewYCbCr(image.Rect(0, 0, 1280, 720), image.YCbCrSubsampleRatio420)
	got := scaleYCbCr(src, 1280, 720)
	if got.Rect.Dx() != 1280 || got.Rect.Dy() != 720 {
		t.Errorf("got %dx%d, want 1280x720", got.Rect.Dx(), got.Rect.Dy())
	}
}
