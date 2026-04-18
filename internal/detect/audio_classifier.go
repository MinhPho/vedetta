package detect

import (
	"log/slog"
	"os"
	"sync"
	"time"
)

// AudioBackend runs audio classification on a fixed-length float32 window.
// Implementations are codec-independent and operate on YAMNet's expected
// input shape (15600 float32 samples in [-1, 1] at 16 kHz mono). Output is
// per-class scores in [0, 1] with a fixed length matching the model's label
// count (521 for YAMNet/AudioSet).
//
// The signature is intentionally identical to the video Backend interface so
// the existing TFLiteBackend can be reused for YAMNet without changes.
type AudioBackend interface {
	Run(window []float32) (scores []float32, err error)
	Close()
	Name() string
}

// AudioClassifier wraps an AudioBackend with the same worker+timeout+watchdog
// pattern used by Detector. The motivation is identical: TFLite CGO calls have
// been observed to wedge in production, and a wedge in a shared classifier
// would freeze every camera's audio pipeline. This guarantees liveness.
type AudioClassifier struct {
	backend AudioBackend

	workerOnce sync.Once
	stopOnce   sync.Once
	requestCh  chan inferRequest
	stopCh     chan struct{}

	inferTimeout time.Duration
	wedgeLimit   time.Duration
	onWedged     func()
}

// NewAudioClassifier wraps backend. Call Close to stop the worker goroutine.
func NewAudioClassifier(backend AudioBackend) *AudioClassifier {
	return &AudioClassifier{backend: backend}
}

func (c *AudioClassifier) ensureWorker() {
	c.workerOnce.Do(func() {
		if c.inferTimeout == 0 {
			c.inferTimeout = defaultInferTimeout
		}
		if c.wedgeLimit == 0 {
			c.wedgeLimit = defaultWedgeLimit
		}
		if c.onWedged == nil {
			limit := c.wedgeLimit
			c.onWedged = func() {
				slog.Error("audio inference wedged beyond limit, exiting for restart",
					"limit", limit)
				os.Exit(1)
			}
		}
		c.requestCh = make(chan inferRequest, 1)
		c.stopCh = make(chan struct{})
		go c.workerLoop()
	})
}

func (c *AudioClassifier) workerLoop() {
	for {
		select {
		case <-c.stopCh:
			return
		case req, ok := <-c.requestCh:
			if !ok {
				return
			}
			wedge := time.AfterFunc(c.wedgeLimit, c.onWedged)
			output, err := c.backend.Run(req.input)
			wedge.Stop()
			select {
			case req.resultCh <- inferResult{output: output, err: err}:
			default:
			}
		}
	}
}

// Classify runs the backend on window and returns per-class scores. Safe for
// concurrent use — inference is serialized on a worker goroutine. Returns nil
// on busy worker, timeout, panic, or backend error so callers can simply skip
// the window without leaking blocked goroutines.
func (c *AudioClassifier) Classify(window []float32) (scores []float32) {
	c.ensureWorker()

	defer func() {
		if r := recover(); r != nil {
			slog.Error("audio inference panic recovered", "error", r)
			scores = nil
		}
	}()

	resultCh := make(chan inferResult, 1)
	select {
	case c.requestCh <- inferRequest{input: window, resultCh: resultCh}:
	default:
		slog.Warn("audio inference busy (previous call wedged)")
		return nil
	}

	select {
	case res := <-resultCh:
		if res.err != nil {
			slog.Warn("audio inference error", "error", res.err)
			return nil
		}
		return res.output
	case <-time.After(c.inferTimeout):
		slog.Warn("audio inference timeout", "after", c.inferTimeout)
		return nil
	}
}

// Close stops the worker goroutine and releases the backend. The backend
// reference is preserved so an in-flight workerLoop call doesn't nil-deref.
func (c *AudioClassifier) Close() {
	if c.backend == nil {
		return
	}
	c.stopOnce.Do(func() {
		if c.stopCh != nil {
			close(c.stopCh)
		}
	})
	c.backend.Close()
}
