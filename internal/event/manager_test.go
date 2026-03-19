package event

import (
	"image"
	"testing"
	"time"

	"github.com/rvben/watchpost/internal/detect"
)

func newTestManager(cooldownSec int) *Manager {
	return NewManager(Config{
		CooldownSeconds: cooldownSec,
		SnapshotPath:    "/tmp/watchpost-test-snapshots",
		SnapshotQuality: 50,
	})
}

func testDetection(label string, score float32) detect.Detection {
	return detect.Detection{
		Label: label,
		Score: score,
		Box:   [4]int{10, 20, 100, 200},
	}
}

func testFrame() *image.RGBA {
	return image.NewRGBA(image.Rect(0, 0, 640, 480))
}

func TestCooldownSuppressesDuplicateEvents(t *testing.T) {
	m := newTestManager(30)

	d := testDetection("person", 0.9)

	// First detection should create an event
	event1 := m.ProcessDetection("front_door", d, nil)
	if event1 == nil {
		t.Fatal("expected first detection to create an event")
	}
	if event1.Label != "person" {
		t.Errorf("expected label 'person', got %s", event1.Label)
	}
	if event1.CameraName != "front_door" {
		t.Errorf("expected camera 'front_door', got %s", event1.CameraName)
	}

	// Second detection (same label+camera) within cooldown should be suppressed
	event2 := m.ProcessDetection("front_door", d, nil)
	if event2 != nil {
		t.Fatal("expected second detection within cooldown to be suppressed")
	}
}

func TestCooldownAllowsEventsAfterExpiry(t *testing.T) {
	m := newTestManager(1) // 1 second cooldown

	d := testDetection("person", 0.85)

	event1 := m.ProcessDetection("cam1", d, nil)
	if event1 == nil {
		t.Fatal("expected first event to be created")
	}

	// Wait for cooldown to expire
	time.Sleep(1100 * time.Millisecond)

	event2 := m.ProcessDetection("cam1", d, nil)
	if event2 == nil {
		t.Fatal("expected event after cooldown expiry")
	}

	if event1.ID == event2.ID {
		t.Error("expected different event IDs")
	}
}

func TestDifferentLabelsDontInterfere(t *testing.T) {
	m := newTestManager(30)

	person := testDetection("person", 0.9)
	car := testDetection("car", 0.8)

	event1 := m.ProcessDetection("cam1", person, nil)
	if event1 == nil {
		t.Fatal("expected person event")
	}

	// Car detection on same camera should not be suppressed
	event2 := m.ProcessDetection("cam1", car, nil)
	if event2 == nil {
		t.Fatal("expected car event (different label should not be suppressed)")
	}

	if event2.Label != "car" {
		t.Errorf("expected label 'car', got %s", event2.Label)
	}

	// Another person should be suppressed
	event3 := m.ProcessDetection("cam1", person, nil)
	if event3 != nil {
		t.Fatal("expected second person to be suppressed by cooldown")
	}
}

func TestDifferentCamerasDontInterfere(t *testing.T) {
	m := newTestManager(30)

	d := testDetection("person", 0.9)

	event1 := m.ProcessDetection("front_door", d, nil)
	if event1 == nil {
		t.Fatal("expected event on front_door")
	}

	// Same label on different camera should not be suppressed
	event2 := m.ProcessDetection("backyard", d, nil)
	if event2 == nil {
		t.Fatal("expected event on backyard (different camera should not be suppressed)")
	}

	if event2.CameraName != "backyard" {
		t.Errorf("expected camera 'backyard', got %s", event2.CameraName)
	}
}

func TestProcessDetectionsBatchFiltering(t *testing.T) {
	m := newTestManager(30)

	detections := []detect.Detection{
		testDetection("person", 0.9),
		testDetection("car", 0.8),
		testDetection("dog", 0.7),
	}

	events := m.ProcessDetections("cam1", detections, nil)
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}

	// Second batch on same camera should all be suppressed
	events2 := m.ProcessDetections("cam1", detections, nil)
	if len(events2) != 0 {
		t.Fatalf("expected 0 events (all suppressed), got %d", len(events2))
	}
}

func TestSnapshotGeneration(t *testing.T) {
	m := newTestManager(30)

	d := testDetection("person", 0.9)
	frame := testFrame()

	event := m.ProcessDetection("cam1", d, frame)
	if event == nil {
		t.Fatal("expected event")
	}

	if event.SnapshotPath == "" {
		t.Error("expected snapshot path to be set when frame is provided")
	}
}

func TestNoSnapshotWithoutFrame(t *testing.T) {
	m := newTestManager(30)

	d := testDetection("person", 0.9)

	event := m.ProcessDetection("cam1", d, nil)
	if event == nil {
		t.Fatal("expected event")
	}

	if event.SnapshotPath != "" {
		t.Error("expected empty snapshot path when no frame provided")
	}
}

func TestCleanupCooldowns(t *testing.T) {
	m := newTestManager(1)

	d := testDetection("person", 0.9)
	m.ProcessDetection("cam1", d, nil)
	m.ProcessDetection("cam2", d, nil)

	if len(m.cooldowns) != 2 {
		t.Fatalf("expected 2 cooldown entries, got %d", len(m.cooldowns))
	}

	// Wait for cooldowns to expire, then cleanup
	time.Sleep(2100 * time.Millisecond)
	m.CleanupCooldowns()

	if len(m.cooldowns) != 0 {
		t.Errorf("expected 0 cooldown entries after cleanup, got %d", len(m.cooldowns))
	}
}

func TestDefaultConfig(t *testing.T) {
	m := NewManager(Config{})

	if m.config.CooldownSeconds != 30 {
		t.Errorf("expected default cooldown 30, got %d", m.config.CooldownSeconds)
	}
	if m.config.SnapshotPath != "./snapshots" {
		t.Errorf("expected default snapshot path, got %s", m.config.SnapshotPath)
	}
	if m.config.SnapshotQuality != 85 {
		t.Errorf("expected default quality 85, got %d", m.config.SnapshotQuality)
	}
}

func TestEventIDsAreUnique(t *testing.T) {
	m := newTestManager(0) // no cooldown to allow rapid events

	// With cooldown=0, it defaults to 30, so use different labels
	labels := []string{"person", "car", "dog", "cat", "bird"}
	seen := make(map[string]bool)

	for _, label := range labels {
		d := testDetection(label, 0.9)
		event := m.ProcessDetection("cam1", d, nil)
		if event == nil {
			t.Fatalf("expected event for %s", label)
		}
		if seen[event.ID] {
			t.Errorf("duplicate event ID: %s", event.ID)
		}
		seen[event.ID] = true
	}
}

func TestEventFieldsAreCorrect(t *testing.T) {
	m := newTestManager(30)

	d := detect.Detection{
		Label: "person",
		Score: 0.87,
		Box:   [4]int{50, 100, 200, 300},
	}

	event := m.ProcessDetection("backyard", d, nil)
	if event == nil {
		t.Fatal("expected event")
	}

	if event.CameraName != "backyard" {
		t.Errorf("wrong camera: %s", event.CameraName)
	}
	if event.Label != "person" {
		t.Errorf("wrong label: %s", event.Label)
	}
	if event.Score != 0.87 {
		t.Errorf("wrong score: %f", event.Score)
	}
	if event.Box != [4]int{50, 100, 200, 300} {
		t.Errorf("wrong box: %v", event.Box)
	}
	if event.Timestamp.IsZero() {
		t.Error("timestamp should not be zero")
	}
}
