//go:build tflite

package detect

/*
#cgo LDFLAGS: -ltensorflowlite_c
#cgo CFLAGS: -Wall

#include <stdlib.h>
#include <string.h>
#include <tensorflow/lite/c/c_api.h>

// EdgeTPU delegate — linked conditionally via libedgetpu.
// We dlopen it at runtime so builds work even without libedgetpu installed,
// but we also support static linking via the edgetpu build tag.
#include <tensorflow/lite/c/c_api_types.h>

// Forward declarations for EdgeTPU delegate functions.
// These are resolved at link time when -ledgetpu is available.
typedef struct TfLiteDelegate TfLiteDelegate;

// edgetpu_create wraps the EdgeTPU delegate creation.
// Returns NULL if EdgeTPU is not available.
static TfLiteDelegate* vedetta_edgetpu_create(int* ok) {
#ifdef VEDETTA_EDGETPU
	extern TfLiteDelegate* edgetpu_create_delegate_for_device(
		const char* device, const char* options);
	TfLiteDelegate* d = edgetpu_create_delegate_for_device(NULL, NULL);
	if (d != NULL) { *ok = 1; }
	return d;
#else
	*ok = 0;
	return NULL;
#endif
}

static void vedetta_edgetpu_destroy(TfLiteDelegate* d) {
#ifdef VEDETTA_EDGETPU
	extern void edgetpu_free_delegate(TfLiteDelegate*);
	if (d != NULL) { edgetpu_free_delegate(d); }
#endif
}
*/
import "C"

import (
	"fmt"
	"log/slog"
	"math"
	"unsafe"
)

// TFLiteBackend wraps the TensorFlow Lite C API for inference,
// with optional EdgeTPU hardware delegate for Coral accelerators.
//
// Build with: go build -tags tflite
// For EdgeTPU: go build -tags "tflite edgetpu" -ldflags "-ledgetpu"
//
// Not safe for concurrent use. Each goroutine needs its own instance.
type TFLiteBackend struct {
	model       *C.TfLiteModel
	interpreter *C.TfLiteInterpreter
	options     *C.TfLiteInterpreterOptions
	delegate    *C.TfLiteDelegate

	// Input tensor metadata.
	inputTensor  *C.TfLiteTensor
	inputSize    int   // number of elements
	inputBytes   int   // byte size of input tensor
	inputIsQuant bool  // true if input is uint8 quantized
	inputScale   float32
	inputZero    int32

	// Output tensor metadata.
	outputCount  int
	outputIsQuant []bool
	outputScales  []float32
	outputZeros   []int32

	// Reusable buffers.
	quantBuf  []uint8   // quantized input buffer
	outputBuf []float32 // dequantized output buffer

	hasEdgeTPU bool
}

// NewTFLiteBackend creates a TFLite inference backend.
// If useEdgeTPU is true, it attempts to load the EdgeTPU delegate for Coral hardware.
// Falls back to CPU-only TFLite if EdgeTPU is unavailable.
func NewTFLiteBackend(modelPath string, useEdgeTPU bool) (*TFLiteBackend, error) {
	b := &TFLiteBackend{}

	// Load the TFLite model from file.
	cPath := C.CString(modelPath)
	defer C.free(unsafe.Pointer(cPath))

	b.model = C.TfLiteModelCreateFromFile(cPath)
	if b.model == nil {
		return nil, fmt.Errorf("tflite: failed to load model from %s", modelPath)
	}

	// Create interpreter options.
	b.options = C.TfLiteInterpreterOptionsCreate()
	if b.options == nil {
		b.Close()
		return nil, fmt.Errorf("tflite: failed to create interpreter options")
	}

	// Use 2 threads — detection is not the bottleneck with EdgeTPU,
	// and we want to leave CPU headroom for face recognition.
	C.TfLiteInterpreterOptionsSetNumThreads(b.options, 2)

	// Try to load EdgeTPU delegate if requested.
	if useEdgeTPU {
		var ok C.int
		b.delegate = C.vedetta_edgetpu_create(&ok)
		if ok == 1 && b.delegate != nil {
			C.TfLiteInterpreterOptionsAddDelegate(b.options, b.delegate)
			b.hasEdgeTPU = true
			slog.Info("tflite: EdgeTPU delegate loaded")
		} else {
			slog.Warn("tflite: EdgeTPU requested but not available, using CPU")
		}
	}

	// Create the interpreter.
	b.interpreter = C.TfLiteInterpreterCreate(b.model, b.options)
	if b.interpreter == nil {
		b.Close()
		return nil, fmt.Errorf("tflite: failed to create interpreter")
	}

	// Allocate tensors.
	if status := C.TfLiteInterpreterAllocateTensors(b.interpreter); status != C.kTfLiteOk {
		b.Close()
		return nil, fmt.Errorf("tflite: failed to allocate tensors")
	}

	// Inspect input tensor.
	inputCount := int(C.TfLiteInterpreterGetInputTensorCount(b.interpreter))
	if inputCount == 0 {
		b.Close()
		return nil, fmt.Errorf("tflite: model has no input tensors")
	}

	b.inputTensor = C.TfLiteInterpreterGetInputTensor(b.interpreter, 0)
	if b.inputTensor == nil {
		b.Close()
		return nil, fmt.Errorf("tflite: failed to get input tensor")
	}

	b.inputBytes = int(C.TfLiteTensorByteSize(b.inputTensor))
	inputType := C.TfLiteTensorType(b.inputTensor)

	// Calculate number of elements from tensor dimensions.
	numDims := int(C.TfLiteTensorNumDims(b.inputTensor))
	b.inputSize = 1
	for i := 0; i < numDims; i++ {
		b.inputSize *= int(C.TfLiteTensorDim(b.inputTensor, C.int32_t(i)))
	}

	if inputType == C.kTfLiteUInt8 {
		b.inputIsQuant = true
		params := C.TfLiteTensorQuantizationParams(b.inputTensor)
		b.inputScale = float32(params.scale)
		b.inputZero = int32(params.zero_point)
		b.quantBuf = make([]uint8, b.inputSize)
		slog.Info("tflite: input tensor is quantized uint8",
			"scale", b.inputScale, "zero_point", b.inputZero, "elements", b.inputSize)
	} else if inputType == C.kTfLiteFloat32 {
		slog.Info("tflite: input tensor is float32", "elements", b.inputSize)
	} else {
		b.Close()
		return nil, fmt.Errorf("tflite: unsupported input type %d (need float32 or uint8)", int(inputType))
	}

	// Inspect output tensors.
	b.outputCount = int(C.TfLiteInterpreterGetOutputTensorCount(b.interpreter))
	if b.outputCount == 0 {
		b.Close()
		return nil, fmt.Errorf("tflite: model has no output tensors")
	}

	b.outputIsQuant = make([]bool, b.outputCount)
	b.outputScales = make([]float32, b.outputCount)
	b.outputZeros = make([]int32, b.outputCount)

	totalOutputElements := 0
	for i := 0; i < b.outputCount; i++ {
		tensor := C.TfLiteInterpreterGetOutputTensor(b.interpreter, C.int32_t(i))
		outType := C.TfLiteTensorType(tensor)
		if outType == C.kTfLiteUInt8 {
			b.outputIsQuant[i] = true
			params := C.TfLiteTensorQuantizationParams(tensor)
			b.outputScales[i] = float32(params.scale)
			b.outputZeros[i] = int32(params.zero_point)
		}
		numDims := int(C.TfLiteTensorNumDims(tensor))
		elements := 1
		for d := 0; d < numDims; d++ {
			elements *= int(C.TfLiteTensorDim(tensor, C.int32_t(d)))
		}
		totalOutputElements += elements
	}

	b.outputBuf = make([]float32, totalOutputElements)

	slog.Info("tflite: backend initialized",
		"input_elements", b.inputSize,
		"output_tensors", b.outputCount,
		"total_output_elements", totalOutputElements,
		"edgetpu", b.hasEdgeTPU)

	return b, nil
}

// Run executes inference on a preprocessed float32 input tensor.
// For quantized models (EdgeTPU), input is quantized to uint8 before inference
// and output is dequantized back to float32.
//
// Returns a flat float32 slice containing all output tensors concatenated.
// The returned slice may be reused on the next call to Run.
func (b *TFLiteBackend) Run(input []float32) ([]float32, error) {
	if len(input) != b.inputSize {
		return nil, fmt.Errorf("tflite: input size %d, want %d", len(input), b.inputSize)
	}

	// Copy input data to the TFLite input tensor.
	if b.inputIsQuant {
		// Quantize float32 → uint8: q = clamp(round(value / scale) + zero_point, 0, 255)
		quantizeFloat32ToUint8(input, b.quantBuf, b.inputScale, b.inputZero)
		if status := C.TfLiteTensorCopyFromBuffer(
			b.inputTensor,
			unsafe.Pointer(&b.quantBuf[0]),
			C.size_t(len(b.quantBuf)),
		); status != C.kTfLiteOk {
			return nil, fmt.Errorf("tflite: failed to copy quantized input")
		}
	} else {
		if status := C.TfLiteTensorCopyFromBuffer(
			b.inputTensor,
			unsafe.Pointer(&input[0]),
			C.size_t(len(input)*4),
		); status != C.kTfLiteOk {
			return nil, fmt.Errorf("tflite: failed to copy float input")
		}
	}

	// Run inference.
	if status := C.TfLiteInterpreterInvoke(b.interpreter); status != C.kTfLiteOk {
		return nil, fmt.Errorf("tflite: inference failed")
	}

	// Read output tensors into a single flat buffer.
	offset := 0
	for i := 0; i < b.outputCount; i++ {
		tensor := C.TfLiteInterpreterGetOutputTensor(b.interpreter, C.int32_t(i))
		numDims := int(C.TfLiteTensorNumDims(tensor))
		elements := 1
		for d := 0; d < numDims; d++ {
			elements *= int(C.TfLiteTensorDim(tensor, C.int32_t(d)))
		}

		if b.outputIsQuant[i] {
			// Dequantize uint8 → float32: value = (q - zero_point) * scale
			byteSize := C.TfLiteTensorByteSize(tensor)
			tmpBuf := make([]uint8, int(byteSize))
			C.memcpy(
				unsafe.Pointer(&tmpBuf[0]),
				C.TfLiteTensorData(tensor),
				byteSize,
			)
			dequantizeUint8ToFloat32(tmpBuf[:elements], b.outputBuf[offset:offset+elements],
				b.outputScales[i], b.outputZeros[i])
		} else {
			C.memcpy(
				unsafe.Pointer(&b.outputBuf[offset]),
				C.TfLiteTensorData(tensor),
				C.size_t(elements*4),
			)
		}
		offset += elements
	}

	return b.outputBuf[:offset], nil
}

// Close releases all TFLite resources. Safe to call multiple times.
func (b *TFLiteBackend) Close() {
	if b.interpreter != nil {
		C.TfLiteInterpreterDelete(b.interpreter)
		b.interpreter = nil
	}
	if b.options != nil {
		C.TfLiteInterpreterOptionsDelete(b.options)
		b.options = nil
	}
	if b.delegate != nil {
		C.vedetta_edgetpu_destroy(b.delegate)
		b.delegate = nil
	}
	if b.model != nil {
		C.TfLiteModelDelete(b.model)
		b.model = nil
	}
}

// Name returns the backend identifier.
func (b *TFLiteBackend) Name() string {
	if b.hasEdgeTPU {
		return "TFLite + EdgeTPU"
	}
	return "TFLite (CPU)"
}

// OutputTensorCount returns the number of output tensors in the model.
// This is needed by SSD post-processing to know how many tensors to parse.
func (b *TFLiteBackend) OutputTensorCount() int {
	return b.outputCount
}

// OutputTensorSize returns the number of elements in the i-th output tensor.
func (b *TFLiteBackend) OutputTensorSize(idx int) int {
	if idx < 0 || idx >= b.outputCount {
		return 0
	}
	tensor := C.TfLiteInterpreterGetOutputTensor(b.interpreter, C.int32_t(idx))
	numDims := int(C.TfLiteTensorNumDims(tensor))
	elements := 1
	for d := 0; d < numDims; d++ {
		elements *= int(C.TfLiteTensorDim(tensor, C.int32_t(d)))
	}
	return elements
}

// InputSize returns the total number of input elements expected.
func (b *TFLiteBackend) InputSize() int {
	return b.inputSize
}

// --- Quantization helpers ---

// quantizeFloat32ToUint8 converts float32 values to uint8 using affine quantization:
//
//	q = clamp(round(value / scale + zero_point), 0, 255)
func quantizeFloat32ToUint8(src []float32, dst []uint8, scale float32, zeroPoint int32) {
	invScale := 1.0 / scale
	for i, v := range src {
		q := int32(math.Round(float64(v)*float64(invScale))) + zeroPoint
		if q < 0 {
			q = 0
		} else if q > 255 {
			q = 255
		}
		dst[i] = uint8(q)
	}
}

// dequantizeUint8ToFloat32 converts uint8 quantized values back to float32:
//
//	value = (q - zero_point) * scale
func dequantizeUint8ToFloat32(src []uint8, dst []float32, scale float32, zeroPoint int32) {
	for i, q := range src {
		dst[i] = float32(int32(q)-zeroPoint) * scale
	}
}
