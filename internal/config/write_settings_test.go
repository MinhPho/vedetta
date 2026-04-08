package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testConfigBase = `auth:
  users:
    - username: admin
      password_hash: "$2a$10$7EqJtq98hPqEX7fNZaFWoOHi8V6I5WJFlQ7Y7S6d6n9zQ0jD4S3yu"
recording:
  path: ./recordings
  continuous: true
  segment_length: 10m
  pre_capture: 5s
  post_capture: 10s
  retain_days: 7
  event_retain_days: 30
detect:
  score_threshold: 0.5
  labels:
    - person
    - car
api:
  host: 0.0.0.0
  port: 5050
  exposure: lan
`

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}
	return path
}

func TestUpdateRecording_RoundTrip(t *testing.T) {
	path := writeTempConfig(t, testConfigBase)

	rec := RecordingConfig{
		Path:          "./recordings",
		Continuous:    false,
		SegmentLength: 5 * time.Minute,
		PreCapture:    10 * time.Second,
		PostCapture:   20 * time.Second,
		RetainDays:    14,
		EventRetain:   60,
		MaxStorage:    "50GB",
	}

	if err := UpdateRecording(path, rec); err != nil {
		t.Fatalf("UpdateRecording: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load after UpdateRecording: %v", err)
	}

	got := cfg.Recording
	if got.Continuous != false {
		t.Errorf("Continuous: got %v, want false", got.Continuous)
	}
	if got.SegmentLength != 5*time.Minute {
		t.Errorf("SegmentLength: got %v, want 5m", got.SegmentLength)
	}
	if got.PreCapture != 10*time.Second {
		t.Errorf("PreCapture: got %v, want 10s", got.PreCapture)
	}
	if got.PostCapture != 20*time.Second {
		t.Errorf("PostCapture: got %v, want 20s", got.PostCapture)
	}
	if got.RetainDays != 14 {
		t.Errorf("RetainDays: got %d, want 14", got.RetainDays)
	}
	if got.EventRetain != 60 {
		t.Errorf("EventRetain: got %d, want 60", got.EventRetain)
	}
	if got.MaxStorage != "50GB" {
		t.Errorf("MaxStorage: got %q, want %q", got.MaxStorage, "50GB")
	}

	// Verify other sections are preserved.
	if cfg.API.Port != 5050 {
		t.Errorf("API.Port: got %d, want 5050 (other sections must be preserved)", cfg.API.Port)
	}
	if len(cfg.Auth.Users) == 0 || cfg.Auth.Users[0].Username != "admin" {
		t.Errorf("Auth.Users: unexpected value (other sections must be preserved)")
	}
}

func TestUpdateDetect_RoundTrip(t *testing.T) {
	path := writeTempConfig(t, testConfigBase)

	detect := DetectConfig{
		ScoreThreshold: 0.75,
		Labels:         []string{"person", "dog", "cat"},
	}

	if err := UpdateDetect(path, detect); err != nil {
		t.Fatalf("UpdateDetect: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load after UpdateDetect: %v", err)
	}

	got := cfg.Detect
	if got.ScoreThreshold != 0.75 {
		t.Errorf("ScoreThreshold: got %v, want 0.75", got.ScoreThreshold)
	}
	if len(got.Labels) != 3 || got.Labels[0] != "person" || got.Labels[1] != "dog" || got.Labels[2] != "cat" {
		t.Errorf("Labels: got %v, want [person dog cat]", got.Labels)
	}

	// Verify other sections are preserved.
	if cfg.Recording.RetainDays != 7 {
		t.Errorf("Recording.RetainDays: got %d, want 7 (other sections must be preserved)", cfg.Recording.RetainDays)
	}
	if cfg.API.Port != 5050 {
		t.Errorf("API.Port: got %d, want 5050 (other sections must be preserved)", cfg.API.Port)
	}
}

func TestUpdateDetect_ClearsLabels(t *testing.T) {
	path := writeTempConfig(t, testConfigBase)

	// Empty labels should remove the labels key from the detect section.
	detect := DetectConfig{
		ScoreThreshold: 0.6,
		Labels:         nil,
	}

	if err := UpdateDetect(path, detect); err != nil {
		t.Fatalf("UpdateDetect: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load after UpdateDetect: %v", err)
	}

	// With no labels set, the default labels from Defaults() apply on Load.
	if cfg.Detect.ScoreThreshold != 0.6 {
		t.Errorf("ScoreThreshold: got %v, want 0.6", cfg.Detect.ScoreThreshold)
	}
}

func TestUpdateCamera_RoundTrip(t *testing.T) {
	path := writeTempConfig(t, testConfigBase)
	enabled := true
	cam := CameraConfig{
		Name:    "front_door",
		URL:     "rtsp://old-url",
		Detect:  DetectStreamConfig{Width: 640, Height: 480, FPS: 5},
		Record:  StreamConfig{Width: 1920, Height: 1080, FPS: 15},
		Enabled: &enabled,
	}
	if err := AppendCamera(path, cam, ""); err != nil {
		t.Fatalf("AppendCamera error: %v", err)
	}
	cam.URL = "rtsp://new-url"
	cam.Name = "front_door_renamed"
	if err := UpdateCamera(path, 0, cam); err != nil {
		t.Fatalf("UpdateCamera error: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if len(cfg.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d", len(cfg.Cameras))
	}
	if cfg.Cameras[0].URL != "rtsp://new-url" {
		t.Errorf("expected new URL, got %s", cfg.Cameras[0].URL)
	}
	if cfg.Cameras[0].Name != "front_door_renamed" {
		t.Errorf("expected renamed, got %s", cfg.Cameras[0].Name)
	}
}

func TestRemoveCamera(t *testing.T) {
	path := writeTempConfig(t, testConfigBase)
	enabled := true
	for _, name := range []string{"cam1", "cam2", "cam3"} {
		if err := AppendCamera(path, CameraConfig{Name: name, URL: "rtsp://" + name, Enabled: &enabled}, ""); err != nil {
			t.Fatalf("AppendCamera %s error: %v", name, err)
		}
	}
	if err := RemoveCamera(path, 1); err != nil {
		t.Fatalf("RemoveCamera error: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if len(cfg.Cameras) != 2 {
		t.Fatalf("expected 2 cameras, got %d", len(cfg.Cameras))
	}
	if cfg.Cameras[0].Name != "cam1" {
		t.Errorf("expected cam1, got %s", cfg.Cameras[0].Name)
	}
	if cfg.Cameras[1].Name != "cam3" {
		t.Errorf("expected cam3, got %s", cfg.Cameras[1].Name)
	}
}

func TestRemoveCamera_InvalidIndex(t *testing.T) {
	path := writeTempConfig(t, testConfigBase)
	err := RemoveCamera(path, 0) // no cameras in base config
	if err == nil {
		t.Fatal("expected error for out-of-bounds index")
	}
}

func TestUpdateAuthPassword(t *testing.T) {
	path := writeTempConfig(t, testConfigBase)
	if err := UpdateAuthPassword(path, "admin", "$2a$10$newhashvalue"); err != nil {
		t.Fatalf("UpdateAuthPassword error: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if len(cfg.Auth.Users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(cfg.Auth.Users))
	}
	if cfg.Auth.Users[0].PasswordHash != "$2a$10$newhashvalue" {
		t.Errorf("expected new hash, got %s", cfg.Auth.Users[0].PasswordHash)
	}
}
