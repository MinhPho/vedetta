package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rvben/vedetta/internal/recording"
)

func (s *Server) handleHealthLive(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"uptime": formatDuration(time.Since(startTime)),
	})
}

func (s *Server) handleHealthReady(w http.ResponseWriter, _ *http.Request) {
	statusCode := http.StatusOK
	status := "ready"

	checks := map[string]any{
		"initialized": s.ready.Load(),
	}

	if !s.ready.Load() {
		status = "starting"
		statusCode = http.StatusServiceUnavailable
	}

	if err := s.db.Ping(); err != nil {
		status = "degraded"
		statusCode = http.StatusServiceUnavailable
		checks["database"] = err.Error()
	} else {
		checks["database"] = "ok"
	}

	cameraStatuses := s.cameraStatuses()
	degraded := 0
	for _, st := range cameraStatuses {
		if st.Degraded {
			degraded++
		}
	}
	checks["cameras"] = map[string]any{
		"total":    len(cameraStatuses),
		"degraded": degraded,
	}
	if degraded > 0 {
		status = "degraded"
		statusCode = http.StatusServiceUnavailable
	}

	if s.recorder != nil {
		storageStats := s.recorder.StorageStats()
		checks["storage"] = map[string]any{
			"disk_low":         storageStats.DiskLow,
			"recording_paused": storageStats.RecordingPaused,
		}
		if storageStats.DiskLow {
			status = "degraded"
			statusCode = http.StatusServiceUnavailable
		}
	}

	writeJSON(w, statusCode, map[string]any{
		"status": status,
		"checks": checks,
	})
}

func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")

	cameraStatuses := s.cameraStatuses()
	online := 0
	degraded := 0
	for _, st := range cameraStatuses {
		if st.Online {
			online++
		}
		if st.Degraded {
			degraded++
		}
	}

	var storageStats recording.StorageStats
	if s.recorder != nil {
		storageStats = s.recorder.StorageStats()
	}
	eventCount, _ := s.db.CountEvents()
	segmentCount, _ := s.db.CountSegments()

	var b strings.Builder
	fmt.Fprintf(&b, "vedetta_up 1\n")
	fmt.Fprintf(&b, "vedetta_ready %d\n", boolMetric(s.ready.Load()))
	fmt.Fprintf(&b, "vedetta_cameras_total %d\n", len(cameraStatuses))
	fmt.Fprintf(&b, "vedetta_cameras_online %d\n", online)
	fmt.Fprintf(&b, "vedetta_cameras_degraded %d\n", degraded)
	fmt.Fprintf(&b, "vedetta_events_total %d\n", eventCount)
	fmt.Fprintf(&b, "vedetta_segments_total %d\n", segmentCount)
	fmt.Fprintf(&b, "vedetta_storage_bytes %d\n", storageStats.TotalBytes)
	fmt.Fprintf(&b, "vedetta_disk_available_bytes %d\n", storageStats.DiskAvailable)
	fmt.Fprintf(&b, "vedetta_recording_paused %d\n", boolMetric(storageStats.RecordingPaused))
	fmt.Fprintf(&b, "vedetta_disk_low %d\n", boolMetric(storageStats.DiskLow))
	for _, st := range cameraStatuses {
		fmt.Fprintf(&b, "vedetta_camera_online{camera=%q} %d\n", promLabel(st.Name), boolMetric(st.Online))
		fmt.Fprintf(&b, "vedetta_camera_degraded{camera=%q} %d\n", promLabel(st.Name), boolMetric(st.Degraded))
	}

	_, _ = w.Write([]byte(b.String()))
}

func boolMetric(v bool) int {
	if v {
		return 1
	}
	return 0
}

func promLabel(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return replacer.Replace(value)
}
