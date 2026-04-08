package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/rvben/vedetta/internal/config"
)

func newTestServerWithCameras(t *testing.T) (*Server, string) {
	t.Helper()
	srv, _ := newTestServer(t)

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yml")
	os.WriteFile(cfgPath, []byte("auth:\n  users:\n    - username: admin\n      password_hash: \"$2a$10$7EqJtq98hPqEX7fNZaFWoOHi8V6I5WJFlQ7Y7S6d6n9zQ0jD4S3yu\"\napi:\n  host: 0.0.0.0\n  port: 5050\n  exposure: lan\n"), 0644)
	srv.SetConfigPath(cfgPath)

	enabled := true
	srv.cameraConfigs = []config.CameraConfig{
		{Name: "front", URL: "rtsp://front", Enabled: &enabled},
		{Name: "back", URL: "rtsp://back", Enabled: &enabled},
	}
	for _, cam := range srv.cameraConfigs {
		config.AppendCamera(cfgPath, cam, "")
	}

	return srv, cfgPath
}

func TestListCamerasManage(t *testing.T) {
	srv, _ := newTestServerWithCameras(t)

	req := httptest.NewRequest(http.MethodGet, "/api/cameras/manage", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var body map[string]any
	json.NewDecoder(w.Body).Decode(&body)
	cameras := body["cameras"].([]any)
	if len(cameras) != 2 {
		t.Fatalf("expected 2 cameras, got %d", len(cameras))
	}
	if body["restart_required"] != false {
		t.Error("expected restart_required=false initially")
	}
}

func TestAddCameraManage(t *testing.T) {
	srv, _ := newTestServerWithCameras(t)

	payload := `{"name":"garage","url":"rtsp://garage","enabled":true,"detect":{"width":640,"height":480,"fps":5},"record":{"width":1920,"height":1080,"fps":15}}`
	req := httptest.NewRequest(http.MethodPost, "/api/cameras/manage", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(srv.cameraConfigs) != 3 {
		t.Fatalf("expected 3 cameras, got %d", len(srv.cameraConfigs))
	}
	if !srv.restartRequired {
		t.Error("expected restartRequired=true after add")
	}
}

func TestUpdateCameraManage(t *testing.T) {
	srv, _ := newTestServerWithCameras(t)

	payload := `{"name":"front_updated","url":"rtsp://front-new","enabled":true,"detect":{"width":640,"height":480,"fps":5},"record":{"width":1920,"height":1080,"fps":15}}`
	req := httptest.NewRequest(http.MethodPut, "/api/cameras/manage/0", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if srv.cameraConfigs[0].Name != "front_updated" {
		t.Errorf("expected front_updated, got %s", srv.cameraConfigs[0].Name)
	}
}

func TestRemoveCameraManage(t *testing.T) {
	srv, _ := newTestServerWithCameras(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/cameras/manage/0", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(srv.cameraConfigs) != 1 {
		t.Fatalf("expected 1 camera, got %d", len(srv.cameraConfigs))
	}
	if srv.cameraConfigs[0].Name != "back" {
		t.Errorf("expected back, got %s", srv.cameraConfigs[0].Name)
	}
}

func TestRemoveCameraManage_InvalidIndex(t *testing.T) {
	srv, _ := newTestServerWithCameras(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/cameras/manage/99", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestAddCameraManage_InvalidJSON(t *testing.T) {
	srv, _ := newTestServerWithCameras(t)

	req := httptest.NewRequest(http.MethodPost, "/api/cameras/manage", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAddCameraManage_MissingURL(t *testing.T) {
	srv, _ := newTestServerWithCameras(t)

	payload := `{"name":"test","url":"","enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/cameras/manage", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
