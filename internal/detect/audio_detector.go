package detect

import (
	"log/slog"
	"sync"
	"time"
)

// AudioEvent is a YAMNet classification above threshold for one (camera, label).
// It is an intermediate value: callers convert it to camera.Event before
// publishing to MQTT / writing to storage / triggering recording.
type AudioEvent struct {
	Label string
	Score float32
}

// AudioScorer turns a 16 kHz mono float32 window into per-class scores. The
// returned slice length must equal the configured label count. Returning nil
// is permitted (and preferred over partial results) when the underlying
// backend is busy, timed out, or errored.
type AudioScorer interface {
	Classify(window []float32) []float32
}

// AudioDetector is the coordinator for sound recognition. It runs windows
// through an AudioScorer, filters the result by an optional allowlist and a
// score threshold, applies a per-(camera, label) cooldown to suppress event
// spam, and returns the surviving events for downstream emission.
//
// The detector is shared across all cameras (one classifier, one model load)
// — camera identity is passed per-call.
type AudioDetector struct {
	scorer    AudioScorer
	labels    []string
	allowed   map[string]bool
	threshold float32
	cooldown  time.Duration

	mu       sync.Mutex
	lastSeen map[string]time.Time // key: camera + "|" + label
}

// NewAudioDetector wires the classifier together with its label table and
// filter knobs. allowed=nil disables the allowlist filter (all labels eligible).
// cooldown=0 disables suppression (every above-threshold window emits).
func NewAudioDetector(scorer AudioScorer, labels []string, allowed []string, threshold float32, cooldown time.Duration) *AudioDetector {
	d := &AudioDetector{
		scorer:    scorer,
		labels:    labels,
		threshold: threshold,
		cooldown:  cooldown,
		lastSeen:  make(map[string]time.Time),
	}
	if len(allowed) > 0 {
		d.allowed = make(map[string]bool, len(allowed))
		for _, l := range allowed {
			d.allowed[l] = true
		}
	}
	return d
}

// Detect classifies window and returns events for labels above threshold,
// passing the allowlist and not currently suppressed by cooldown.
func (d *AudioDetector) Detect(camera string, window []float32) []AudioEvent {
	scores := d.scorer.Classify(window)
	if scores == nil {
		return nil
	}
	if len(scores) != len(d.labels) {
		slog.Warn("audio score length mismatch",
			"camera", camera,
			"got", len(scores),
			"want", len(d.labels),
		)
		return nil
	}

	now := time.Now()
	var events []AudioEvent

	d.mu.Lock()
	defer d.mu.Unlock()
	for i, score := range scores {
		if score < d.threshold {
			continue
		}
		label := d.labels[i]
		if d.allowed != nil && !d.allowed[label] {
			continue
		}
		key := camera + "|" + label
		if d.cooldown > 0 {
			if last, ok := d.lastSeen[key]; ok && now.Sub(last) < d.cooldown {
				continue
			}
		}
		d.lastSeen[key] = now
		events = append(events, AudioEvent{Label: label, Score: score})
	}
	return events
}
