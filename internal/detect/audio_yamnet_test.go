package detect

import (
	"errors"
	"testing"

	"github.com/rvben/vedetta/internal/audio"
)

func TestYAMNetBackend_SlicesScoresTensor(t *testing.T) {
	// YAMNet has three output tensors concatenated by TFLiteBackend:
	// scores [521], embeddings [1024], log_mel_spectrogram [N*64].
	// The wrapper must return only the first 521.
	full := make([]float32, 1609) // 521 + 1024 + 64
	for i := range full {
		full[i] = float32(i)
	}
	inner := &mockBackend{output: full}
	y := &YAMNetBackend{inner: inner}

	got, err := y.Run(make([]float32, audio.WindowSamples))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got) != YAMNetClassCount {
		t.Fatalf("score len: got %d want %d", len(got), YAMNetClassCount)
	}
	for i := 0; i < YAMNetClassCount; i++ {
		if got[i] != float32(i) {
			t.Fatalf("scores[%d]: got %f want %f", i, got[i], float32(i))
			break
		}
	}
}

func TestYAMNetBackend_RejectsShortOutput(t *testing.T) {
	inner := &mockBackend{output: make([]float32, 100)}
	y := &YAMNetBackend{inner: inner}
	if _, err := y.Run(nil); err == nil {
		t.Fatal("expected error on short output")
	}
}

func TestYAMNetBackend_PropagatesInnerError(t *testing.T) {
	inner := &mockBackend{runErr: errors.New("boom")}
	y := &YAMNetBackend{inner: inner}
	if _, err := y.Run(nil); err == nil {
		t.Fatal("expected error from inner backend")
	}
}

func TestYAMNetBackend_CloseDelegates(t *testing.T) {
	inner := &mockBackend{output: make([]float32, YAMNetClassCount)}
	y := &YAMNetBackend{inner: inner}
	y.Close()
	if inner.closed != 1 {
		t.Errorf("inner.Close called %d times, want 1", inner.closed)
	}
}
