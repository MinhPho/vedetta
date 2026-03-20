package media

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/pion/rtp"

	"github.com/rvben/vedetta/internal/rtsp"
)

// SegmentInfo is passed to the OnSegmentDone callback when a segment is completed.
type SegmentInfo struct {
	Camera    string
	Path      string
	StartTime time.Time
	EndTime   time.Time
	SizeBytes int64
}

// RecordingConsumer implements rtsp.Consumer and writes RTP packets to fMP4 segments.
type RecordingConsumer struct {
	camera     string
	segLen     time.Duration
	videoTrack *rtsp.TrackInfo
	audioTrack *rtsp.TrackInfo
	onSegment  func(SegmentInfo)

	mu       sync.Mutex
	writer   *SegmentWriter
	segPath  string
	segStart time.Time
	segDir   string
}

// NewRecordingConsumer creates a consumer that records to rotating fMP4 segments.
// onSegment is called when each segment completes (for DB registration).
func NewRecordingConsumer(segDir, camera string, segLen time.Duration, video, audio *rtsp.TrackInfo, onSegment func(SegmentInfo)) *RecordingConsumer {
	if err := os.MkdirAll(segDir, 0o755); err != nil {
		slog.Error("failed to create segment directory", "camera", camera, "error", err)
	}

	return &RecordingConsumer{
		camera:     camera,
		segLen:     segLen,
		videoTrack: video,
		audioTrack: audio,
		onSegment:  onSegment,
		segDir:     segDir,
	}
}

// OnVideoRTP receives a video RTP packet and writes it to the current segment.
func (rc *RecordingConsumer) OnVideoRTP(pkt *rtp.Packet) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if err := rc.ensureSegment(); err != nil {
		slog.Error("ensure segment failed", "camera", rc.camera, "error", err)
		return
	}

	if err := rc.writer.WriteVideo(pkt); err != nil {
		slog.Error("write video failed", "camera", rc.camera, "error", err)
	}

	rc.maybeRotate()
}

// OnAudioRTP receives an audio RTP packet.
func (rc *RecordingConsumer) OnAudioRTP(pkt *rtp.Packet) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if rc.writer == nil {
		return
	}

	if err := rc.writer.WriteAudio(pkt); err != nil {
		slog.Error("write audio failed", "camera", rc.camera, "error", err)
	}
}

// OnDisconnect is called when the RTSP source disconnects.
func (rc *RecordingConsumer) OnDisconnect() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.closeCurrentSegment()
}

// Close finalizes the current segment.
func (rc *RecordingConsumer) Close() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.closeCurrentSegment()
}

func (rc *RecordingConsumer) ensureSegment() error {
	if rc.writer != nil {
		return nil
	}

	now := time.Now()
	rc.segStart = now
	rc.segPath = filepath.Join(rc.segDir, fmt.Sprintf("%s.mp4", now.Format("2006-01-02_15-04-05")))

	var err error
	rc.writer, err = NewSegmentWriter(rc.segPath, rc.videoTrack, rc.audioTrack)
	if err != nil {
		return fmt.Errorf("create segment writer: %w", err)
	}

	slog.Debug("started new segment", "camera", rc.camera, "path", rc.segPath)
	return nil
}

func (rc *RecordingConsumer) maybeRotate() {
	if time.Since(rc.segStart) < rc.segLen {
		return
	}
	rc.closeCurrentSegment()
}

func (rc *RecordingConsumer) closeCurrentSegment() {
	if rc.writer == nil {
		return
	}

	duration, err := rc.writer.Close()
	if err != nil {
		slog.Error("close segment failed", "camera", rc.camera, "error", err)
	}

	if info, err := os.Stat(rc.segPath); err == nil && info.Size() > 0 {
		if rc.onSegment != nil {
			rc.onSegment(SegmentInfo{
				Camera:    rc.camera,
				Path:      rc.segPath,
				StartTime: rc.segStart,
				EndTime:   rc.segStart.Add(duration),
				SizeBytes: info.Size(),
			})
		}
		slog.Debug("segment completed", "camera", rc.camera, "path", rc.segPath,
			"duration", duration.Round(time.Second), "size", info.Size())
	} else {
		os.Remove(rc.segPath)
	}

	rc.writer = nil
}
