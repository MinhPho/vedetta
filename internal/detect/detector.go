package detect

import (
	"fmt"
	"image"
	"log/slog"
	"math"
	"os"
	"sync"

	"github.com/rvben/vedetta/internal/config"
)

// Detection represents a single detected object.
type Detection struct {
	Label string
	Score float32
	Box   [4]int // x1, y1, x2, y2
}

// Detector runs object detection on image frames.
// It selects the best available backend automatically.
// Safe for concurrent use by multiple camera goroutines.
type Detector struct {
	mu           sync.Mutex
	config       config.DetectConfig
	backend      Backend
	enabled      bool
	inputBuf     []float32       // reusable preprocessing buffer, guarded by mu
	labelAllowed map[string]bool // nil = allow all labels

	// Model type determines pre/post-processing pipeline.
	// "yolo" uses CHW input + YOLOv8 output parsing.
	// "ssd" uses HWC input + SSD/EfficientDet output parsing.
	modelType string

	// SSD-specific fields (only set when modelType == "ssd").
	ssdLayout    SSDOutputLayout
	ssdInputW    int // model input width (e.g. 320 for EfficientDet-Lite0)
	ssdInputH    int // model input height
}

func New(cfg config.DetectConfig) *Detector {
	d := &Detector{
		config: cfg,
	}

	if len(cfg.Labels) > 0 {
		d.labelAllowed = make(map[string]bool, len(cfg.Labels))
		for _, l := range cfg.Labels {
			d.labelAllowed[l] = true
		}
		slog.Info("label filter active", "labels", cfg.Labels)
	}

	if err := d.init(cfg); err != nil {
		slog.Warn("object detection unavailable, using motion-only mode",
			"reason", err.Error(),
		)
		return d
	}

	d.enabled = true
	slog.Info("object detection initialized",
		"backend", d.backend.Name(),
		"model_type", d.modelType)

	return d
}

func (d *Detector) filterLabels(dets []Detection) []Detection {
	if d.labelAllowed == nil {
		return dets
	}
	filtered := dets[:0]
	for _, det := range dets {
		if d.labelAllowed[det.Label] {
			filtered = append(filtered, det)
		}
	}
	return filtered
}

func (d *Detector) MotionThreshold() float64 {
	return d.config.Motion.MinRegionScore
}

func (d *Detector) Available() bool {
	return d != nil && d.enabled
}

// Detect runs object detection on a frame and returns detections above threshold.
// Safe for concurrent use — serializes access to the backend.
// Recovers from panics in the inference backend to prevent server crashes.
func (d *Detector) Detect(img *image.RGBA) (result []Detection) {
	if !d.enabled {
		return nil
	}

	defer func() {
		if r := recover(); r != nil {
			slog.Error("inference panic recovered", "error", r)
			result = nil
		}
	}()

	d.mu.Lock()
	defer d.mu.Unlock()

	switch d.modelType {
	case "ssd":
		bounds := img.Bounds()
		bufSize := d.ssdInputW * d.ssdInputH * 3
		if d.inputBuf == nil || len(d.inputBuf) < bufSize {
			d.inputBuf = make([]float32, bufSize)
		}
		inputData, scale, padX, padY := prepareSSDInputRGBA(
			d.inputBuf, img.Pix, bounds.Dx(), bounds.Dy(), d.ssdInputW, d.ssdInputH)

		output, err := d.backend.Run(inputData)
		if err != nil {
			slog.Error("inference failed", "error", err)
			return nil
		}
		return d.filterLabels(processSSDOutput(output, d.ssdLayout,
			d.config.ScoreThreshold, bounds.Dx(), bounds.Dy(), scale, padX, padY))

	default: // "yolo"
		if d.inputBuf == nil || len(d.inputBuf) < 3*modelInputSize*modelInputSize {
			d.inputBuf = make([]float32, 3*modelInputSize*modelInputSize)
		}
		inputData, scale, padX, padY := prepareInputInto(d.inputBuf, img)

		output, err := d.backend.Run(inputData)
		if err != nil {
			slog.Error("inference failed", "error", err)
			return nil
		}
		return d.filterLabels(processOutput(output, d.config.ScoreThreshold, scale, padX, padY))
	}
}

// DetectRGB24 runs object detection directly on RGB24 frame data,
// avoiding the intermediate RGBA conversion.
// Safe for concurrent use — serializes access to the backend.
// Recovers from panics in the inference backend to prevent server crashes.
func (d *Detector) DetectRGB24(data []byte, w, h int) (result []Detection) {
	if !d.enabled {
		return nil
	}

	defer func() {
		if r := recover(); r != nil {
			slog.Error("inference panic recovered", "error", r)
			result = nil
		}
	}()

	d.mu.Lock()
	defer d.mu.Unlock()

	switch d.modelType {
	case "ssd":
		bufSize := d.ssdInputW * d.ssdInputH * 3
		if d.inputBuf == nil || len(d.inputBuf) < bufSize {
			d.inputBuf = make([]float32, bufSize)
		}
		inputData, scale, padX, padY := prepareSSDInput(d.inputBuf, data, w, h, d.ssdInputW, d.ssdInputH)

		output, err := d.backend.Run(inputData)
		if err != nil {
			slog.Error("inference failed", "error", err)
			return nil
		}
		return d.filterLabels(processSSDOutput(output, d.ssdLayout,
			d.config.ScoreThreshold, w, h, scale, padX, padY))

	default: // "yolo"
		if d.inputBuf == nil || len(d.inputBuf) < 3*modelInputSize*modelInputSize {
			d.inputBuf = make([]float32, 3*modelInputSize*modelInputSize)
		}
		inputData, scale, padX, padY := prepareInputFromRGB24Into(d.inputBuf, data, w, h)

		output, err := d.backend.Run(inputData)
		if err != nil {
			slog.Error("inference failed", "error", err)
			return nil
		}
		return d.filterLabels(processOutput(output, d.config.ScoreThreshold, scale, padX, padY))
	}
}

func (d *Detector) Close() {
	if d.backend != nil {
		d.backend.Close()
	}
}

func (d *Detector) init(cfg config.DetectConfig) error {
	// TFLite/EdgeTPU backends load from file path, not byte slice.
	if cfg.Backend == "tflite" || cfg.Backend == "edgetpu" {
		return d.initTFLite(cfg)
	}

	modelData, err := d.loadModelData(cfg.ModelPath)
	if err != nil {
		return fmt.Errorf("load model: %w", err)
	}

	backend, err := selectBackend(cfg.Backend, modelData)
	if err != nil {
		return err
	}

	d.backend = backend
	d.modelType = resolveModelType(cfg.ModelType, cfg.Backend)
	return nil
}

// initTFLite initializes the detector with a TFLite backend.
// TFLite models are loaded from file (supports memory-mapping).
func (d *Detector) initTFLite(cfg config.DetectConfig) error {
	modelPath := cfg.TFLiteModelPath
	if modelPath == "" {
		// Auto-download an EfficientDet-Lite model for EdgeTPU.
		var err error
		modelPath, err = downloadTFLiteModel(cfg.Backend == "edgetpu")
		if err != nil {
			return fmt.Errorf("tflite model: %w", err)
		}
	}

	useEdgeTPU := cfg.Backend == "edgetpu"
	backend, err := NewTFLiteBackend(modelPath, useEdgeTPU)
	if err != nil {
		return fmt.Errorf("tflite backend: %w", err)
	}

	d.backend = backend
	d.modelType = resolveModelType(cfg.ModelType, cfg.Backend)

	// If using SSD model type, inspect the TFLite output tensors to build the layout.
	if d.modelType == "ssd" {
		if err := d.initSSDLayout(backend); err != nil {
			backend.Close()
			return err
		}
	}

	return nil
}

// initSSDLayout reads TFLite output tensor dimensions to configure SSD post-processing.
func (d *Detector) initSSDLayout(b *TFLiteBackend) error {
	if b.OutputTensorCount() < 4 {
		return fmt.Errorf("ssd model requires 4 output tensors, got %d", b.OutputTensorCount())
	}

	boxesSize := b.OutputTensorSize(0)  // N * 4
	classSize := b.OutputTensorSize(1)  // N
	scoreSize := b.OutputTensorSize(2)  // N
	countSize := b.OutputTensorSize(3)  // 1

	if classSize == 0 || boxesSize != classSize*4 {
		return fmt.Errorf("ssd output layout mismatch: boxes=%d classes=%d scores=%d count=%d",
			boxesSize, classSize, scoreSize, countSize)
	}

	d.ssdLayout = SSDOutputLayout{
		BoxesSize: boxesSize,
		ClassSize: classSize,
		ScoreSize: scoreSize,
		CountSize: countSize,
	}

	// Infer input dimensions from the TFLite input tensor.
	// SSD models typically have input shape [1, H, W, 3].
	inputElements := b.InputSize()
	// inputElements = 1 * H * W * 3, and for square models H == W.
	side := int(math.Sqrt(float64(inputElements / 3)))
	if side*side*3 != inputElements {
		// Non-square — try common sizes.
		for _, sz := range []int{300, 320, 384, 448, 512} {
			if sz*sz*3 == inputElements {
				side = sz
				break
			}
		}
	}
	d.ssdInputW = side
	d.ssdInputH = side

	slog.Info("ssd model configured",
		"input_size", side,
		"max_detections", classSize,
		"boxes", boxesSize)

	return nil
}

// resolveModelType determines the post-processing pipeline based on config and backend.
func resolveModelType(configured string, backend string) string {
	switch configured {
	case "yolo":
		return "yolo"
	case "ssd":
		return "ssd"
	case "", "auto":
		// TFLite/EdgeTPU backends default to SSD (EfficientDet-Lite).
		// ONNX backends default to YOLO (YOLOv8n).
		if backend == "tflite" || backend == "edgetpu" {
			return "ssd"
		}
		return "yolo"
	default:
		slog.Warn("unknown model_type, defaulting to yolo", "model_type", configured)
		return "yolo"
	}
}

// selectBackend picks the best available ONNX backend based on config and build tags.
// TFLite backends are handled separately in initTFLite().
func selectBackend(preference string, modelData []byte) (Backend, error) {
	switch preference {
	case "go":
		return NewGoBackend(modelData)

	case "onnxruntime_c":
		b, err := NewCAPIBackend(modelData)
		if err != nil {
			return nil, fmt.Errorf("c ONNX Runtime backend: %w", err)
		}
		return b, nil

	case "", "auto":
		// Try C ONNX Runtime first (faster), fall back to pure Go.
		b, err := NewCAPIBackend(modelData)
		if err == nil {
			slog.Info("auto-selected C ONNX Runtime backend")
			return b, nil
		}
		slog.Info("C ONNX Runtime not available, using pure Go backend", "reason", err.Error())
		return NewGoBackend(modelData)

	default:
		return nil, fmt.Errorf("unknown backend %q: use \"auto\", \"go\", \"onnxruntime_c\", \"tflite\", or \"edgetpu\"", preference)
	}
}

// loadModelData resolves model bytes from config path, embedded data, common locations, or auto-download.
func (d *Detector) loadModelData(modelPath string) ([]byte, error) {
	if modelPath != "" {
		slog.Info("loading model from path", "path", modelPath)
		data, err := os.ReadFile(modelPath)
		if err == nil {
			return data, nil
		}
		slog.Warn("configured model not found, will try cache/download", "path", modelPath, "error", err)
	}

	if len(embeddedModel) > 0 {
		slog.Info("using embedded model")
		return embeddedModel, nil
	}

	// Check common local paths
	candidates := []string{
		"yolov8n.onnx",
		cachedModelPath(),
	}
	for _, path := range candidates {
		if data, err := os.ReadFile(path); err == nil {
			slog.Info("found model at", "path", path)
			return data, nil
		}
	}

	// Auto-download as last resort
	path, err := downloadModel()
	if err != nil {
		return nil, fmt.Errorf("auto-download model: %w", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read downloaded model: %w", err)
	}
	return data, nil
}
