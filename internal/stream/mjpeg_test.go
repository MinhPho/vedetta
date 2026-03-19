package stream

import (
	"bufio"
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMJPEGHandlerProducesValidMultipart(t *testing.T) {
	// Create a simple test image
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{R: 128, G: 64, B: 32, A: 255})
		}
	}

	snapshotFn := func() *image.RGBA {
		return img
	}

	handler := MJPEGHandler(snapshotFn)

	// Use a test server so we can read the streaming response
	ts := httptest.NewServer(handler)
	defer ts.Close()

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(ts.URL)
	if err != nil {
		t.Fatalf("failed to GET MJPEG stream: %v", err)
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "multipart/x-mixed-replace") {
		t.Fatalf("expected multipart content type, got: %s", contentType)
	}

	if !strings.Contains(contentType, mjpegBoundary) {
		t.Fatalf("content type missing boundary, got: %s", contentType)
	}

	// Read at least one JPEG frame from the stream
	scanner := bufio.NewScanner(resp.Body)
	foundJPEGHeader := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "Content-Type: image/jpeg") {
			foundJPEGHeader = true
			break
		}
	}

	if !foundJPEGHeader {
		t.Fatal("did not find JPEG content type header in MJPEG stream")
	}
}

func TestMJPEGHandlerNilSnapshot(t *testing.T) {
	snapshotFn := func() *image.RGBA {
		return nil
	}

	handler := MJPEGHandler(snapshotFn)

	ts := httptest.NewServer(handler)
	defer ts.Close()

	client := &http.Client{Timeout: 1 * time.Second}
	resp, err := client.Get(ts.URL)
	if err != nil {
		t.Fatalf("failed to GET: %v", err)
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "multipart/x-mixed-replace") {
		t.Fatalf("expected multipart content type, got: %s", contentType)
	}
}
