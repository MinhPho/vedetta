package recording

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/rvben/vedetta/internal/camera"
	"github.com/rvben/vedetta/internal/config"
	"github.com/rvben/vedetta/internal/rtsp"
	"github.com/rvben/vedetta/internal/storage"
)

// StorageStats contains aggregate storage information.
type StorageStats struct {
	TotalBytes   int64            `json:"total_bytes"`
	SegmentCount int              `json:"segment_count"`
	CameraStats  map[string]int64 `json:"camera_stats"`
}

// Recorder manages saving video clips for detected events.
type Recorder struct {
	config     config.RecordingConfig
	db         *storage.DB
	hub        *rtsp.Hub
	segments   *SegmentRecorder
	cameraURLs map[string]string // camera name → record RTSP URL
}

func New(cfg config.RecordingConfig, db *storage.DB, hub *rtsp.Hub) *Recorder {
	if err := os.MkdirAll(cfg.Path, 0o755); err != nil {
		slog.Error("failed to create recording directory", "path", cfg.Path, "error", err)
	}

	return &Recorder{
		config:     cfg,
		db:         db,
		hub:        hub,
		segments:   NewSegmentRecorder(cfg, db, hub),
		cameraURLs: make(map[string]string),
	}
}

// RegisterCamera registers a camera's recording URL for direct-from-stream recording.
func (r *Recorder) RegisterCamera(name, rtspURL string) {
	r.cameraURLs[name] = rtspURL
}

// StartContinuousRecording begins segment recording for all registered cameras.
func (r *Recorder) StartContinuousRecording(ctx context.Context) {
	if !r.config.Continuous {
		slog.Info("continuous recording disabled")
		return
	}

	first := true
	for name, url := range r.cameraURLs {
		if !first {
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
		}
		segDir := filepath.Join(r.config.Path, name, "segments")
		r.segments.ScanExistingSegments(name, segDir)
		r.segments.StartRecording(ctx, name, url)
		first = false
	}

	slog.Info("continuous recording started", "cameras", len(r.cameraURLs))
}

// SaveClip records a clip around the event timestamp.
func (r *Recorder) SaveClip(ctx context.Context, event camera.Event) error {
	clipPath, err := r.ExtractClip(ctx, event)
	if err != nil {
		return fmt.Errorf("extract clip: %w", err)
	}

	if err := r.db.UpdateEventClipPath(event.ID, clipPath); err != nil {
		slog.Error("failed to update event clip path", "error", err)
	}

	slog.Info("clip saved",
		"camera", event.CameraName,
		"label", event.Label,
		"path", clipPath,
	)

	return nil
}

// StorageStats queries the database for aggregate storage information.
func (r *Recorder) StorageStats() StorageStats {
	stats := StorageStats{
		CameraStats: make(map[string]int64),
	}

	totalBytes, err := r.db.TotalSegmentBytes()
	if err != nil {
		slog.Error("failed to query total segment bytes", "error", err)
	} else {
		stats.TotalBytes = totalBytes
	}

	count, err := r.db.CountSegments()
	if err != nil {
		slog.Error("failed to query segment count", "error", err)
	} else {
		stats.SegmentCount = count
	}

	byCamera, err := r.db.SegmentBytesByCamera()
	if err != nil {
		slog.Error("failed to query segment bytes by camera", "error", err)
	} else {
		stats.CameraStats = byCamera
	}

	return stats
}

// ListSegmentsForDate returns segments for a camera on a specific date.
func (r *Recorder) ListSegmentsForDate(cameraName string, date time.Time) []storage.SegmentRecord {
	segments, err := r.db.GetSegmentsForDate(cameraName, date)
	if err != nil {
		slog.Error("failed to query segments for date",
			"camera", cameraName,
			"date", date,
			"error", err,
		)
		return nil
	}
	return segments
}
