package detect

import (
	"fmt"
	"image"
	"log/slog"
	"os"
	"runtime"

	"github.com/rvben/watchpost/internal/config"
	"github.com/rvben/watchpost/internal/detect/onnxruntime"
)

// Detection represents a single detected object.
type Detection struct {
	Label string
	Score float32
	Box   [4]int // x1, y1, x2, y2
}

// Detector runs object detection on image frames using a pure Go ONNX runtime.
type Detector struct {
	config  config.DetectConfig
	session *onnxruntime.Session
	enabled bool
}

func New(cfg config.DetectConfig) *Detector {
	d := &Detector{
		config: cfg,
	}

	if err := d.init(cfg); err != nil {
		slog.Warn("object detection unavailable, using motion-only mode",
			"reason", err.Error(),
		)
		return d
	}

	d.enabled = true
	slog.Info("object detection initialized", "backend", detectBackend())

	return d
}

func (d *Detector) MotionThreshold() float64 {
	return d.config.MotionThreshold
}

// Detect runs object detection on a frame and returns detections above threshold.
func (d *Detector) Detect(img *image.RGBA) []Detection {
	if !d.enabled {
		return nil
	}

	// Preprocess: resize to 640x640, normalize, convert to CHW tensor
	inputData, scale, padX, padY := prepareInput(img)

	// Create input tensor [1, 3, 640, 640]
	input := onnxruntime.NewTensor([]int64{1, 3, modelInputSize, modelInputSize}, inputData)

	// Run inference
	outputs, err := d.session.Run(map[string]*onnxruntime.Tensor{
		"images": input,
	})
	if err != nil {
		slog.Error("inference failed", "error", err)
		return nil
	}

	output, ok := outputs["output0"]
	if !ok {
		slog.Error("inference produced no output0 tensor")
		return nil
	}

	// Postprocess: extract detections, apply NMS
	return processOutput(output.Data, d.config.ScoreThreshold, scale, padX, padY)
}

func (d *Detector) Close() {
	// Pure Go runtime has no external resources to clean up
}

func (d *Detector) init(cfg config.DetectConfig) error {
	modelData, err := d.loadModelData(cfg.ModelPath)
	if err != nil {
		return fmt.Errorf("load model: %w", err)
	}

	session, err := onnxruntime.NewSession(modelData)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}

	d.session = session
	return nil
}

// loadModelData resolves model bytes from config path, embedded data, or auto-download.
func (d *Detector) loadModelData(modelPath string) ([]byte, error) {
	// 1. Explicit model path from config
	if modelPath != "" {
		slog.Info("loading model from path", "path", modelPath)
		data, err := os.ReadFile(modelPath)
		if err != nil {
			return nil, fmt.Errorf("read model file %q: %w", modelPath, err)
		}
		return data, nil
	}

	// 2. Embedded model (set via go:embed build tag)
	if len(embeddedModel) > 0 {
		slog.Info("using embedded model")
		return embeddedModel, nil
	}

	// 3. Check common locations
	candidates := []string{
		"yolov8n.onnx",
		"/tmp/yolov8n.onnx",
	}
	for _, path := range candidates {
		if data, err := os.ReadFile(path); err == nil {
			slog.Info("found model at", "path", path)
			return data, nil
		}
	}

	return nil, fmt.Errorf("no model found: set detect.model_path in config, embed with build tag, or place yolov8n.onnx in working directory")
}

func detectBackend() string {
	switch runtime.GOOS {
	case "darwin":
		return "pure Go + Apple Accelerate BLAS"
	default:
		return "pure Go"
	}
}
