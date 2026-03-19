package api

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"image"
	"image/jpeg"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/rvben/watchpost/internal/camera"
	"github.com/rvben/watchpost/internal/config"
	"github.com/rvben/watchpost/internal/storage"
	"github.com/rvben/watchpost/internal/stream"

	"github.com/pion/webrtc/v4"
)

//go:embed static/*
var staticFiles embed.FS

type Server struct {
	config   config.APIConfig
	db       *storage.DB
	cameras  *camera.Manager
	streams  *stream.StreamManager
	mux      *http.ServeMux
	funcMap  template.FuncMap
}

func New(cfg config.APIConfig, db *storage.DB, cameras *camera.Manager) *Server {
	s := &Server{
		config:  cfg,
		db:      db,
		cameras: cameras,
		streams: stream.NewStreamManager(),
		mux:     http.NewServeMux(),
	}

	s.funcMap = template.FuncMap{
		"timeAgo": func(t time.Time) string {
			d := time.Since(t)
			switch {
			case d < time.Minute:
				return fmt.Sprintf("%ds ago", int(d.Seconds()))
			case d < time.Hour:
				return fmt.Sprintf("%dm ago", int(d.Minutes()))
			case d < 24*time.Hour:
				return fmt.Sprintf("%dh ago", int(d.Hours()))
			default:
				return fmt.Sprintf("%dd ago", int(d.Hours()/24))
			}
		},
		"scorePercent": func(s float32) string {
			return fmt.Sprintf("%.0f%%", s*100)
		},
		"formatTime": func(t time.Time) string {
			return t.Format("2006-01-02 15:04:05")
		},
	}

	// API endpoints
	s.mux.HandleFunc("GET /api/cameras", s.handleListCameras)
	s.mux.HandleFunc("GET /api/cameras/{name}/snapshot", s.handleSnapshot)
	s.mux.HandleFunc("GET /api/events", s.handleListEvents)
	s.mux.HandleFunc("GET /api/events/{id}", s.handleGetEvent)
	s.mux.HandleFunc("GET /api/health", s.handleHealth)

	// Streaming endpoints
	s.mux.HandleFunc("POST /api/cameras/{name}/webrtc/offer", s.handleWebRTCOffer)
	s.mux.HandleFunc("GET /api/cameras/{name}/mjpeg", s.handleMJPEG)

	// HTML partial endpoints for htmx
	s.mux.HandleFunc("GET /partials/camera-grid", s.handleCameraGridPartial)
	s.mux.HandleFunc("GET /partials/events", s.handleEventsPartial)
	s.mux.HandleFunc("GET /partials/event/{id}", s.handleEventDetailPartial)

	// Serve static files at root
	staticSub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		slog.Error("failed to create static sub filesystem", "error", err)
	} else {
		s.mux.Handle("GET /", http.FileServer(http.FS(staticSub)))
	}

	return s
}

func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	slog.Info("API server listening", "addr", addr)
	return http.ListenAndServe(addr, s.mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleListCameras(w http.ResponseWriter, _ *http.Request) {
	names := s.cameras.ListCameras()
	writeJSON(w, http.StatusOK, map[string]any{"cameras": names})
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	cam := s.cameras.GetCamera(name)
	if cam == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "camera not found"})
		return
	}

	img := cam.LastSnapshot()
	if img == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no snapshot available"})
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	if err := jpeg.Encode(w, img, &jpeg.Options{Quality: 85}); err != nil {
		slog.Error("failed to encode snapshot", "error", err)
	}
}

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	cameraFilter := r.URL.Query().Get("camera")
	labelFilter := r.URL.Query().Get("label")
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	events, err := s.db.QueryEvents(cameraFilter, labelFilter, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (s *Server) handleGetEvent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	events, err := s.db.QueryEvents("", "", 0)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	for _, e := range events {
		if e.ID == id {
			writeJSON(w, http.StatusOK, e)
			return
		}
	}

	writeJSON(w, http.StatusNotFound, map[string]string{"error": "event not found"})
}

func (s *Server) handleWebRTCOffer(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	cam := s.cameras.GetCamera(name)
	if cam == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "camera not found"})
		return
	}

	var offer webrtc.SessionDescription
	if err := json.NewDecoder(r.Body).Decode(&offer); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid SDP offer"})
		return
	}

	rtspURL := cam.RecordURL()

	answer, err := s.streams.HandleOffer(name, rtspURL, offer)
	if err != nil {
		slog.Error("WebRTC offer failed", "camera", name, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "WebRTC negotiation failed"})
		return
	}

	writeJSON(w, http.StatusOK, answer)
}

func (s *Server) handleMJPEG(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	cam := s.cameras.GetCamera(name)
	if cam == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "camera not found"})
		return
	}

	handler := stream.MJPEGHandler(func() *image.RGBA {
		return cam.LastSnapshot()
	})

	handler.ServeHTTP(w, r)
}

// HTML partial handlers for htmx

func (s *Server) handleCameraGridPartial(w http.ResponseWriter, _ *http.Request) {
	names := s.cameras.ListCameras()

	type cameraInfo struct {
		Name   string
		Online bool
	}

	cameras := make([]cameraInfo, 0, len(names))
	for _, name := range names {
		cam := s.cameras.GetCamera(name)
		online := cam != nil && cam.LastSnapshot() != nil
		cameras = append(cameras, cameraInfo{Name: name, Online: online})
	}

	tmpl := template.Must(template.New("grid").Parse(`{{range .}}<div class="camera-card" onclick="location.href='/camera.html?name={{.Name}}'">
  <div class="camera-preview">
    <img src="/api/cameras/{{.Name}}/snapshot" alt="{{.Name}}" loading="lazy" onerror="this.style.display='none'">
  </div>
  <div class="camera-info">
    <span class="camera-name">{{.Name}}</span>
    <span class="status-indicator {{if .Online}}online{{else}}offline{{end}}"></span>
  </div>
</div>{{end}}`))

	w.Header().Set("Content-Type", "text/html")
	if err := tmpl.Execute(w, cameras); err != nil {
		slog.Error("template error", "error", err)
	}
}

func (s *Server) handleEventsPartial(w http.ResponseWriter, r *http.Request) {
	cameraFilter := r.URL.Query().Get("camera")
	labelFilter := r.URL.Query().Get("label")
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	events, err := s.db.QueryEvents(cameraFilter, labelFilter, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	tmpl := template.Must(template.New("events").Funcs(s.funcMap).Parse(`{{if not .}}<div class="empty-state"><p>No events recorded yet.</p></div>{{else}}{{range .}}<a class="event-row" href="/event.html?id={{.ID}}">
  <div class="event-thumbnail">
    {{if .SnapshotPath}}<img src="/api/cameras/{{.CameraName}}/snapshot" alt="event" loading="lazy">{{else}}<div class="no-thumb"></div>{{end}}
  </div>
  <div class="event-details">
    <span class="event-label">{{.Label}}</span>
    <span class="event-score">{{scorePercent .Score}}</span>
    <span class="event-camera">{{.CameraName}}</span>
    <span class="event-time">{{timeAgo .Timestamp}}</span>
  </div>
</a>{{end}}{{end}}`))

	w.Header().Set("Content-Type", "text/html")
	if err := tmpl.Execute(w, events); err != nil {
		slog.Error("template error", "error", err)
	}
}

func (s *Server) handleEventDetailPartial(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	events, err := s.db.QueryEvents("", "", 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var event *camera.Event
	for _, e := range events {
		if e.ID == id {
			event = &e
			break
		}
	}

	if event == nil {
		http.Error(w, "event not found", http.StatusNotFound)
		return
	}

	tmpl := template.Must(template.New("detail").Funcs(s.funcMap).Parse(`<div class="event-detail-content">
  <div class="event-snapshot-container">
    <img src="/api/cameras/{{.CameraName}}/snapshot" alt="event snapshot" class="event-snapshot-img">
  </div>
  {{if .ClipPath}}<div class="event-video-container">
    <video controls class="event-video">
      <source src="{{.ClipPath}}" type="video/mp4">
    </video>
  </div>{{end}}
  <dl class="event-meta">
    <dt>Camera</dt><dd>{{.CameraName}}</dd>
    <dt>Label</dt><dd>{{.Label}}</dd>
    <dt>Confidence</dt><dd>{{scorePercent .Score}}</dd>
    <dt>Time</dt><dd>{{formatTime .Timestamp}}</dd>
    <dt>Event ID</dt><dd class="mono">{{.ID}}</dd>
  </dl>
</div>`))

	w.Header().Set("Content-Type", "text/html")
	if err := tmpl.Execute(w, event); err != nil {
		slog.Error("template error", "error", err)
	}
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("failed to write JSON response", "error", err)
	}
}
