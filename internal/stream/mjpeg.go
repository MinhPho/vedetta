package stream

import (
	"fmt"
	"image"
	"image/jpeg"
	"log/slog"
	"net/http"
	"time"
)

const (
	mjpegBoundary = "watchpostframe"
	mjpegFPS      = 5
)

// SnapshotFunc returns the latest snapshot for a camera.
type SnapshotFunc func() *image.RGBA

// MJPEGHandler returns an HTTP handler that serves a multipart MJPEG stream.
func MJPEGHandler(snapshotFn SnapshotFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", fmt.Sprintf("multipart/x-mixed-replace; boundary=%s", mjpegBoundary))
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		flusher.Flush()

		ticker := time.NewTicker(time.Second / mjpegFPS)
		defer ticker.Stop()

		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				img := snapshotFn()
				if img == nil {
					continue
				}

				var buf []byte
				writer := &sliceWriter{buf: &buf}
				if err := jpeg.Encode(writer, img, &jpeg.Options{Quality: 75}); err != nil {
					slog.Error("MJPEG encode error", "error", err)
					continue
				}

				header := fmt.Sprintf("\r\n--%s\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", mjpegBoundary, len(buf))
				if _, err := w.Write([]byte(header)); err != nil {
					return
				}
				if _, err := w.Write(buf); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	})
}

type sliceWriter struct {
	buf *[]byte
}

func (w *sliceWriter) Write(p []byte) (int, error) {
	*w.buf = append(*w.buf, p...)
	return len(p), nil
}
