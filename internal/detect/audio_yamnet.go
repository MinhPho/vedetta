package detect

import (
	"fmt"
)

// YAMNetClassCount is the size of the YAMNet/AudioSet ontology — and the
// number of scores YAMNet emits per inference window.
const YAMNetClassCount = 521

// YAMNetBackend wraps a generic Backend (typically *TFLiteBackend) running the
// YAMNet model and exposes only the scores tensor. YAMNet's TFLite export has
// three output tensors concatenated by Backend.Run — scores [521], embeddings
// [1024], log-mel spectrogram. v1 sound recognition cares only about scores.
type YAMNetBackend struct {
	inner Backend
}

// NewYAMNetBackend loads the YAMNet TFLite model from modelPath. CPU only —
// EdgeTPU YAMNet requires a custom recompile and saves only ~15 ms, not worth
// the build complexity for v1.
func NewYAMNetBackend(modelPath string) (*YAMNetBackend, error) {
	inner, err := NewTFLiteBackend(modelPath, false)
	if err != nil {
		return nil, fmt.Errorf("yamnet: %w", err)
	}
	return &YAMNetBackend{inner: inner}, nil
}

// Run returns the per-class score vector (length YAMNetClassCount) for the
// given 0.96 s audio window. The returned slice is a fresh copy and safe to
// retain across calls.
func (y *YAMNetBackend) Run(window []float32) ([]float32, error) {
	out, err := y.inner.Run(window)
	if err != nil {
		return nil, err
	}
	if len(out) < YAMNetClassCount {
		return nil, fmt.Errorf("yamnet: output too small: got %d want >= %d", len(out), YAMNetClassCount)
	}
	scores := make([]float32, YAMNetClassCount)
	copy(scores, out[:YAMNetClassCount])
	return scores, nil
}

// Close releases the underlying backend.
func (y *YAMNetBackend) Close() { y.inner.Close() }

// Name returns the backend identifier for logging.
func (y *YAMNetBackend) Name() string { return "YAMNet (CPU)" }
