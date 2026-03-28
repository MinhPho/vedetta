//go:build !tflite

package detect

import "fmt"

// TFLiteBackend is a stub when built without the tflite tag.
// All methods return errors indicating the backend is unavailable.
type TFLiteBackend struct{}

// NewTFLiteBackend returns an error when built without the tflite tag.
func NewTFLiteBackend(_ string, _ bool) (*TFLiteBackend, error) {
	return nil, fmt.Errorf("TFLite backend not available: build with -tags tflite")
}

func (b *TFLiteBackend) Run(_ []float32) ([]float32, error) {
	return nil, fmt.Errorf("TFLite backend not available")
}

func (b *TFLiteBackend) Close()      {}
func (b *TFLiteBackend) Name() string { return "TFLite [not compiled]" }

func (b *TFLiteBackend) OutputTensorCount() int    { return 0 }
func (b *TFLiteBackend) OutputTensorSize(_ int) int { return 0 }
func (b *TFLiteBackend) InputSize() int             { return 0 }
