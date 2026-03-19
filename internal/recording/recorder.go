package recording

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/rvben/watchpost/internal/camera"
	"github.com/rvben/watchpost/internal/config"
	"github.com/rvben/watchpost/internal/storage"
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
	segments   *SegmentRecorder
	cameraURLs map[string]string // camera name → record RTSP URL
}

func New(cfg config.RecordingConfig, db *storage.DB) *Recorder {
	if err := os.MkdirAll(cfg.Path, 0o755); err != nil {
		slog.Error("failed to create recording directory", "path", cfg.Path, "error", err)
	}

	return &Recorder{
		config:     cfg,
		db:         db,
		segments:   NewSegmentRecorder(cfg, db),
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

	for name, url := range r.cameraURLs {
		segDir := filepath.Join(r.config.Path, name, "segments")
		r.segments.ScanExistingSegments(name, segDir)
		r.segments.StartRecording(ctx, name, url)
	}

	slog.Info("continuous recording started", "cameras", len(r.cameraURLs))
}

// SaveClip records a clip around the event timestamp.
// It first tries to extract from existing segments, then falls back to direct recording.
func (r *Recorder) SaveClip(ctx context.Context, event camera.Event) error {
	clipPath, err := r.ExtractClip(ctx, event)
	if err != nil {
		return fmt.Errorf("extract clip: %w", err)
	}

	// Update the event with the clip path
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

// recordFromStream uses ffmpeg to capture a clip directly from RTSP.
func (r *Recorder) recordFromStream(ctx context.Context, rtspURL, outputPath string, duration time.Duration) error {
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner",
		"-loglevel", "error",
		"-rtsp_transport", "tcp",
		"-i", rtspURL,
		"-t", fmt.Sprintf("%.0f", duration.Seconds()),
		"-c:v", "copy",
		"-c:a", "aac",
		"-movflags", "frag_keyframe+empty_moov",
		"-y",
		outputPath,
	)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg record: %w: %s", err, string(output))
	}

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
