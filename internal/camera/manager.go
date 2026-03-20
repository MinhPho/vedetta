package camera

import (
	"context"
	"sort"
	"sync"

	"github.com/rvben/vedetta/internal/config"
	"github.com/rvben/vedetta/internal/detect"
)

// Manager manages all camera streams.
type Manager struct {
	cameras  map[string]*Camera
	detector *detect.Detector
	events   chan<- Event
	hwaccel  *HWAccel
	mu       sync.RWMutex
}

func NewManager(configs []config.CameraConfig, detector *detect.Detector, events chan<- Event, hwaccel *HWAccel) *Manager {
	m := &Manager{
		cameras:  make(map[string]*Camera),
		detector: detector,
		events:   events,
		hwaccel:  hwaccel,
	}

	for _, cfg := range configs {
		if cfg.Enabled {
			cam := NewCamera(cfg, detector, events, hwaccel)
			m.cameras[cfg.Name] = cam
		}
	}

	return m
}

// HWAccelBackend returns the detected hardware acceleration, or nil for CPU-only.
func (m *Manager) HWAccelBackend() *HWAccel {
	return m.hwaccel
}

func (m *Manager) Start(ctx context.Context) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, cam := range m.cameras {
		cam.Start(ctx)
	}
}

func (m *Manager) GetCamera(name string) *Camera {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cameras[name]
}

func (m *Manager) ListCameras() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.cameras))
	for name := range m.cameras {
		names = append(names, name)
	}
	return names
}

// CameraStatuses returns the status of all managed cameras, sorted by name.
func (m *Manager) CameraStatuses() []CameraStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statuses := make([]CameraStatus, 0, len(m.cameras))
	for _, cam := range m.cameras {
		statuses = append(statuses, cam.Status())
	}
	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].Name < statuses[j].Name
	})
	return statuses
}
