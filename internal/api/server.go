package api

import (
	"context"
	"crypto/tls"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rvben/vedetta/internal/auth"
	"github.com/rvben/vedetta/internal/camera"
	"github.com/rvben/vedetta/internal/config"
	"github.com/rvben/vedetta/internal/detect"
	"github.com/rvben/vedetta/internal/recording"
	"github.com/rvben/vedetta/internal/rtsp"
	"github.com/rvben/vedetta/internal/storage"
	"github.com/rvben/vedetta/internal/stream"
)

//go:embed static/*
var staticFiles embed.FS

var startTime = time.Now()

// MQTTPublisher is the subset of mqtt.Client used by the API server.
type MQTTPublisher interface {
	PublishSnapshot(cameraName, label string, jpegData []byte)
	PublishDoorbell(cameraName, person string, jpegData []byte)
}

type Server struct {
	config         config.APIConfig
	auth           *auth.Checker
	db             *storage.DB
	cameras        *camera.Manager
	recorder       *recording.Recorder
	hub            *rtsp.Hub
	streams        *stream.StreamManager
	mse            *stream.MSEManager
	faceRecognizer *detect.FaceRecognizer
	objectEmbedder       *detect.ObjectEmbedder
	ObjectMatchThreshold float64
	mqttClient           MQTTPublisher
	mqttEnabled          bool
	hlsSegmentCache      sync.Map // map[string][]media.HLSSegmentRef — keyed by "camera:segID"
	snapshotPath         string
	faceCropDir    string
	ptzClients     map[string]*camera.PTZClient
	cameraConfigs  []config.CameraConfig
	httpSrv        *http.Server
	mux            *http.ServeMux
	funcMap        template.FuncMap
	ready          atomic.Bool
	setupHandler   *SetupHandler
	setupMode      bool

	// SSE event bus for real-time browser notifications
	sseMu      sync.Mutex
	sseClients map[chan []byte]struct{}

	// ctx is the application lifetime context (cancelled on shutdown).
	ctx context.Context
}

func New(cfg config.APIConfig, authChecker *auth.Checker, db *storage.DB) *Server {
	s := &Server{
		config: cfg,
		auth:   authChecker,
		db:     db,
		mux:        http.NewServeMux(),
		sseClients: make(map[chan []byte]struct{}),
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
		"toFloat32": func(f float64) float32 { return float32(f) },
		"formatTime": func(t time.Time) template.HTML {
			iso := t.UTC().Format(time.RFC3339)
			display := t.UTC().Format("2006-01-02 15:04:05 UTC")
			return template.HTML(fmt.Sprintf(`<time datetime="%s">%s</time>`, iso, display))
		},
		"formatBytes": formatBytes,
		"displayName": displayName,
		"eventDuration": func(e camera.Event) string {
			if e.EndTime.IsZero() {
				return ""
			}
			d := e.EndTime.Sub(e.Timestamp)
			if d < time.Second {
				return ""
			}
			if d < time.Minute {
				return fmt.Sprintf("%ds", int(d.Seconds()))
			}
			return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
		},
	}

	s.registerRoutes()

	return s
}

// NewSetupMode creates a Server that only serves setup/onboarding endpoints.
// No auth middleware is applied. The setupDone channel is closed when setup completes.
func NewSetupMode(cfg config.APIConfig, db *storage.DB, configPath string, setupDone chan struct{}) *Server {
	s := &Server{
		config:     cfg,
		db:         db,
		mux:        http.NewServeMux(),
		sseClients: make(map[chan []byte]struct{}),
		setupMode:  true,
	}

	sh := NewSetupHandler(configPath, db, setupDone)
	s.setupHandler = sh

	// Setup-only routes (no auth middleware)
	s.mux.HandleFunc("POST /api/setup", sh.HandleSetup)
	s.mux.HandleFunc("GET /api/discover", sh.HandleDiscover)
	s.mux.HandleFunc("POST /api/discover/probe", sh.HandleProbe)
	s.mux.HandleFunc("GET /api/discover/thumbnail/{ip}", sh.HandleThumbnail)
	s.mux.HandleFunc("POST /api/cameras", sh.HandleAddCameras)
	s.mux.HandleFunc("POST /api/setup/complete", sh.HandleComplete)
	s.mux.HandleFunc("GET /api/setup/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "setup"})
	})

	// Serve setup.html as default page
	staticSub, _ := fs.Sub(staticFiles, "static")
	fileServer := http.FileServer(http.FS(staticSub))
	s.mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			r.URL.Path = "/setup.html"
		}
		fileServer.ServeHTTP(w, r)
	})

	// Catch-all: block non-setup API routes.
	// Uses GET and POST since those are the only methods not already covered by
	// setup-specific handlers above; this avoids mux conflict with "GET /".
	blockSetup := func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "setup not complete"})
	}
	s.mux.HandleFunc("GET /api/", blockSetup)
	s.mux.HandleFunc("POST /api/", blockSetup)
	s.mux.HandleFunc("DELETE /api/", blockSetup)
	s.mux.HandleFunc("PUT /api/", blockSetup)
	s.mux.HandleFunc("PATCH /api/", blockSetup)

	return s
}

// registerRoutes registers all application routes on s.mux.
// Called from New() and TransitionToFull().
func (s *Server) registerRoutes() {
	s.mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	s.mux.HandleFunc("GET /api/auth/me", s.handleAuthMe)
	s.mux.HandleFunc("POST /api/auth/change-password", s.handleChangePassword)
	s.mux.HandleFunc("GET /api/tokens", s.handleListTokens)
	s.mux.HandleFunc("POST /api/tokens", s.handleCreateToken)
	s.mux.HandleFunc("DELETE /api/tokens/{id}", s.handleDeleteToken)

	// API endpoints
	s.mux.HandleFunc("GET /api/cameras", s.handleListCameras)
	s.mux.HandleFunc("GET /api/cameras/{name}", s.handleGetCamera)
	s.mux.HandleFunc("GET /api/cameras/{name}/snapshot", s.handleSnapshot)
	s.mux.HandleFunc("GET /api/events", s.handleListEvents)
	s.mux.HandleFunc("GET /api/events/{id}", s.handleGetEvent)
	s.mux.HandleFunc("GET /api/events/{id}/snapshot", s.handleEventSnapshot)
	s.mux.HandleFunc("GET /api/events/{id}/clip", s.handleEventClip)
	s.mux.HandleFunc("POST /api/events/{id}/clip", s.handleReextractClip)
	s.mux.HandleFunc("GET /api/events/counts", s.handleEventCounts)
	s.mux.HandleFunc("GET /api/openapi.json", s.handleOpenAPISpec)
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/health/live", s.handleHealthLive)
	s.mux.HandleFunc("GET /api/health/ready", s.handleHealthReady)
	s.mux.HandleFunc("GET /api/system", s.handleSystemAPI)
	s.mux.HandleFunc("GET /metrics", s.handleMetrics)

	s.mux.HandleFunc("GET /api/recordings/calendar", s.handleRecordingsCalendar)
	s.mux.HandleFunc("GET /api/recordings/summary", s.handleRecordingsSummary)

	s.mux.HandleFunc("GET /api/cameras/{name}/timeline", s.handleCameraTimeline)
	// Old progressive MP4 playback endpoint removed — replaced by HLS m3u8
	s.mux.HandleFunc("GET /api/cameras/{name}/playback.m3u8", s.handlePlaybackM3U8)
	s.mux.HandleFunc("GET /api/cameras/{name}/segments/{id}", s.handleSegment)
	s.mux.HandleFunc("GET /api/cameras/{name}/segments/{id}/hls/init.mp4", s.handleSegmentInit)
	s.mux.HandleFunc("GET /api/cameras/{name}/segments/{id}/hls/{segNum}", s.handleSegmentHLS)
	s.mux.HandleFunc("GET /api/cameras/{name}/thumbnail", s.handleThumbnail)
	s.mux.HandleFunc("GET /api/recordings/segments/{camera}", s.handleListSegments)
	s.mux.HandleFunc("GET /api/recordings/export/{camera}", s.handleRecordingExport)

	// Zone endpoints
	s.mux.HandleFunc("GET /api/cameras/{name}/zones/snapshot", s.handleSnapshot) // reuse camera snapshot for zone overlay background
	s.mux.HandleFunc("GET /api/cameras/{name}/zones", s.handleListZones)
	s.mux.HandleFunc("POST /api/cameras/{name}/zones", s.handleCreateZone)
	s.mux.HandleFunc("PUT /api/cameras/{name}/zones/{zone}", s.handleUpdateZone)
	s.mux.HandleFunc("DELETE /api/cameras/{name}/zones/{zone}", s.handleDeleteZone)
	s.mux.HandleFunc("GET /api/cameras/{name}/zones/{zone}/presence", s.handleZonePresence)

	// People/Face endpoints
	s.mux.HandleFunc("GET /api/people", s.handleListPeople)
	s.mux.HandleFunc("GET /api/people/{id}", s.handleGetPerson)
	s.mux.HandleFunc("PUT /api/people/{id}", s.handleUpdatePerson)
	s.mux.HandleFunc("DELETE /api/people/{id}", s.handleDeletePerson)
	s.mux.HandleFunc("GET /api/people/{id}/faces", s.handleListPersonFaces)
	s.mux.HandleFunc("GET /api/people/{id}/events", s.handleListPersonEvents)
	s.mux.HandleFunc("GET /api/faces/unmatched", s.handleListUnmatchedFaces)
	s.mux.HandleFunc("PUT /api/faces/{id}/assign", s.handleAssignFace)
	s.mux.HandleFunc("GET /api/faces/{id}/crop", s.handleFaceCrop)
	s.mux.HandleFunc("POST /api/faces/{id}/ignore", s.handleIgnoreFace)
	s.mux.HandleFunc("POST /api/faces/backfill", s.handleFaceBackfill)
	s.mux.HandleFunc("POST /api/people/merge", s.handleMergePeople)

	// Object re-identification
	s.mux.HandleFunc("GET /api/objects", s.handleListObjects)
	s.mux.HandleFunc("POST /api/objects", s.handleCreateObject)
	s.mux.HandleFunc("PUT /api/objects/{id}", s.handleUpdateObject)
	s.mux.HandleFunc("DELETE /api/objects/{id}", s.handleDeleteObject)
	s.mux.HandleFunc("GET /api/objects/{id}/sightings", s.handleObjectSightings)
	s.mux.HandleFunc("GET /api/objects/{id}/crop", s.handleObjectCrop)
	s.mux.HandleFunc("GET /api/objects/{id}/references", s.handleObjectReferences)
	s.mux.HandleFunc("POST /api/objects/{id}/references", s.handleAddObjectReference)
	s.mux.HandleFunc("DELETE /api/objects/references/{id}", s.handleDeleteObjectReference)
	s.mux.HandleFunc("DELETE /api/objects/sightings/{id}", s.handleDismissSighting)
	s.mux.HandleFunc("POST /api/events/{id}/identify", s.handleIdentifyEvent)

	s.mux.HandleFunc("GET /api/events/{id}/detection-crop", s.handleEventDetectionCrop)
	s.mux.HandleFunc("POST /api/events/{id}/track-person", s.handleTrackPerson)
	s.mux.HandleFunc("POST /api/events/{id}/assign-person", s.handleAssignPersonToEvent)

	// Doorbell + real-time events
	s.mux.HandleFunc("POST /api/cameras/{name}/doorbell", s.handleDoorbellPress)
	s.mux.HandleFunc("POST /api/cameras/{name}/ptz", s.handlePTZ)
	s.mux.HandleFunc("GET /api/events/stream", s.handleSSE)

	// Streaming endpoints
	s.mux.HandleFunc("POST /api/cameras/{name}/webrtc/offer", s.handleWebRTCOffer)
	s.mux.HandleFunc("GET /api/cameras/{name}/mse/ws", s.handleMSEWebSocket)
	s.mux.HandleFunc("GET /api/cameras/{name}/mjpeg", s.handleMJPEG)

	// HTML partial endpoints for htmx
	s.mux.HandleFunc("GET /partials/camera-grid", s.handleCameraGridPartial)
	s.mux.HandleFunc("GET /partials/dashboard-stats", s.handleDashboardStatsPartial)
	s.mux.HandleFunc("GET /partials/events-gallery", s.handleEventsGalleryPartial)
	s.mux.HandleFunc("GET /partials/event/{id}", s.handleEventDetailPartial)
	s.mux.HandleFunc("GET /partials/system-status", s.handleSystemStatusPartial)
	s.mux.HandleFunc("GET /partials/system", s.handleSystemPartial)
	s.mux.HandleFunc("POST /api/system/recompress/trigger", s.handleRecompressTrigger)

	// Setup status endpoint (returns "running" in normal mode)
	s.mux.HandleFunc("GET /api/setup/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "running"})
	})

	// Serve static files at root
	staticSub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		slog.Error("failed to create static sub filesystem", "error", err)
	} else {
		s.mux.Handle("GET /", http.FileServer(http.FS(staticSub)))
	}
}

// SetContext sets the application lifetime context used for background operations
// triggered by API requests (e.g. manual recompression).
func (s *Server) SetContext(ctx context.Context) {
	s.ctx = ctx
}

func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)

	var handler http.Handler = s.mux
	if !s.setupMode {
		handler = s.readyMiddleware(authMiddleware(s, s.mux))
	}

	s.httpSrv = &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	if s.config.TLSCert != "" && s.config.TLSKey != "" {
		s.httpSrv.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
		slog.Info("API server listening (HTTPS)", "addr", addr)
		return s.httpSrv.ListenAndServeTLS(s.config.TLSCert, s.config.TLSKey)
	}

	slog.Info("API server listening", "addr", addr)
	return s.httpSrv.ListenAndServe()
}

func (s *Server) SetMQTT(publisher MQTTPublisher) {
	s.mqttClient = publisher
	s.mqttEnabled = true
}

func (s *Server) SetMQTTEnabled(enabled bool) {
	s.mqttEnabled = enabled
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpSrv == nil {
		return nil
	}
	return s.httpSrv.Shutdown(ctx)
}

func (s *Server) TransitionToFull(authChecker *auth.Checker) {
	s.auth = authChecker
	s.setupMode = false

	newMux := http.NewServeMux()
	s.mux = newMux
	s.registerRoutes()

	s.httpSrv.Handler = s.readyMiddleware(authMiddleware(s, newMux))
}

func (s *Server) SetSubsystems(cameras *camera.Manager, recorder *recording.Recorder, hub *rtsp.Hub, faceRecognizer *detect.FaceRecognizer, objectEmbedder *detect.ObjectEmbedder, snapshotPath string, faceCropDir string, cameraConfigs []config.CameraConfig, ptzClients map[string]*camera.PTZClient) {
	s.cameras = cameras
	s.recorder = recorder
	s.hub = hub
	s.streams = stream.NewStreamManager(hub)
	s.mse = stream.NewMSEManager(hub)
	s.faceRecognizer = faceRecognizer
	s.objectEmbedder = objectEmbedder
	s.snapshotPath = snapshotPath
	s.faceCropDir = faceCropDir
	s.cameraConfigs = cameraConfigs
	s.ptzClients = ptzClients
	s.ready.Store(true)
	slog.Info("API server ready (all subsystems initialized)")
}

// readyMiddleware serves static files immediately but returns 503 for API/partial
// endpoints until subsystems are initialized.
func (s *Server) readyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.ready.Load() && (strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/partials/")) {
			// Return JSON for API, HTML for partials
			if strings.HasPrefix(r.URL.Path, "/api/") {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "5")
				w.WriteHeader(http.StatusServiceUnavailable)
				w.Write([]byte(`{"status":"starting","message":"Vedetta is initializing..."}`))
			} else {
				w.Header().Set("Content-Type", "text/html")
				w.Header().Set("Retry-After", "5")
				w.WriteHeader(http.StatusServiceUnavailable)
				w.Write([]byte(`<div class="empty-state"><p>Vedetta is starting up...</p></div>`))
			}
			return
		}
		next.ServeHTTP(w, r)
	})
}

func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)
	switch {
	case bytes >= TB:
		return fmt.Sprintf("%.1f TB", float64(bytes)/float64(TB))
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func displayName(name string) string {
	parts := strings.Split(name, "_")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}

func (s *Server) cameraStatuses() []camera.CameraStatus {
	if s.cameras == nil {
		return nil
	}
	ordered := s.cameras.ListCameras()
	statuses := make([]camera.CameraStatus, 0, len(ordered))
	for _, name := range ordered {
		cam := s.cameras.GetCamera(name)
		if cam != nil {
			statuses = append(statuses, cam.Status())
		}
	}
	return statuses
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("failed to write JSON response", "error", err)
	}
}
