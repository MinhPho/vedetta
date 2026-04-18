package detect

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockAudioBackend is the audio counterpart of mockBackend. It supports
// hang simulation via blockCh and error injection via runErr.
type mockAudioBackend struct {
	scores   []float32
	runCount int32
	closed   int
	runErr   error
	blockCh  chan struct{}
}

func (m *mockAudioBackend) Run(_ []float32) ([]float32, error) {
	atomic.AddInt32(&m.runCount, 1)
	if m.blockCh != nil {
		<-m.blockCh
	}
	if m.runErr != nil {
		return nil, m.runErr
	}
	return m.scores, nil
}

func (m *mockAudioBackend) Close()       { m.closed++ }
func (m *mockAudioBackend) Name() string { return "mock" }

func TestAudioClassifier_HappyPath(t *testing.T) {
	want := []float32{0.1, 0.7, 0.2}
	c := NewAudioClassifier(&mockAudioBackend{scores: want})
	c.onWedged = func() {} // never exit the test binary
	defer c.Close()

	got := c.Classify([]float32{0, 0, 0})
	if len(got) != len(want) {
		t.Fatalf("score length: got %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("score[%d]: got %f want %f", i, got[i], want[i])
		}
	}
}

func TestAudioClassifier_TimeoutReturnsNil(t *testing.T) {
	mock := &mockAudioBackend{blockCh: make(chan struct{})}
	c := NewAudioClassifier(mock)
	c.inferTimeout = 50 * time.Millisecond
	c.onWedged = func() {}
	defer func() {
		close(mock.blockCh)
		c.Close()
	}()

	if got := c.Classify(nil); got != nil {
		t.Fatalf("expected nil on timeout, got %v", got)
	}
}

func TestAudioClassifier_BusyFastPath(t *testing.T) {
	// White-box: directly occupy the worker and fill the buffer slot, then
	// verify that Classify returns nil without waiting for inferTimeout.
	mock := &mockAudioBackend{blockCh: make(chan struct{})}
	c := NewAudioClassifier(mock)
	c.inferTimeout = 5 * time.Second // long enough that any non-fast-path would obviously hang
	c.onWedged = func() {}
	defer func() {
		close(mock.blockCh)
		c.Close()
	}()

	// Force worker startup, then occupy it via the request channel.
	c.ensureWorker()
	parked := make(chan inferResult, 1)
	c.requestCh <- inferRequest{input: nil, resultCh: parked} // worker pulls and blocks in Run
	// Wait for the worker to actually pull the request.
	for atomic.LoadInt32(&mock.runCount) == 0 {
		time.Sleep(time.Millisecond)
	}
	// Fill the buffered slot so the next non-blocking send must fail.
	c.requestCh <- inferRequest{input: nil, resultCh: make(chan inferResult, 1)}

	start := time.Now()
	got := c.Classify(nil)
	elapsed := time.Since(start)

	if got != nil {
		t.Fatalf("expected nil on busy fast path, got %v", got)
	}
	if elapsed > 50*time.Millisecond {
		t.Errorf("busy fast path took %v — should be near-immediate", elapsed)
	}
}

func TestAudioClassifier_ConcurrentCallsReturnNil(t *testing.T) {
	// Black-box: many concurrent callers must all return nil eventually
	// under a wedged backend, without panicking or blocking forever.
	mock := &mockAudioBackend{blockCh: make(chan struct{})}
	c := NewAudioClassifier(mock)
	c.inferTimeout = 100 * time.Millisecond
	c.onWedged = func() {}
	defer func() {
		close(mock.blockCh)
		c.Close()
	}()

	const n = 5
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := c.Classify(nil); got != nil {
				t.Errorf("expected nil under wedge, got %v", got)
			}
		}()
	}
	wg.Wait()
}

func TestAudioClassifier_WatchdogFiresOnLongHang(t *testing.T) {
	mock := &mockAudioBackend{blockCh: make(chan struct{})}
	c := NewAudioClassifier(mock)
	c.inferTimeout = 50 * time.Millisecond
	c.wedgeLimit = 100 * time.Millisecond
	wedged := make(chan struct{}, 1)
	c.onWedged = func() {
		select {
		case wedged <- struct{}{}:
		default:
		}
	}
	defer func() {
		close(mock.blockCh)
		c.Close()
	}()

	c.Classify(nil) // returns nil after inferTimeout

	select {
	case <-wedged:
		// expected
	case <-time.After(500 * time.Millisecond):
		t.Fatal("watchdog did not fire after wedgeLimit")
	}
}

func TestAudioClassifier_BackendErrorReturnsNil(t *testing.T) {
	c := NewAudioClassifier(&mockAudioBackend{runErr: errors.New("boom")})
	c.onWedged = func() {}
	defer c.Close()

	if got := c.Classify(nil); got != nil {
		t.Fatalf("expected nil on backend error, got %v", got)
	}
}

func TestAudioClassifier_CloseStopsWorker(t *testing.T) {
	mock := &mockAudioBackend{scores: []float32{1}}
	c := NewAudioClassifier(mock)
	c.onWedged = func() {}

	_ = c.Classify(nil) // start the worker
	c.Close()
	if mock.closed != 1 {
		t.Errorf("backend Close called %d times, want 1", mock.closed)
	}
}
