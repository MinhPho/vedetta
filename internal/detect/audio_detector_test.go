package detect

import (
	"testing"
	"time"
)

// fakeScorer returns a fixed score vector for every Classify call.
type fakeScorer struct{ scores []float32 }

func (f *fakeScorer) Classify(_ []float32) []float32 { return f.scores }

func TestAudioDetector_EmitsAboveThreshold(t *testing.T) {
	labels := []string{"Bark", "Speech", "Glass shattering"}
	scores := []float32{0.10, 0.80, 0.65}
	d := NewAudioDetector(&fakeScorer{scores: scores}, labels, nil, 0.5, time.Second)

	events := d.Detect("cam1", make([]float32, 15600))
	if len(events) != 2 {
		t.Fatalf("event count: got %d want 2", len(events))
	}
	wantLabels := map[string]float32{"Speech": 0.80, "Glass shattering": 0.65}
	for _, ev := range events {
		want, ok := wantLabels[ev.Label]
		if !ok {
			t.Errorf("unexpected label %q", ev.Label)
			continue
		}
		if ev.Score != want {
			t.Errorf("%q score: got %f want %f", ev.Label, ev.Score, want)
		}
	}
}

func TestAudioDetector_DropsBelowThreshold(t *testing.T) {
	labels := []string{"Bark", "Speech"}
	d := NewAudioDetector(&fakeScorer{scores: []float32{0.4, 0.49}}, labels, nil, 0.5, time.Second)

	if events := d.Detect("cam1", nil); len(events) != 0 {
		t.Fatalf("expected no events, got %v", events)
	}
}

func TestAudioDetector_AllowlistFilter(t *testing.T) {
	labels := []string{"Bark", "Speech", "Glass shattering"}
	scores := []float32{0.9, 0.95, 0.9}
	allowed := []string{"Bark", "Glass shattering"}
	d := NewAudioDetector(&fakeScorer{scores: scores}, labels, allowed, 0.5, time.Second)

	events := d.Detect("cam1", nil)
	if len(events) != 2 {
		t.Fatalf("event count: got %d want 2", len(events))
	}
	for _, ev := range events {
		if ev.Label == "Speech" {
			t.Errorf("Speech leaked past allowlist filter")
		}
	}
}

func TestAudioDetector_CooldownSuppressesRepeats(t *testing.T) {
	labels := []string{"Bark"}
	d := NewAudioDetector(&fakeScorer{scores: []float32{0.9}}, labels, nil, 0.5, time.Hour)

	if got := d.Detect("cam1", nil); len(got) != 1 {
		t.Fatalf("first call: got %d events, want 1", len(got))
	}
	if got := d.Detect("cam1", nil); len(got) != 0 {
		t.Fatalf("second call within cooldown: got %d events, want 0", len(got))
	}
}

func TestAudioDetector_CooldownIsPerCamera(t *testing.T) {
	labels := []string{"Bark"}
	d := NewAudioDetector(&fakeScorer{scores: []float32{0.9}}, labels, nil, 0.5, time.Hour)

	if got := d.Detect("cam1", nil); len(got) != 1 {
		t.Fatalf("cam1 first: got %d, want 1", len(got))
	}
	if got := d.Detect("cam2", nil); len(got) != 1 {
		t.Fatalf("cam2 first should fire — cooldown is per-camera: got %d, want 1", len(got))
	}
}

func TestAudioDetector_CooldownIsPerLabel(t *testing.T) {
	labels := []string{"Bark", "Speech"}
	d := NewAudioDetector(&fakeScorer{scores: []float32{0.9, 0.9}}, labels, nil, 0.5, time.Hour)

	got := d.Detect("cam1", nil)
	if len(got) != 2 {
		t.Fatalf("first call: got %d events, want 2", len(got))
	}
	// Both labels suppressed on second call
	if got := d.Detect("cam1", nil); len(got) != 0 {
		t.Fatalf("second call: got %d events, want 0", len(got))
	}
}

func TestAudioDetector_CooldownExpires(t *testing.T) {
	labels := []string{"Bark"}
	d := NewAudioDetector(&fakeScorer{scores: []float32{0.9}}, labels, nil, 0.5, 50*time.Millisecond)

	if got := d.Detect("cam1", nil); len(got) != 1 {
		t.Fatalf("first call: got %d, want 1", len(got))
	}
	time.Sleep(70 * time.Millisecond)
	if got := d.Detect("cam1", nil); len(got) != 1 {
		t.Fatalf("after cooldown expiry: got %d, want 1", len(got))
	}
}

func TestAudioDetector_NilScoresProducesNoEvents(t *testing.T) {
	// Classifier returned nil (busy/timeout/wedged) — must be handled gracefully.
	d := NewAudioDetector(&fakeScorer{scores: nil}, []string{"Bark"}, nil, 0.5, time.Second)
	if got := d.Detect("cam1", nil); len(got) != 0 {
		t.Fatalf("expected no events on nil scores, got %d", len(got))
	}
}

func TestAudioDetector_LabelCountMismatchProducesNoEvents(t *testing.T) {
	// Classifier returned a different-length score vector — log and skip
	// rather than indexing out of range.
	d := NewAudioDetector(&fakeScorer{scores: []float32{0.9, 0.9, 0.9}}, []string{"Bark"}, nil, 0.5, time.Second)
	if got := d.Detect("cam1", nil); len(got) != 0 {
		t.Fatalf("expected no events on mismatched score length, got %d", len(got))
	}
}
