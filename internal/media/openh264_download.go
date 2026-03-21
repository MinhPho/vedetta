package media

import (
	"compress/bzip2"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
)

const openh264Version = "2.6.0"

// openH264DownloadURL returns the Cisco binary download URL and local filename
// for the current platform.
func openH264DownloadURL() (url, filename string) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64":
		return fmt.Sprintf("http://ciscobinary.openh264.org/libopenh264-%s-mac-arm64.dylib.bz2", openh264Version),
			"libopenh264.dylib"
	case "darwin/amd64":
		return fmt.Sprintf("http://ciscobinary.openh264.org/libopenh264-%s-mac-x64.dylib.bz2", openh264Version),
			"libopenh264.dylib"
	case "linux/amd64":
		return fmt.Sprintf("http://ciscobinary.openh264.org/libopenh264-%s-linux64.8.so.bz2", openh264Version),
			"libopenh264.so"
	case "linux/arm64":
		return fmt.Sprintf("http://ciscobinary.openh264.org/libopenh264-%s-linux-arm64.8.so.bz2", openh264Version),
			"libopenh264.so"
	default:
		return "", ""
	}
}

// openH264CacheDir returns the directory used to cache the downloaded library.
func openH264CacheDir() string {
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "vedetta")
	}
	return filepath.Join(os.TempDir(), "vedetta-cache")
}

// cachedOpenH264Path returns the path where the cached library should be.
func cachedOpenH264Path() string {
	_, filename := openH264DownloadURL()
	if filename == "" {
		return ""
	}
	return filepath.Join(openH264CacheDir(), filename)
}

// downloadOpenH264 downloads the OpenH264 binary from Cisco's servers,
// decompresses it, and caches it locally. Returns the path to the library.
func downloadOpenH264() (string, error) {
	url, filename := openH264DownloadURL()
	if url == "" {
		return "", fmt.Errorf("no OpenH264 binary available for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	cacheDir := openH264CacheDir()
	destPath := filepath.Join(cacheDir, filename)

	// Already cached?
	if _, err := os.Stat(destPath); err == nil {
		return destPath, nil
	}

	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}

	slog.Info("downloading OpenH264 from Cisco", "url", url)
	slog.Info("OpenH264 Video Codec provided by Cisco Systems, Inc.")

	resp, err := http.Get(url) //nolint:gosec // Cisco's official URL
	if err != nil {
		return "", fmt.Errorf("download OpenH264: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download OpenH264: HTTP %d", resp.StatusCode)
	}

	// Decompress bzip2
	bzReader := bzip2.NewReader(resp.Body)

	// Write to temp file then rename for atomicity
	tmp := destPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}

	n, err := io.Copy(f, bzReader)
	if closeErr := f.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("decompress OpenH264: %w", err)
	}

	if err := os.Rename(tmp, destPath); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("rename OpenH264 library: %w", err)
	}

	slog.Info("OpenH264 downloaded and cached", "path", destPath, "size", n)
	return destPath, nil
}
