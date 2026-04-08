package api

import "log/slog"

func (s *Server) scheduleObjectRematch(objectID int64) {
	if s == nil || objectID == 0 {
		return
	}

	s.objectRematchMu.Lock()
	if s.objectRematchRunning[objectID] {
		s.objectRematchPending[objectID] = true
		s.objectRematchMu.Unlock()
		return
	}
	s.objectRematchRunning[objectID] = true
	s.objectRematchMu.Unlock()

	go func() {
		run := s.rematchRecentEvents
		if s.objectRematchFn != nil {
			run = s.objectRematchFn
		}
		for {
			run(objectID)

			s.objectRematchMu.Lock()
			if s.objectRematchPending[objectID] {
				s.objectRematchPending[objectID] = false
				s.objectRematchMu.Unlock()
				continue
			}
			delete(s.objectRematchRunning, objectID)
			delete(s.objectRematchPending, objectID)
			s.objectRematchMu.Unlock()
			return
		}
	}()
}

func (s *Server) beginFaceBackfill() bool {
	if s == nil {
		return false
	}
	return s.faceBackfillRunning.CompareAndSwap(false, true)
}

func (s *Server) endFaceBackfill() {
	if s == nil {
		return
	}
	if !s.faceBackfillRunning.CompareAndSwap(true, false) {
		slog.Warn("face backfill state was not marked running during completion")
	}
}
