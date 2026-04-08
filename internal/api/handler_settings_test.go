package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rvben/vedetta/internal/config"
	"github.com/rvben/vedetta/internal/detect"
	"github.com/rvben/vedetta/internal/storage"
	"github.com/rvben/vedetta/internal/update"
)

func TestGetMQTTSettings(t *testing.T) {
	srv, _ := newTestServer(t)

	srv.SetMQTTConfig(config.MQTTConfig{
		Enabled:  true,
		Host:     "10.0.0.1",
		Port:     1883,
		Username: "user",
		Topic:    "vedetta",
	})
	srv.mqttEnabled = true

	req := httptest.NewRequest(http.MethodGet, "/api/settings/mqtt", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body["enabled"] != true {
		t.Errorf("expected enabled=true, got %v", body["enabled"])
	}
	if body["host"] != "10.0.0.1" {
		t.Errorf("expected host=10.0.0.1, got %v", body["host"])
	}
	if body["status"] != "disconnected" {
		t.Errorf("expected status=disconnected, got %v", body["status"])
	}
	if _, ok := body["password"]; ok {
		t.Error("password should not be returned in GET response")
	}
}

func TestGetMQTTSettings_Disabled(t *testing.T) {
	srv, _ := newTestServer(t)

	// Default: mqttEnabled=false, mqttClient=nil
	req := httptest.NewRequest(http.MethodGet, "/api/settings/mqtt", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body["status"] != "disabled" {
		t.Errorf("expected status=disabled, got %v", body["status"])
	}
}

func TestUpdateMQTTSettings(t *testing.T) {
	srv, _ := newTestServer(t)

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yml")
	initial := "auth:\n  users:\n    - username: admin\n      password_hash: \"$2a$10$7EqJtq98hPqEX7fNZaFWoOHi8V6I5WJFlQ7Y7S6d6n9zQ0jD4S3yu\"\napi:\n  host: 0.0.0.0\n  port: 5050\n  exposure: lan\n"
	if err := os.WriteFile(cfgPath, []byte(initial), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	srv.SetConfigPath(cfgPath)

	payload := `{"enabled":true,"host":"10.0.0.5","port":1883,"username":"test","password":"secret","topic":"vedetta"}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings/mqtt", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body["host"] != "10.0.0.5" {
		t.Errorf("expected host=10.0.0.5, got %v", body["host"])
	}
	if _, ok := body["password"]; ok {
		t.Error("password should not be returned in response")
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("10.0.0.5")) {
		t.Error("config file should contain the new host")
	}
}

func TestUpdateMQTTSettings_InvalidPort(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.SetConfigPath("/dev/null")

	tests := []struct {
		name    string
		payload string
	}{
		{"zero port", `{"enabled":true,"host":"localhost","port":0,"topic":"vedetta"}`},
		{"negative port", `{"enabled":true,"host":"localhost","port":-1,"topic":"vedetta"}`},
		{"port too large", `{"enabled":true,"host":"localhost","port":65536,"topic":"vedetta"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/api/settings/mqtt", bytes.NewBufferString(tt.payload))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			srv.mux.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for invalid port, got %d", w.Code)
			}

			var body map[string]string
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body["error"] == "" {
				t.Error("expected error message in response")
			}
		})
	}
}

func TestUpdateMQTTSettings_InvalidJSON(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodPut, "/api/settings/mqtt", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestGetUpdateStatus_NoChecker(t *testing.T) {
	srv, _ := newTestServer(t)
	// updateChecker is nil by default

	req := httptest.NewRequest(http.MethodGet, "/api/updates/status", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body["current"] != "test" {
		t.Errorf("expected current=test, got %v", body["current"])
	}
	if body["update_available"] != false {
		t.Errorf("expected update_available=false, got %v", body["update_available"])
	}
}

func TestCheckForUpdates_NoChecker(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/updates/check", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body["current"] != "test" {
		t.Errorf("expected current=test, got %v", body["current"])
	}
	if body["update_available"] != false {
		t.Errorf("expected update_available=false, got %v", body["update_available"])
	}
}

func TestDismissUpdate_NoChecker(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/updates/dismiss", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestCheckForUpdates_WithChecker(t *testing.T) {
	srv, db := newTestServer(t)

	mockGH := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name":"v1.0.0","html_url":"https://github.com/rvben/vedetta/releases/tag/v1.0.0"}`))
	}))
	defer mockGH.Close()

	origURL := update.GithubLatestURL
	update.GithubLatestURL = mockGH.URL
	defer func() { update.GithubLatestURL = origURL }()

	checker := update.New("v0.1.0", 24*time.Hour, db)
	srv.SetUpdateChecker(checker)

	req := httptest.NewRequest(http.MethodGet, "/api/updates/check", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body update.Status
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if !body.UpdateAvailable {
		t.Errorf("expected update_available=true, got false")
	}
	if body.Latest != "v1.0.0" {
		t.Errorf("expected latest=v1.0.0, got %v", body.Latest)
	}
}

func TestGetUpdateStatus_WithChecker(t *testing.T) {
	srv, db := newTestServer(t)

	mockGH := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name":"v2.0.0","html_url":"https://github.com/rvben/vedetta/releases/tag/v2.0.0"}`))
	}))
	defer mockGH.Close()

	origURL := update.GithubLatestURL
	update.GithubLatestURL = mockGH.URL
	defer func() { update.GithubLatestURL = origURL }()

	checker := update.New("v1.0.0", 24*time.Hour, db)
	checker.CheckNow()
	srv.SetUpdateChecker(checker)

	req := httptest.NewRequest(http.MethodGet, "/api/updates/status", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body update.Status
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if !body.UpdateAvailable {
		t.Errorf("expected update_available=true, got false")
	}
	if body.Current != "v1.0.0" {
		t.Errorf("expected current=v1.0.0, got %v", body.Current)
	}
}

func TestDismissUpdate_WithChecker(t *testing.T) {
	srv, db := newTestServer(t)

	mockGH := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name":"v1.1.0","html_url":"https://github.com/rvben/vedetta/releases/tag/v1.1.0"}`))
	}))
	defer mockGH.Close()

	origURL := update.GithubLatestURL
	update.GithubLatestURL = mockGH.URL
	defer func() { update.GithubLatestURL = origURL }()

	checker := update.New("v1.0.0", 24*time.Hour, db)
	checker.CheckNow()
	srv.SetUpdateChecker(checker)

	req := httptest.NewRequest(http.MethodPost, "/api/updates/dismiss", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}

	// Status should now show dismissed=true
	status := checker.Status()
	if !status.Dismissed {
		t.Error("expected update to be dismissed after POST /api/updates/dismiss")
	}
}

func TestDiscoverMQTTBrokers(t *testing.T) {
	// mDNS discovery requires network access and uses zeroconf which may panic
	// in CI environments. Skip unless running integration tests explicitly.
	if testing.Short() {
		t.Skip("skipping mDNS discovery test in short mode")
	}
	t.Skip("skipping mDNS discovery: zeroconf double-close in test environments")
}

func TestGetRecordingSettings(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.SetRecordingConfig(config.RecordingConfig{
		Continuous:    true,
		RetainDays:    7,
		EventRetain:   30,
		SegmentLength: 10 * time.Minute,
		PreCapture:    5 * time.Second,
		PostCapture:   10 * time.Second,
		MaxStorage:    "500GB",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/settings/recording", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var body map[string]any
	json.NewDecoder(w.Body).Decode(&body)
	if body["continuous"] != true {
		t.Errorf("expected continuous=true, got %v", body["continuous"])
	}
	if body["retain_days"] != float64(7) {
		t.Errorf("expected retain_days=7, got %v", body["retain_days"])
	}
	if body["segment_length"] != "10m0s" {
		t.Errorf("expected segment_length=10m0s, got %v", body["segment_length"])
	}
}

func TestUpdateRecordingSettings(t *testing.T) {
	srv, _ := newTestServer(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yml")
	os.WriteFile(cfgPath, []byte("auth:\n  users:\n    - username: admin\n      password_hash: \"$2a$10$7EqJtq98hPqEX7fNZaFWoOHi8V6I5WJFlQ7Y7S6d6n9zQ0jD4S3yu\"\nrecording:\n  path: ./recordings\n  continuous: true\n  retain_days: 7\napi:\n  host: 0.0.0.0\n  port: 5050\n  exposure: lan\n"), 0644)
	srv.SetConfigPath(cfgPath)
	srv.SetRecordingConfig(config.RecordingConfig{
		Path: "./recordings", Continuous: true, RetainDays: 7,
		SegmentLength: 10 * time.Minute, PreCapture: 5 * time.Second, PostCapture: 10 * time.Second,
	})

	payload := `{"continuous":false,"retain_days":14,"event_retain_days":60,"segment_length":"5m","pre_capture":"3s","post_capture":"8s","max_storage":"1TB"}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings/recording", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	json.NewDecoder(w.Body).Decode(&body)
	if body["continuous"] != false {
		t.Errorf("expected continuous=false")
	}
	if body["retain_days"] != float64(14) {
		t.Errorf("expected retain_days=14, got %v", body["retain_days"])
	}
}

func TestUpdateRecordingSettings_InvalidDuration(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.SetConfigPath("/dev/null")
	srv.SetRecordingConfig(config.RecordingConfig{})

	payload := `{"continuous":true,"retain_days":7,"event_retain_days":30,"segment_length":"notaduration","pre_capture":"5s","post_capture":"10s"}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings/recording", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGetDetectSettings_NilDetector(t *testing.T) {
	srv, _ := newTestServer(t)
	// detector is nil by default

	req := httptest.NewRequest(http.MethodGet, "/api/settings/detect", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestUpdateDetectSettings(t *testing.T) {
	srv, _ := newTestServer(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yml")
	os.WriteFile(cfgPath, []byte("auth:\n  users:\n    - username: admin\n      password_hash: \"$2a$10$7EqJtq98hPqEX7fNZaFWoOHi8V6I5WJFlQ7Y7S6d6n9zQ0jD4S3yu\"\ndetect:\n  score_threshold: 0.5\napi:\n  host: 0.0.0.0\n  port: 5050\n  exposure: lan\n"), 0644)
	srv.SetConfigPath(cfgPath)

	d := detect.New(config.DetectConfig{ScoreThreshold: 0.5, Labels: []string{"person"}})
	srv.SetDetector(d)

	payload := `{"score_threshold":0.75,"labels":["person","car"]}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings/detect", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify hot-reload happened
	if d.ScoreThreshold() != 0.75 {
		t.Errorf("expected hot-reloaded threshold 0.75, got %v", d.ScoreThreshold())
	}
}

func TestUpdateDetectSettings_InvalidThreshold(t *testing.T) {
	srv, _ := newTestServer(t)
	d := detect.New(config.DetectConfig{ScoreThreshold: 0.5})
	srv.SetDetector(d)

	payload := `{"score_threshold":1.5,"labels":[]}`
	req := httptest.NewRequest(http.MethodPut, "/api/settings/detect", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// Ensure storage.DB satisfies the settingsStore interface used by the update checker.
// This is a compile-time check that the test helpers wire correctly.
var _ interface {
	GetSetting(key string) (string, error)
	SetSetting(key, value string) error
	DeleteSetting(key string) error
} = (*storage.DB)(nil)
