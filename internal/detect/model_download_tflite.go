package detect

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
)

// TFLite model files for EdgeTPU and CPU-only inference.
// These are pre-compiled models from the Coral model repository.
const (
	// EfficientDet-Lite0 for EdgeTPU — 320x320 input, ~6ms inference on Coral USB.
	tfliteEdgeTPUModelFile = "efficientdet_lite0_edgetpu.tflite"
	tfliteEdgeTPUModelURL  = "https://raw.githubusercontent.com/google-coral/test_data/master/efficientdet_lite0_320_ptq_edgetpu.tflite"

	// EfficientDet-Lite0 for CPU — same model without EdgeTPU compilation.
	tfliteCPUModelFile = "efficientdet_lite0.tflite"
	tfliteCPUModelURL  = "https://raw.githubusercontent.com/google-coral/test_data/master/efficientdet_lite0_320_ptq.tflite"
)

// downloadTFLiteModel downloads and caches the appropriate TFLite model.
// If edgetpu is true, downloads the EdgeTPU-compiled variant.
// Returns the path to the cached model file.
func downloadTFLiteModel(edgetpu bool) (string, error) {
	var modelFile, modelURL string
	if edgetpu {
		modelFile = tfliteEdgeTPUModelFile
		modelURL = tfliteEdgeTPUModelURL
	} else {
		modelFile = tfliteCPUModelFile
		modelURL = tfliteCPUModelURL
	}

	cacheDir := modelCacheDir()
	destPath := filepath.Join(cacheDir, modelFile)

	// Return cached model if it exists.
	if _, err := os.Stat(destPath); err == nil {
		slog.Info("using cached TFLite model", "path", destPath)
		return destPath, nil
	}

	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}

	slog.Info("downloading TFLite model", "url", modelURL, "edgetpu", edgetpu)

	resp, err := http.Get(modelURL) //nolint:gosec // Google's official Coral model repo
	if err != nil {
		return "", fmt.Errorf("download tflite model: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download tflite model: HTTP %d", resp.StatusCode)
	}

	tmp := destPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}

	n, err := io.Copy(f, resp.Body)
	if closeErr := f.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("write model: %w", err)
	}

	if err := os.Rename(tmp, destPath); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("rename model file: %w", err)
	}

	slog.Info("TFLite model downloaded and cached",
		"path", destPath, "size", n, "edgetpu", edgetpu)
	return destPath, nil
}
