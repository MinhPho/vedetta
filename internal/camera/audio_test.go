package camera

import (
	"context"
	"testing"
	"time"

	"github.com/rvben/vedetta/internal/audio"
	"github.com/rvben/vedetta/internal/config"
	"github.com/rvben/vedetta/internal/detect"
	"github.com/rvben/vedetta/internal/media"
)

func camConfig(name string) config.CameraConfig {
	return config.CameraConfig{
		Name:   name,
		Detect: config.DetectStreamConfig{Width: 64, Height: 64, FPS: 5},
	}
}

type fakeAudioDetector struct {
	events []detect.AudioEvent
}

func (f *fakeAudioDetector) Detect(_ string, _ []float32) []detect.AudioEvent {
	return f.events
}

func TestPickAudioDecoder(t *testing.T) {
	tests := []struct {
		codec string
		want  bool // whether a decoder is returned
	}{
		{"PCMU", true},
		{"PCMA", true},
		{"AAC", false}, // unsupported in v1, must return nil
		{"OPUS", false},
		{"", false},
	}
	for _, tt := range tests {
		got := pickAudioDecoder(tt.codec)
		if (got != nil) != tt.want {
			t.Errorf("pickAudioDecoder(%q): got non-nil=%v, want non-nil=%v", tt.codec, got != nil, tt.want)
		}
	}
}

func TestAudioWindowLoop_EmitsEvent(t *testing.T) {
	cam := newTestCamera(camConfig("cam1"), nil)
	events := make(chan Event, 1)
	cam.events = events

	det := &fakeAudioDetector{events: []detect.AudioEvent{
		{Label: "Bark", Score: 0.9},
	}}

	windows := make(chan []float32, 1)
	windows <- make([]float32, audio.WindowSamples)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	go cam.audioWindowLoop(ctx, windows, det)

	select {
	case ev := <-events:
		if ev.Label != "Bark" {
			t.Errorf("Label: got %q, want %q", ev.Label, "Bark")
		}
		if ev.Score != 0.9 {
			t.Errorf("Score: got %f, want %f", ev.Score, 0.9)
		}
		if ev.CameraName != "cam1" {
			t.Errorf("CameraName: got %q, want %q", ev.CameraName, "cam1")
		}
		if ev.ID == "" {
			t.Error("ID: must be non-empty")
		}
		if ev.Timestamp.IsZero() {
			t.Error("Timestamp: must be set")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for audio event")
	}
}

func TestAudioWindowLoop_NoEventsWhenDetectorReturnsNil(t *testing.T) {
	cam := newTestCamera(camConfig("cam1"), nil)
	events := make(chan Event, 1)
	cam.events = events

	det := &fakeAudioDetector{events: nil}

	windows := make(chan []float32, 1)
	windows <- make([]float32, audio.WindowSamples)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	go cam.audioWindowLoop(ctx, windows, det)

	select {
	case ev := <-events:
		t.Fatalf("unexpected event: %+v", ev)
	case <-ctx.Done():
		// expected
	}
}

func TestAudioWindowLoop_StopsOnContextCancel(t *testing.T) {
	cam := newTestCamera(camConfig("cam1"), nil)
	cam.events = make(chan Event, 1)

	det := &fakeAudioDetector{}
	windows := make(chan []float32)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		cam.audioWindowLoop(ctx, windows, det)
		close(done)
	}()
	cancel()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("audioWindowLoop did not return after context cancel")
	}
}

func TestAudioWindowLoop_DropsEventWhenChannelFull(t *testing.T) {
	cam := newTestCamera(camConfig("cam1"), nil)
	// Capacity 0 — sender should fall through default rather than block.
	cam.events = make(chan Event)

	det := &fakeAudioDetector{events: []detect.AudioEvent{{Label: "Bark", Score: 0.9}}}
	windows := make(chan []float32, 1)
	windows <- make([]float32, audio.WindowSamples)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		cam.audioWindowLoop(ctx, windows, det)
		close(done)
	}()

	// If the loop blocks on c.events, the goroutine never sees ctx.Done() and
	// `done` never closes. Force the test to fail in that case.
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("audioWindowLoop blocked on full events channel")
	}
}

// Smoke: ensure NewAudioConsumer + G711Decoder compose without nil deref via
// the same path runAudio takes.
func TestPickAudioDecoder_BuildsConsumer(t *testing.T) {
	dec := pickAudioDecoder("PCMU")
	if dec == nil {
		t.Fatal("PCMU decoder is nil")
	}
	c := media.NewAudioConsumer("cam1", dec)
	if c == nil {
		t.Fatal("NewAudioConsumer returned nil")
	}
	c.Close()
}
