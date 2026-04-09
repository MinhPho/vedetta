package camera

import (
	"context"
	"testing"
	"time"

	"github.com/rvben/vedetta/internal/config"
)

func TestStopCamera(t *testing.T) {
	m := &Manager{
		cameras:     make(map[string]*Camera),
		cancelFuncs: make(map[string]context.CancelFunc),
		order:       []string{"test-cam"},
	}
	m.cameras["test-cam"] = &Camera{config: config.CameraConfig{Name: "test-cam"}}

	if err := m.StopCamera("nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent camera")
	}

	if err := m.StopCamera("test-cam"); err == nil {
		t.Fatal("expected error for camera without cancel func")
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.cancelFuncs["test-cam"] = cancel

	if err := m.StopCamera("test-cam"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("context not cancelled after StopCamera")
	}

	if _, ok := m.cancelFuncs["test-cam"]; ok {
		t.Fatal("cancel func should be removed after StopCamera")
	}

	if err := m.StopCamera("test-cam"); err == nil {
		t.Fatal("expected error for already-stopped camera")
	}
}

func TestIsStopped(t *testing.T) {
	m := &Manager{
		cameras:     make(map[string]*Camera),
		cancelFuncs: make(map[string]context.CancelFunc),
		order:       []string{"cam1"},
	}
	m.cameras["cam1"] = &Camera{config: config.CameraConfig{Name: "cam1"}}

	if !m.IsStopped("cam1") {
		t.Fatal("camera without cancel func should be stopped")
	}

	_, cancel := context.WithCancel(context.Background())
	m.cancelFuncs["cam1"] = cancel

	if m.IsStopped("cam1") {
		t.Fatal("camera with cancel func should not be stopped")
	}

	if m.IsStopped("nonexistent") {
		t.Fatal("nonexistent camera should not report as stopped")
	}
}
