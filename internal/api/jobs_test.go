package api

import (
	"sync"
	"testing"
	"time"
)

func TestScheduleObjectRematchCoalescesConcurrentRequests(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	done := make(chan struct{}, 2)

	s := &Server{
		objectRematchRunning: make(map[int64]bool),
		objectRematchPending: make(map[int64]bool),
	}
	s.objectRematchFn = func(id int64) {
		started <- struct{}{}
		<-release
		done <- struct{}{}
	}

	s.scheduleObjectRematch(7)
	<-started

	s.scheduleObjectRematch(7)

	release <- struct{}{}
	<-done

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("expected pending rematch to rerun once")
	}

	release <- struct{}{}
	<-done

	time.Sleep(50 * time.Millisecond)
	s.objectRematchMu.Lock()
	defer s.objectRematchMu.Unlock()
	if s.objectRematchRunning[7] || s.objectRematchPending[7] {
		t.Fatal("object rematch state was not cleared")
	}
}

func TestFaceBackfillStateRejectsConcurrentRuns(t *testing.T) {
	s := &Server{}
	if !s.beginFaceBackfill() {
		t.Fatal("first beginFaceBackfill() should succeed")
	}
	if s.beginFaceBackfill() {
		t.Fatal("second beginFaceBackfill() should fail while running")
	}
	s.endFaceBackfill()
	if !s.beginFaceBackfill() {
		t.Fatal("beginFaceBackfill() should succeed after end")
	}
	s.endFaceBackfill()
}

func TestScheduleObjectRematchIgnoresZeroID(t *testing.T) {
	var called bool
	var mu sync.Mutex
	s := &Server{
		objectRematchRunning: make(map[int64]bool),
		objectRematchPending: make(map[int64]bool),
		objectRematchFn: func(id int64) {
			mu.Lock()
			called = true
			mu.Unlock()
		},
	}

	s.scheduleObjectRematch(0)
	time.Sleep(25 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if called {
		t.Fatal("scheduleObjectRematch(0) should not run")
	}
}
