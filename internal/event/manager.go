package event

import (
	"fmt"
	"image"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rvben/vedetta/internal/camera"
	"github.com/rvben/vedetta/internal/detect"
	"github.com/rvben/vedetta/internal/snapshot"
)

// Config holds event manager settings.
type Config struct {
	CooldownSeconds  int    `yaml:"cooldown_seconds"`
	SnapshotPath     string `yaml:"snapshot_path"`
	SnapshotQuality  int    `yaml:"snapshot_quality"`
}

// cooldownKey uniquely identifies a camera+label pair for cooldown tracking.
type cooldownKey struct {
	Camera string
	Label  string
}

// Manager processes raw detections into deduplicated events with cooldown.
type Manager struct {
	config    Config
	cooldowns map[cooldownKey]time.Time
	mu        sync.Mutex
	seq       atomic.Uint64
}

// NewManager creates an event manager with the given config.
func NewManager(cfg Config) *Manager {
	if cfg.CooldownSeconds <= 0 {
		cfg.CooldownSeconds = 30
	}
	if cfg.SnapshotPath == "" {
		cfg.SnapshotPath = "./snapshots"
	}
	if cfg.SnapshotQuality <= 0 {
		cfg.SnapshotQuality = 85
	}

	return &Manager{
		config:    cfg,
		cooldowns: make(map[cooldownKey]time.Time),
	}
}

// ProcessDetection checks cooldown and creates an event if not suppressed.
// Returns nil if the detection is suppressed by cooldown.
// The frame parameter is used to generate a snapshot with bounding box overlay.
func (m *Manager) ProcessDetection(cameraName string, d detect.Detection, frame *image.RGBA) *camera.Event {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	key := cooldownKey{Camera: cameraName, Label: d.Label}
	cooldown := time.Duration(m.config.CooldownSeconds) * time.Second

	if last, ok := m.cooldowns[key]; ok {
		if now.Sub(last) < cooldown {
			return nil
		}
	}

	// Create event with unique ID using timestamp + sequence number
	seq := m.seq.Add(1)
	eventID := fmt.Sprintf("%s-%d-%d", cameraName, now.UnixMilli(), seq)
	event := &camera.Event{
		ID:         eventID,
		CameraName: cameraName,
		Label:      d.Label,
		Score:      d.Score,
		Box:        d.Box,
		Timestamp:  now,
	}

	// Generate snapshot with bounding box if frame is available
	if frame != nil {
		snapshotImg := snapshot.DrawDetections(frame, []detect.Detection{d})
		snapshotFile := filepath.Join(
			m.config.SnapshotPath,
			cameraName,
			fmt.Sprintf("%s.jpg", eventID),
		)
		if err := snapshot.SaveSnapshot(snapshotImg, snapshotFile, m.config.SnapshotQuality); err != nil {
			slog.Error("failed to save snapshot", "event", eventID, "error", err)
		} else {
			event.SnapshotPath = snapshotFile
		}
	}

	m.cooldowns[key] = now
	return event
}

// ProcessDetections processes multiple detections for a camera frame.
// Returns events for detections that are not suppressed by cooldown.
func (m *Manager) ProcessDetections(cameraName string, detections []detect.Detection, frame *image.RGBA) []camera.Event {
	var events []camera.Event
	for _, d := range detections {
		if event := m.ProcessDetection(cameraName, d, frame); event != nil {
			events = append(events, *event)
		}
	}
	return events
}

// CleanupCooldowns removes expired cooldown entries to prevent memory leaks.
// Should be called periodically.
func (m *Manager) CleanupCooldowns() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	cooldown := time.Duration(m.config.CooldownSeconds) * time.Second

	for key, lastTime := range m.cooldowns {
		if now.Sub(lastTime) > cooldown*2 {
			delete(m.cooldowns, key)
		}
	}
}
