package detect

import (
	"math"
	"testing"
)

func TestProcessSSDOutput_BasicDetection(t *testing.T) {
	// Simulate SSD output with 2 detections, max N=10.
	layout := SSDOutputLayout{
		BoxesSize: 40,  // 10 * 4
		ClassSize: 10,  // 10
		ScoreSize: 10,  // 10
		CountSize: 1,
	}

	output := make([]float32, 40+10+10+1)

	// Detection 0: person at center of image, high confidence.
	output[0] = 0.2  // ymin
	output[1] = 0.2  // xmin
	output[2] = 0.8  // ymax
	output[3] = 0.8  // xmax

	// Detection 1: car at bottom-right, lower confidence.
	output[4] = 0.5  // ymin
	output[5] = 0.5  // xmin
	output[6] = 1.0  // ymax
	output[7] = 1.0  // xmax

	// Classes (offset 40): person=0, car=2
	output[40] = 0   // person
	output[41] = 2   // car

	// Scores (offset 50):
	output[50] = 0.9  // person
	output[51] = 0.7  // car

	// Count (offset 60):
	output[60] = 2

	// No letterboxing — direct normalized coords to 640x480 image.
	dets := processSSDOutput(output, layout, 0.5, 640, 480, 0, 0, 0)

	if len(dets) != 2 {
		t.Fatalf("expected 2 detections, got %d", len(dets))
	}

	// Check person detection.
	if dets[0].Label != "person" {
		t.Errorf("expected label 'person', got %q", dets[0].Label)
	}
	if dets[0].Score != 0.9 {
		t.Errorf("expected score 0.9, got %f", dets[0].Score)
	}
	// Box: xmin=0.2*640=128, ymin=0.2*480=96, xmax=0.8*640=512, ymax=0.8*480=384
	expectedBox := [4]int{128, 96, 512, 384}
	if dets[0].Box != expectedBox {
		t.Errorf("expected box %v, got %v", expectedBox, dets[0].Box)
	}

	// Check car detection.
	if dets[1].Label != "car" {
		t.Errorf("expected label 'car', got %q", dets[1].Label)
	}
}

func TestProcessSSDOutput_ScoreFilter(t *testing.T) {
	layout := SSDOutputLayout{
		BoxesSize: 8, ClassSize: 2, ScoreSize: 2, CountSize: 1,
	}

	output := make([]float32, 8+2+2+1)

	// Two detections, one below threshold.
	output[0], output[1], output[2], output[3] = 0.1, 0.1, 0.5, 0.5
	output[4], output[5], output[6], output[7] = 0.6, 0.6, 0.9, 0.9

	output[8] = 0   // person
	output[9] = 0   // person

	output[10] = 0.8 // above threshold
	output[11] = 0.3 // below threshold

	output[12] = 2 // count

	dets := processSSDOutput(output, layout, 0.5, 100, 100, 0, 0, 0)

	if len(dets) != 1 {
		t.Fatalf("expected 1 detection after filtering, got %d", len(dets))
	}
	if dets[0].Score != 0.8 {
		t.Errorf("expected score 0.8, got %f", dets[0].Score)
	}
}

func TestProcessSSDOutput_EmptyCount(t *testing.T) {
	layout := SSDOutputLayout{
		BoxesSize: 8, ClassSize: 2, ScoreSize: 2, CountSize: 1,
	}

	output := make([]float32, 8+2+2+1)
	output[12] = 0 // no detections

	dets := processSSDOutput(output, layout, 0.5, 640, 480, 0, 0, 0)

	if len(dets) != 0 {
		t.Errorf("expected 0 detections, got %d", len(dets))
	}
}

func TestProcessSSDOutput_ClampsCoordinates(t *testing.T) {
	layout := SSDOutputLayout{
		BoxesSize: 4, ClassSize: 1, ScoreSize: 1, CountSize: 1,
	}

	output := make([]float32, 4+1+1+1)

	// Coordinates slightly out of bounds.
	output[0] = -0.1 // ymin
	output[1] = -0.1 // xmin
	output[2] = 1.1  // ymax
	output[3] = 1.1  // xmax

	output[4] = 0   // person
	output[5] = 0.9 // score
	output[6] = 1   // count

	dets := processSSDOutput(output, layout, 0.5, 100, 100, 0, 0, 0)

	if len(dets) != 1 {
		t.Fatalf("expected 1 detection, got %d", len(dets))
	}

	// Clamped to [0,1] then scaled to 100x100.
	if dets[0].Box[0] != 0 || dets[0].Box[1] != 0 {
		t.Errorf("expected clamped min coords to 0, got %v", dets[0].Box)
	}
	if dets[0].Box[2] != 100 || dets[0].Box[3] != 100 {
		t.Errorf("expected clamped max coords to 100, got %v", dets[0].Box)
	}
}

func TestPrepareSSDInput_OutputShape(t *testing.T) {
	srcW, srcH := 1920, 1080
	dstW, dstH := 320, 320
	data := make([]byte, srcW*srcH*3)
	buf := make([]float32, dstW*dstH*3)

	out, scale, padX, padY := prepareSSDInput(buf, data, srcW, srcH, dstW, dstH)

	expectedLen := dstW * dstH * 3
	if len(out) != expectedLen {
		t.Errorf("expected tensor length %d, got %d", expectedLen, len(out))
	}
	if scale <= 0 {
		t.Errorf("expected positive scale, got %f", scale)
	}
	// 1920x1080 → scale = 320/1920 = 0.1667, newW=320, newH=180, padX=0, padY=70
	if padX != 0 {
		t.Errorf("expected padX=0 for wider-than-tall image, got %f", padX)
	}
	if padY <= 0 {
		t.Errorf("expected positive padY for wider-than-tall image, got %f", padY)
	}
}

func TestPrepareSSDInput_HWCLayout(t *testing.T) {
	// Small image to verify HWC pixel ordering.
	srcW, srcH := 2, 2
	dstW, dstH := 2, 2
	data := []byte{
		255, 0, 0, // (0,0) red
		0, 255, 0, // (1,0) green
		0, 0, 255, // (0,1) blue
		128, 128, 128, // (1,1) gray
	}
	buf := make([]float32, dstW*dstH*3)

	out, scale, _, _ := prepareSSDInput(buf, data, srcW, srcH, dstW, dstH)

	// Scale should be 1.0, no padding (2x2 → 2x2).
	if scale != 1.0 {
		t.Errorf("expected scale 1.0, got %f", scale)
	}

	// HWC: pixel (0,0) is at indices 0,1,2 → R=1.0, G=0.0, B=0.0
	if math.Abs(float64(out[0])-1.0) > 0.01 {
		t.Errorf("pixel (0,0) R expected ~1.0, got %f", out[0])
	}
	if math.Abs(float64(out[1])) > 0.01 {
		t.Errorf("pixel (0,0) G expected ~0.0, got %f", out[1])
	}
}

func TestResolveModelType(t *testing.T) {
	tests := []struct {
		configured string
		backend    string
		want       string
	}{
		{"yolo", "auto", "yolo"},
		{"ssd", "go", "ssd"},
		{"", "auto", "yolo"},
		{"", "go", "yolo"},
		{"", "onnxruntime_c", "yolo"},
		{"", "tflite", "ssd"},
		{"", "edgetpu", "ssd"},
		{"auto", "edgetpu", "ssd"},
		{"yolo", "tflite", "yolo"}, // explicit override
		{"unknown", "auto", "yolo"},
	}

	for _, tt := range tests {
		got := resolveModelType(tt.configured, tt.backend)
		if got != tt.want {
			t.Errorf("resolveModelType(%q, %q) = %q, want %q",
				tt.configured, tt.backend, got, tt.want)
		}
	}
}
