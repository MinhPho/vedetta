package main

import (
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/rvben/vedetta/internal/api"
	"github.com/rvben/vedetta/internal/auth"
	"github.com/rvben/vedetta/internal/camera"
	"github.com/rvben/vedetta/internal/config"
	"github.com/rvben/vedetta/internal/detect"
	"github.com/rvben/vedetta/internal/mqtt"
	"github.com/rvben/vedetta/internal/recording"
	"github.com/rvben/vedetta/internal/rtsp"
	"github.com/rvben/vedetta/internal/storage"
	"github.com/rvben/vedetta/internal/stream"
)

func main() {
	// Handle subcommands before flag parsing
	if len(os.Args) > 1 && os.Args[1] == "discover" {
		runDiscover()
		return
	}

	configPath := flag.String("config", "config.yml", "path to configuration file")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	if err := auth.ValidateConfig(cfg.Auth.Username, cfg.Auth.Password); err != nil {
		slog.Error("invalid auth config", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := storage.New(cfg.Storage.DBPath)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	// Purge events whose snapshot files no longer exist on disk
	go purgeOrphanedEvents(db)

	var mqttClient *mqtt.Client
	if cfg.MQTT.Enabled {
		mqttClient, err = mqtt.New(cfg.MQTT)
		if err != nil {
			slog.Error("failed to connect to MQTT", "error", err)
			os.Exit(1)
		}
		defer mqttClient.Close()
	}

	detector := detect.New(cfg.Detect)
	defer detector.Close()

	var faceRecognizer *detect.FaceRecognizer
	fr, frErr := detect.NewFaceRecognizer(detect.FaceRecognizerConfig{
		CropDir: filepath.Join(cfg.Events.SnapshotPath, "faces"),
	})
	if frErr != nil {
		slog.Warn("face recognition disabled", "error", frErr)
	} else {
		faceRecognizer = fr
		defer fr.Close()
		slog.Info("face recognition enabled")
	}

	// Create RTSP Hub — central connection manager
	hub := rtsp.NewHub(ctx)
	defer hub.Close()

	slog.Info("native Go media pipeline active (no ffmpeg required)")

	recorder := recording.New(cfg.Recording, db, hub, cfg.Events.SnapshotPath)

	// Register cameras for recording
	for _, cam := range cfg.Cameras {
		if !cam.Enabled {
			continue
		}
		recordURL := cam.RecordURL
		if recordURL == "" {
			recordURL = cam.URL
		}
		recorder.RegisterCamera(cam.Name, recordURL)
	}

	// Start continuous segment recording
	recorder.StartContinuousRecording(ctx)
	recorder.StartRetentionCleanup(ctx)
	recorder.StartStatsRefresh(ctx)

	// Publish HA MQTT discovery for all enabled cameras
	if mqttClient != nil {
		var cameraNames []string
		for _, cam := range cfg.Cameras {
			if cam.Enabled {
				cameraNames = append(cameraNames, cam.Name)
			}
		}
		mqttClient.PublishDiscovery(cameraNames)
	}

	events := make(chan camera.Event, 100)
	eventEnds := make(chan camera.EventEnd, 100)
	presenceEvents := make(chan camera.PresenceEvent, 100)
	faceEvents := make(chan camera.FaceEvent, 100)

	manager := camera.NewManager(cfg.Cameras, detector, events, eventEnds, presenceEvents, hub, cfg.Events.SnapshotPath, cfg.Events.SnapshotQuality, faceRecognizer, faceEvents, filepath.Join(cfg.Events.SnapshotPath, "faces"))

	// Sync zones from config to DB and load them into cameras
	syncConfigZones(db, cfg.Cameras, manager)

	manager.Start(ctx)

	// Periodically publish camera online/offline status to MQTT.
	// Uses a short initial interval so cameras that connect quickly get
	// reported promptly, then switches to the normal 30s interval.
	if mqttClient != nil {
		go func() {
			publishStatuses := func() {
				for _, st := range manager.CameraStatuses() {
					mqttClient.PublishCameraStatus(st.Name, st.Online)
				}
			}

			// Publish a few times quickly at startup to catch cameras as they connect
			for range 3 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Second):
					publishStatuses()
				}
			}

			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					publishStatuses()
				}
			}
		}()
	}

	// Event lifecycle manager: tracks active events and schedules clip extraction
	// when the tracked object leaves the frame or max duration is reached.
	go func() {
		type activeEvent struct {
			event      camera.Event
			timer      *time.Timer
			tempCancel context.CancelFunc // for non-continuous temporary recording
		}
		active := make(map[string]*activeEvent) // eventID → state
		maxDur := cfg.Recording.MaxEventDuration
		timeouts := make(chan string, 100) // eventIDs that hit max duration

		finalizeEvent := func(ae *activeEvent, endTime time.Time) {
			ae.timer.Stop()
			ev := ae.event
			ev.EndTime = endTime
			duration := endTime.Sub(ev.Timestamp)

			if err := db.UpdateEventEndTime(ev.ID, endTime); err != nil {
				slog.Error("failed to update event end time", "event", ev.ID, "error", err)
			}
			slog.Info("event ended",
				"event", ev.ID,
				"camera", ev.CameraName,
				"label", ev.Label,
				"duration", duration.Round(time.Second),
			)

			if ae.tempCancel != nil {
				tc := ae.tempCancel
				go func() {
					select {
					case <-time.After(cfg.Recording.PostCapture + 5*time.Second):
					case <-ctx.Done():
					}
					tc()
				}()
			}

			// Schedule clip extraction after post-capture + segment finalization buffer
			go func() {
				delay := cfg.Recording.PostCapture + 15*time.Second
				select {
				case <-time.After(delay):
				case <-ctx.Done():
					return
				}
				for attempt := range 5 {
					err := recorder.SaveClip(ctx, ev)
					if err == nil {
						return
					}
					if attempt < 4 {
						slog.Debug("clip not ready, retrying", "event", ev.ID, "attempt", attempt+1)
						select {
						case <-time.After(time.Duration(attempt+1) * 30 * time.Second):
						case <-ctx.Done():
							return
						}
					} else {
						slog.Error("failed to save clip after retries", "event", ev.ID, "error", err)
					}
				}
			}()
		}

		for {
			select {
			case <-ctx.Done():
				for id, ae := range active {
					ae.timer.Stop()
					if ae.tempCancel != nil {
						ae.tempCancel()
					}
					delete(active, id)
				}
				return

			case event := <-events:
				slog.Info("event detected",
					"camera", event.CameraName,
					"label", event.Label,
					"score", fmt.Sprintf("%.2f", event.Score),
				)

				if err := db.SaveEvent(event); err != nil {
					slog.Error("failed to save event", "error", err)
				}

				if mqttClient != nil {
					if err := mqttClient.PublishEvent(event); err != nil {
						slog.Error("failed to publish event", "error", err)
					}
				}

				// Start temporary recording if continuous is off
				var tempCancel context.CancelFunc
				if !cfg.Recording.Continuous {
					if url := recorder.CameraURL(event.CameraName); url != "" {
						tempCtx, cancel := context.WithCancel(ctx)
						tempCancel = cancel
						recorder.StartTemporaryRecording(tempCtx, event.CameraName, url)
					}
				}

				// Max duration timer sends to timeouts channel (avoids data race)
				evID := event.ID
				timer := time.AfterFunc(maxDur, func() {
					select {
					case timeouts <- evID:
					default:
					}
				})

				active[evID] = &activeEvent{
					event:      event,
					timer:      timer,
					tempCancel: tempCancel,
				}

			case end := <-eventEnds:
				if ae, ok := active[end.EventID]; ok {
					finalizeEvent(ae, end.EndTime)
					delete(active, end.EventID)
				}

			case evID := <-timeouts:
				if ae, ok := active[evID]; ok {
					endTime := ae.event.Timestamp.Add(maxDur)
					finalizeEvent(ae, endTime)
					delete(active, evID)
				}

			case pe := <-presenceEvents:
				if err := db.UpdateZonePresence(pe.ZoneID, pe.Label, pe.Type == "zone_enter"); err != nil {
					slog.Error("failed to persist presence event", "zone", pe.ZoneName, "label", pe.Label, "error", err)
				}

			case fe := <-faceEvents:
				for _, result := range fe.Results {
					personID, similarity := matchFaceToPerson(db, result.Embedding, faceRecognizer)

					face := storage.Face{
						EventID:    fe.EventID,
						Camera:     fe.Camera,
						Embedding:  float32ToBytes(result.Embedding),
						CropPath:   result.CropPath,
						Confidence: float64(result.Confidence),
						Timestamp:  time.Now(),
					}
					if personID > 0 {
						face.PersonID = &personID
						face.Similarity = &similarity
					}

					faceID, saveErr := db.SaveFace(face)
					if saveErr != nil {
						slog.Error("failed to save face", "error", saveErr)
						continue
					}

					if personID == 0 {
						newPID, createErr := db.SavePerson("", false, float32ToBytes(result.Embedding))
						if createErr != nil {
							slog.Error("failed to create person for face", "error", createErr)
							continue
						}
						sim := 1.0
						_ = db.UpdateFacePerson(faceID, newPID, sim)
						slog.Info("new person created from face", "person_id", newPID, "camera", fe.Camera)
					} else {
						updatePersonCentroid(db, personID, result.Embedding)
						slog.Info("face matched to person", "person_id", personID, "similarity", fmt.Sprintf("%.3f", similarity), "camera", fe.Camera)
					}
				}
			}
		}
	}()

	// Create shared auth checker (nil if auth not configured)
	authChecker := auth.New(cfg.Auth.Username, cfg.Auth.Password)
	if authChecker != nil {
		defer authChecker.Close()
	}

	// Start RTSP re-publishing server if enabled
	if cfg.RTSPServer.Enabled {
		rtspServer := stream.NewRTSPServer(hub, cfg.RTSPServer, authChecker, cfg.Cameras)
		if err := rtspServer.Start(); err != nil {
			slog.Error("RTSP re-publish server failed to start", "error", err)
		} else {
			defer rtspServer.Close()
			slog.Info("RTSP re-publish server started", "port", cfg.RTSPServer.Port)
		}
	}

	server := api.New(cfg.API, authChecker, db, manager, recorder, hub)
	go func() {
		if err := server.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("API server failed", "error", err)
			cancel()
		}
	}()

	slog.Info("vedetta started", "cameras", len(cfg.Cameras))

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	slog.Info("shutting down")

	// Gracefully shut down the HTTP server (5s timeout for in-flight requests)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("HTTP server shutdown error", "error", err)
	}

	cancel()

	// Wait for recording goroutines to finalize segments before closing DB
	recorder.Close()
}

// syncConfigZones inserts zones from config into the database (if not already present)
// and loads all zones from DB into the corresponding cameras.
func syncConfigZones(db *storage.DB, cameras []config.CameraConfig, manager *camera.Manager) {
	for _, camCfg := range cameras {
		if !camCfg.Enabled {
			continue
		}

		// Insert config zones into DB if they don't already exist
		for _, cfgZone := range camCfg.Zones {
			existing, err := db.GetZone(camCfg.Name, cfgZone.Name)
			if err != nil {
				slog.Error("failed to check zone existence", "camera", camCfg.Name, "zone", cfgZone.Name, "error", err)
				continue
			}
			if existing != nil {
				continue // Don't overwrite zones created/modified via API
			}

			labels := cfgZone.Labels
			if len(labels) == 0 {
				labels = cfgZone.Objects
			}

			z := camera.Zone{
				Camera:          camCfg.Name,
				Name:            cfgZone.Name,
				X1:              cfgZone.Coordinates[0],
				Y1:              cfgZone.Coordinates[1],
				X2:              cfgZone.Coordinates[2],
				Y2:              cfgZone.Coordinates[3],
				Labels:          labels,
				TrackPresence:   cfgZone.TrackPresence,
				FaceRecognition: cfgZone.FaceRecognition,
				Enabled:         true,
			}
			if err := db.SaveZone(z); err != nil {
				slog.Error("failed to save config zone", "camera", camCfg.Name, "zone", cfgZone.Name, "error", err)
			} else {
				slog.Info("synced zone from config", "camera", camCfg.Name, "zone", cfgZone.Name)
			}
		}

		// Load all zones from DB into the camera
		cam := manager.GetCamera(camCfg.Name)
		if cam == nil {
			continue
		}
		zones, err := db.ListZones(camCfg.Name)
		if err != nil {
			slog.Error("failed to load zones", "camera", camCfg.Name, "error", err)
			continue
		}
		cam.SetZones(zones)
		if len(zones) > 0 {
			slog.Info("loaded zones", "camera", camCfg.Name, "count", len(zones))
		}
	}
}

// matchFaceToPerson finds the best matching person for a face embedding.
// Returns (personID, similarity) or (0, 0) if no match above threshold.
func matchFaceToPerson(db *storage.DB, embedding []float32, fr *detect.FaceRecognizer) (int64, float64) {
	if fr == nil {
		return 0, 0
	}
	people, err := db.ListPeople()
	if err != nil {
		slog.Error("failed to list people for face matching", "error", err)
		return 0, 0
	}

	var bestID int64
	var bestSim float64
	threshold := fr.MatchThreshold()

	for _, p := range people {
		if p.Ignore || len(p.Centroid) == 0 {
			continue
		}
		centroid := bytesToFloat32(p.Centroid)
		sim := detect.CosineSimilarity(embedding, centroid)
		if sim > bestSim {
			bestSim = sim
			bestID = p.ID
		}
	}

	if bestSim >= threshold {
		return bestID, bestSim
	}
	return 0, 0
}

// updatePersonCentroid updates a person's centroid with a running average.
func updatePersonCentroid(db *storage.DB, personID int64, newEmbedding []float32) {
	p, err := db.GetPerson(personID)
	if err != nil || p == nil {
		return
	}

	if len(p.Centroid) == 0 {
		_ = db.UpdatePersonCentroid(personID, float32ToBytes(newEmbedding))
		return
	}

	old := bytesToFloat32(p.Centroid)
	if len(old) != len(newEmbedding) {
		_ = db.UpdatePersonCentroid(personID, float32ToBytes(newEmbedding))
		return
	}

	alpha := float32(0.3)
	merged := make([]float32, len(old))
	var norm float64
	for i := range merged {
		merged[i] = (1-alpha)*old[i] + alpha*newEmbedding[i]
		norm += float64(merged[i]) * float64(merged[i])
	}
	if norm > 1e-10 {
		invNorm := float32(1.0 / math.Sqrt(norm))
		for i := range merged {
			merged[i] *= invNorm
		}
	}

	_ = db.UpdatePersonCentroid(personID, float32ToBytes(merged))
}

// float32ToBytes converts a float32 slice to little-endian bytes.
func float32ToBytes(f []float32) []byte {
	buf := make([]byte, len(f)*4)
	for i, v := range f {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

// bytesToFloat32 converts little-endian bytes to a float32 slice.
func bytesToFloat32(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	f := make([]float32, len(b)/4)
	for i := range f {
		f[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return f
}

// purgeOrphanedEvents removes events whose snapshot files no longer exist on disk.
func purgeOrphanedEvents(db *storage.DB) {
	events, err := db.EventsWithSnapshots()
	if err != nil {
		slog.Error("failed to query events for orphan check", "error", err)
		return
	}

	var purged int
	for _, ev := range events {
		if _, err := os.Stat(ev.SnapshotPath); err != nil {
			if err := db.DeleteEvent(ev.ID); err != nil {
				slog.Error("failed to delete orphaned event", "id", ev.ID, "error", err)
				continue
			}
			purged++
		}
	}
	if purged > 0 {
		slog.Info("purged orphaned events with missing snapshots", "count", purged)
	}
}
