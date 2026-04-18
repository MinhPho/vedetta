package detect

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
)

const (
	yamnetModelFile = "yamnet.tflite"
	// MediaPipe's stable mirror of the official Google YAMNet TFLite export.
	// ~3.8 MB, float32 input, 521-class output (AudioSet ontology).
	yamnetModelURL = "https://storage.googleapis.com/mediapipe-models/audio_classifier/yamnet/float32/latest/yamnet.tflite"
)

// downloadYAMNetModel returns the path to a cached YAMNet TFLite model,
// downloading once on first call. The download is atomic (rename from .tmp)
// so a crashed download won't poison the cache on next start.
func downloadYAMNetModel() (string, error) {
	cacheDir := modelCacheDir()
	destPath := filepath.Join(cacheDir, yamnetModelFile)

	if _, err := os.Stat(destPath); err == nil {
		slog.Info("using cached YAMNet model", "path", destPath)
		return destPath, nil
	}

	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}

	slog.Info("downloading YAMNet model", "url", yamnetModelURL)

	resp, err := http.Get(yamnetModelURL) //nolint:gosec // Google's official MediaPipe model mirror
	if err != nil {
		return "", fmt.Errorf("download yamnet model: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download yamnet model: HTTP %d", resp.StatusCode)
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

	slog.Info("YAMNet model downloaded and cached", "path", destPath, "size", n)
	return destPath, nil
}
