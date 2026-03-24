package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteInitialConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")

	err := WriteInitialConfig(path, "admin", "$2a$10$abc123hashedpassword")
	if err != nil {
		t.Fatalf("WriteInitialConfig() error: %v", err)
	}

	// Verify file permissions
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("file permissions = %o, want 0600", perm)
	}

	// Load it back and verify
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if len(cfg.Auth.Users) != 1 {
		t.Fatalf("expected 1 auth user, got %d", len(cfg.Auth.Users))
	}
	if cfg.Auth.Users[0].Username != "admin" {
		t.Errorf("username = %q, want %q", cfg.Auth.Users[0].Username, "admin")
	}
	if cfg.Auth.Users[0].PasswordHash != "$2a$10$abc123hashedpassword" {
		t.Errorf("password_hash = %q, want %q", cfg.Auth.Users[0].PasswordHash, "$2a$10$abc123hashedpassword")
	}
	if cfg.API.Port != 5050 {
		t.Errorf("api port = %d, want 5050", cfg.API.Port)
	}
	if cfg.API.Host != "0.0.0.0" {
		t.Errorf("api host = %q, want %q", cfg.API.Host, "0.0.0.0")
	}
	if cfg.Detect.ScoreThreshold != 0.65 {
		t.Errorf("score_threshold = %f, want 0.65", cfg.Detect.ScoreThreshold)
	}
}

func TestAppendCamera(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")

	err := WriteInitialConfig(path, "admin", "$2a$10$abc123hashedpassword")
	if err != nil {
		t.Fatalf("WriteInitialConfig() error: %v", err)
	}

	cam := CameraConfig{
		Name: "front_door",
		URL:  "rtsp://192.168.1.100/stream",
	}
	err = AppendCamera(path, cam, "Front door camera")
	if err != nil {
		t.Fatalf("AppendCamera() error: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if len(cfg.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d", len(cfg.Cameras))
	}
	if cfg.Cameras[0].Name != "front_door" {
		t.Errorf("camera name = %q, want %q", cfg.Cameras[0].Name, "front_door")
	}
	if cfg.Cameras[0].URL != "rtsp://192.168.1.100/stream" {
		t.Errorf("camera url = %q, want %q", cfg.Cameras[0].URL, "rtsp://192.168.1.100/stream")
	}

	// Verify auth is preserved
	if len(cfg.Auth.Users) != 1 {
		t.Fatalf("expected 1 auth user after append, got %d", len(cfg.Auth.Users))
	}
	if cfg.Auth.Users[0].Username != "admin" {
		t.Errorf("auth username = %q, want %q", cfg.Auth.Users[0].Username, "admin")
	}
}

func TestAppendCamera_Multiple(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")

	err := WriteInitialConfig(path, "admin", "$2a$10$abc123hashedpassword")
	if err != nil {
		t.Fatalf("WriteInitialConfig() error: %v", err)
	}

	cam1 := CameraConfig{
		Name: "front_door",
		URL:  "rtsp://192.168.1.100/stream",
	}
	err = AppendCamera(path, cam1, "Front door camera")
	if err != nil {
		t.Fatalf("AppendCamera(cam1) error: %v", err)
	}

	cam2 := CameraConfig{
		Name: "backyard",
		URL:  "rtsp://192.168.1.101/stream",
	}
	err = AppendCamera(path, cam2, "Backyard camera")
	if err != nil {
		t.Fatalf("AppendCamera(cam2) error: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if len(cfg.Cameras) != 2 {
		t.Fatalf("expected 2 cameras, got %d", len(cfg.Cameras))
	}
	if cfg.Cameras[0].Name != "front_door" {
		t.Errorf("camera[0] name = %q, want %q", cfg.Cameras[0].Name, "front_door")
	}
	if cfg.Cameras[1].Name != "backyard" {
		t.Errorf("camera[1] name = %q, want %q", cfg.Cameras[1].Name, "backyard")
	}
}

func TestGenerateInitialConfigYAML(t *testing.T) {
	yaml, err := GenerateInitialConfigYAML("myuser", "$2a$10$somehash")
	if err != nil {
		t.Fatalf("GenerateInitialConfigYAML() error: %v", err)
	}

	if !strings.Contains(yaml, "myuser") {
		t.Error("YAML should contain username")
	}
	if !strings.Contains(yaml, "$2a$10$somehash") {
		t.Error("YAML should contain password_hash")
	}
	if !strings.Contains(yaml, "port: 5050") {
		t.Error("YAML should contain port: 5050")
	}
	if !strings.Contains(yaml, "score_threshold: 0.65") {
		t.Error("YAML should contain score_threshold: 0.65")
	}
	// Verify durations are human-readable, not nanoseconds
	if strings.Contains(yaml, "600000000000") {
		t.Error("YAML should not contain nanosecond values for durations")
	}
	if !strings.Contains(yaml, "10m") {
		t.Error("YAML should contain human-readable duration '10m'")
	}
}
