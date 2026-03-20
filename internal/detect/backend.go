package detect

// Backend is the inference engine interface. Implementations execute an ONNX
// model and return raw output tensors as flat float32 slices.
//
// The detect package owns preprocessing (image → CHW tensor) and
// postprocessing (raw output → []Detection). Backends only handle inference.
//
// Thread safety: A single Backend instance is NOT safe for concurrent use.
// Each goroutine must use its own Backend, or callers must serialize access.
type Backend interface {
	// Run executes inference on a preprocessed CHW input tensor [1,3,640,640].
	// The input slice must have exactly inputTensorSize elements.
	// Returns the raw model output as a flat float32 slice.
	// The returned slice may be reused on the next call to Run.
	Run(input []float32) ([]float32, error)

	// Close releases any resources held by the backend.
	// Safe to call multiple times.
	Close()

	// Name returns a human-readable backend name for logging.
	Name() string
}

const inputTensorSize = 1 * 3 * modelInputSize * modelInputSize // 1*3*640*640 = 1,228,800
