package api

import (
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/rvben/vedetta/internal/media"
	"github.com/rvben/vedetta/internal/recording"
)

func (s *Server) handleListSegments(w http.ResponseWriter, r *http.Request) {
	cameraName := r.PathValue("camera")
	cam := s.cameras.GetCamera(cameraName)
	if cam == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "camera not found"})
		return
	}

	date := time.Now().UTC()
	if dateStr := r.URL.Query().Get("date"); dateStr != "" {
		if parsed, err := time.Parse("2006-01-02", dateStr); err == nil {
			date = parsed
		}
	}

	segments, err := s.db.GetSegmentsForDate(cameraName, date)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	type segmentInfo struct {
		ID        int64     `json:"id"`
		StartTime time.Time `json:"start_time"`
		EndTime   time.Time `json:"end_time"`
		SizeBytes int64     `json:"size_bytes"`
	}

	result := make([]segmentInfo, 0, len(segments))
	for _, seg := range segments {
		result = append(result, segmentInfo{
			ID:        seg.ID,
			StartTime: seg.StartTime,
			EndTime:   seg.EndTime,
			SizeBytes: seg.SizeBytes,
		})
	}

	total := len(result)
	writeJSON(w, http.StatusOK, map[string]any{
		"items":    result,
		"total":    total,
		"limit":    total,
		"offset":   0,
		"has_more": false,
	})
}

func (s *Server) handleCameraTimeline(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	cam := s.cameras.GetCamera(name)
	if cam == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "camera not found"})
		return
	}

	date := time.Now().UTC()
	if dateStr := r.URL.Query().Get("date"); dateStr != "" {
		if parsed, err := time.Parse("2006-01-02", dateStr); err == nil {
			date = parsed
		}
	}

	segments, err := s.db.GetSegmentsForDate(name, date)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	events, err := s.db.QueryEventsForDate(name, date)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	type timelineSegment struct {
		StartTime time.Time `json:"start_time"`
		EndTime   time.Time `json:"end_time"`
	}

	type timelineEvent struct {
		ID        string    `json:"id"`
		Label     string    `json:"label"`
		Score     float32   `json:"score"`
		Timestamp time.Time `json:"timestamp"`
		EndTime   time.Time `json:"end_time,omitempty"`
	}

	type timelineActivity struct {
		Time  time.Time `json:"t"`
		Score float64   `json:"s"`
	}

	segs := make([]timelineSegment, 0, len(segments))
	for _, seg := range segments {
		segs = append(segs, timelineSegment{
			StartTime: seg.StartTime,
			EndTime:   seg.EndTime,
		})
	}

	evts := make([]timelineEvent, 0, len(events))
	for _, evt := range events {
		evts = append(evts, timelineEvent{
			ID:        evt.ID,
			Label:     evt.Label,
			Score:     evt.Score,
			Timestamp: evt.Timestamp,
			EndTime:   evt.EndTime,
		})
	}

	activity, err := s.db.GetMotionActivity(name, date)
	if err != nil {
		slog.Error("failed to get motion activity", "camera", name, "error", err)
		activity = nil
	}
	acts := make([]timelineActivity, 0, len(activity))
	for _, a := range activity {
		acts = append(acts, timelineActivity{Time: a.Bucket, Score: a.Score})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"segments": segs,
		"events":   evts,
		"activity": acts,
	})
}

func (s *Server) handlePlaybackM3U8(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	cam := s.cameras.GetCamera(name)
	if cam == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "camera not found"})
		return
	}

	startStr := r.URL.Query().Get("start")
	if startStr == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "start parameter required"})
		return
	}

	start, err := time.Parse(time.RFC3339, startStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid start time format"})
		return
	}

	durationSec := 600
	if ds := r.URL.Query().Get("duration"); ds != "" {
		if d, err := strconv.Atoi(ds); err == nil && d > 0 {
			durationSec = d
		}
	}
	if durationSec > 3600 {
		durationSec = 3600
	}

	end := start.Add(time.Duration(durationSec) * time.Second)
	segments, err := s.db.QuerySegments(name, start, end)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if len(segments) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no recordings found"})
		return
	}

	var paths []string
	var uris []string
	for _, seg := range segments {
		paths = append(paths, seg.Path)
		uris = append(uris, fmt.Sprintf("/api/cameras/%s/segments/%d", name, seg.ID))
	}

	offset := start.Sub(segments[0].StartTime)
	if offset < 0 {
		offset = 0
	}

	result, err := media.GenerateHLSPlaylist(paths, uris, offset)
	if err != nil {
		slog.Error("HLS playlist generation failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "playlist generation failed"})
		return
	}

	// Cache the segment refs so handleSegmentHLS can look them up
	for _, seg := range segments {
		cacheKey := fmt.Sprintf("%s:%d", name, seg.ID)
		s.hlsSegmentCache.Store(cacheKey, result.Segments)
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	if _, err := w.Write([]byte(result.Playlist)); err != nil {
		slog.Error("HLS playlist write failed", "error", err)
	}
}

func (s *Server) handleSegment(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	idStr := r.PathValue("id")

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid segment ID"})
		return
	}

	seg, err := s.db.GetSegmentByID(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if seg == nil || seg.Camera != name {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "segment not found"})
		return
	}

	http.ServeFile(w, r, seg.Path)
}

// handleSegmentInit serves the fMP4 init segment (ftyp+moov) for HLS playback.
func (s *Server) handleSegmentInit(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	idStr := r.PathValue("id")

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid segment ID"})
		return
	}

	seg, err := s.db.GetSegmentByID(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if seg == nil || seg.Camera != name {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "segment not found"})
		return
	}

	// Read just the ftyp+moov init segment from the start of the file.
	// Served directly (not via byte-range) for Safari/iOS native HLS compatibility.
	f, err := os.Open(seg.Path)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "open segment file"})
		return
	}
	defer f.Close()

	// Read the init segment size by scanning ftyp+moov box headers
	var initSize int64
	for {
		var hdr [8]byte
		if _, err := io.ReadFull(f, hdr[:]); err != nil {
			break
		}
		boxSize := int64(binary.BigEndian.Uint32(hdr[:4]))
		boxType := string(hdr[4:8])
		if boxType == "moof" || boxType == "mdat" {
			break // past init segment
		}
		initSize += boxSize
		if _, err := f.Seek(initSize, io.SeekStart); err != nil {
			break
		}
	}
	if initSize == 0 {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "no init segment found"})
		return
	}

	// Read and serve the init bytes
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "seek failed"})
		return
	}
	initData := make([]byte, initSize)
	if _, err := io.ReadFull(f, initData); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "read init segment"})
		return
	}

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(initData)
}

// handleSegmentHLS serves a re-segmented fMP4 chunk for HLS playback.
// It reads the raw moof+mdat bytes from disk, unmarshals them, and re-marshals
// as clean fMP4 that MSE/hls.js can consume.
func (s *Server) handleSegmentHLS(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	idStr := r.PathValue("id")
	segNumStr := r.PathValue("segNum")

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid segment ID"})
		return
	}

	segNum, err := strconv.Atoi(segNumStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid segment number"})
		return
	}

	// Look up cached segment refs
	cacheKey := fmt.Sprintf("%s:%d", name, id)
	refsVal, ok := s.hlsSegmentCache.Load(cacheKey)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "playlist not found, request m3u8 first"})
		return
	}
	refs := refsVal.([]media.HLSSegmentRef)

	if segNum < 0 || segNum >= len(refs) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "segment number out of range"})
		return
	}

	ref := refs[segNum]

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "public, max-age=86400")

	if err := media.ServeHLSSegment(w, ref.FilePath, ref.ByteStart, ref.ByteEnd); err != nil {
		slog.Error("HLS segment serve failed", "error", err, "file", ref.FilePath, "segment", segNum)
	}
}

func (s *Server) handleRecordingsCalendar(w http.ResponseWriter, r *http.Request) {
	cameraFilter := r.URL.Query().Get("camera")
	monthStr := r.URL.Query().Get("month")

	year, month := time.Now().Year(), int(time.Now().Month())
	if monthStr != "" {
		if parsed, err := time.Parse("2006-01", monthStr); err == nil {
			year = parsed.Year()
			month = int(parsed.Month())
		}
	}

	days, err := s.db.GetRecordingDays(cameraFilter, year, month)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if days == nil {
		days = []int{}
	}

	writeJSON(w, http.StatusOK, map[string]any{"days": days})
}

func (s *Server) handleRecordingsSummary(w http.ResponseWriter, r *http.Request) {
	dateStr := r.URL.Query().Get("date")

	date := time.Now().UTC()
	if dateStr != "" {
		if parsed, err := time.Parse("2006-01-02", dateStr); err == nil {
			date = parsed
		}
	}

	// Get all segments for the date across all cameras.
	segments, err := s.db.GetSegmentsForDate("", date)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	type segmentInfo struct {
		StartTime time.Time `json:"start_time"`
		EndTime   time.Time `json:"end_time"`
		SizeBytes int64     `json:"size_bytes"`
	}

	type cameraSummary struct {
		Name       string        `json:"name"`
		Segments   []segmentInfo `json:"segments"`
		TotalBytes int64         `json:"total_bytes"`
	}

	// Group by camera, preserving config order.
	cameraOrder := s.cameras.ListCameras()
	grouped := make(map[string]*cameraSummary, len(cameraOrder))
	for _, name := range cameraOrder {
		grouped[name] = &cameraSummary{Name: name, Segments: []segmentInfo{}}
	}

	var totalBytes int64
	for _, seg := range segments {
		cs, ok := grouped[seg.Camera]
		if !ok {
			cs = &cameraSummary{Name: seg.Camera, Segments: []segmentInfo{}}
			grouped[seg.Camera] = cs
			cameraOrder = append(cameraOrder, seg.Camera)
		}
		cs.Segments = append(cs.Segments, segmentInfo{
			StartTime: seg.StartTime,
			EndTime:   seg.EndTime,
			SizeBytes: seg.SizeBytes,
		})
		cs.TotalBytes += seg.SizeBytes
		totalBytes += seg.SizeBytes
	}

	// Build ordered result, skip cameras with no data.
	result := make([]cameraSummary, 0, len(cameraOrder))
	for _, name := range cameraOrder {
		cs := grouped[name]
		if len(cs.Segments) > 0 {
			result = append(result, *cs)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"cameras":     result,
		"total_bytes": totalBytes,
	})
}

func (s *Server) handleRecordingExport(w http.ResponseWriter, r *http.Request) {
	cameraName := r.PathValue("camera")

	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")
	if startStr == "" || endStr == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "start and end parameters required (RFC3339)"})
		return
	}

	start, err := time.Parse(time.RFC3339, startStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid start time, use RFC3339"})
		return
	}
	end, err := time.Parse(time.RFC3339, endStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid end time, use RFC3339"})
		return
	}

	if !end.After(start) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "end must be after start"})
		return
	}

	if end.Sub(start) > 24*time.Hour {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "export range limited to 24 hours"})
		return
	}

	// Run PrepareExport with a timeout to prevent the handler from blocking
	// indefinitely on filesystem issues (e.g., EINTR on macOS APFS USB volumes).
	type exportResult struct {
		result *recording.ExportResult
		err    error
	}
	exportCh := make(chan exportResult, 1)
	go func() {
		res, err := s.recorder.PrepareExport(cameraName, start, end)
		exportCh <- exportResult{res, err}
	}()

	exportTimeout := 5 * time.Minute
	select {
	case res := <-exportCh:
		if res.err != nil {
			slog.Error("recording export failed",
				"camera", cameraName,
				"start", start.Format(time.RFC3339),
				"end", end.Format(time.RFC3339),
				"error", res.err,
			)
			writeJSON(w, http.StatusNotFound, map[string]string{"error": res.err.Error()})
			return
		}
		defer res.result.Close()

		filename := fmt.Sprintf("%s_%s_%s.mp4",
			cameraName,
			start.Format("2006-01-02_15-04-05"),
			end.Format("15-04-05"),
		)

		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))

		// ServeContent handles Content-Type, Content-Length, Range requests,
		// and uses sendfile(2) for zero-copy streaming when possible.
		http.ServeContent(w, r, filename, time.Now(), res.result.File)

	case <-time.After(exportTimeout):
		slog.Error("recording export timed out",
			"camera", cameraName,
			"start", start.Format(time.RFC3339),
			"end", end.Format(time.RFC3339),
			"timeout", exportTimeout,
		)
		writeJSON(w, http.StatusGatewayTimeout, map[string]string{"error": "export timed out"})

	case <-r.Context().Done():
		slog.Info("recording export cancelled by client",
			"camera", cameraName,
		)
	}
}
