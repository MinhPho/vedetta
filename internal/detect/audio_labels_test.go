package detect

import (
	"strings"
	"testing"
)

func TestParseYAMNetLabels(t *testing.T) {
	csv := strings.NewReader(`index,mid,display_name
0,/m/09x0r,Speech
1,/m/0ytgt,"Child speech, kid speaking"
2,/m/01h8n0,Conversation
`)
	labels, err := ParseYAMNetLabels(csv)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []string{"Speech", "Child speech, kid speaking", "Conversation"}
	if len(labels) != len(want) {
		t.Fatalf("count: got %d want %d", len(labels), len(want))
	}
	for i := range want {
		if labels[i] != want[i] {
			t.Errorf("label[%d]: got %q want %q", i, labels[i], want[i])
		}
	}
}

func TestParseYAMNetLabels_RejectsNonContiguousIndices(t *testing.T) {
	// YAMNet's output index must equal the row index. If a row is missing
	// or out of order the result would silently mislabel events.
	csv := strings.NewReader(`index,mid,display_name
0,/m/09x0r,Speech
2,/m/01h8n0,Conversation
`)
	if _, err := ParseYAMNetLabels(csv); err == nil {
		t.Fatal("expected error on non-contiguous indices")
	}
}

func TestEmbeddedYAMNetLabels(t *testing.T) {
	labels, err := EmbeddedYAMNetLabels()
	if err != nil {
		t.Fatalf("load embedded labels: %v", err)
	}
	if got, want := len(labels), 521; got != want {
		t.Fatalf("label count: got %d want %d", got, want)
	}
	if labels[0] != "Speech" {
		t.Errorf("labels[0]: got %q want Speech", labels[0])
	}
	if labels[70] != "Bark" {
		t.Errorf("labels[70]: got %q want Bark", labels[70])
	}
	if labels[520] != "Field recording" {
		t.Errorf("labels[520]: got %q want 'Field recording'", labels[520])
	}
}

func TestDefaultAudioLabels_AllInOntology(t *testing.T) {
	// Every default label must exist in the YAMNet class map, otherwise
	// the allowlist filter would silently match nothing.
	labels, err := EmbeddedYAMNetLabels()
	if err != nil {
		t.Fatalf("load embedded labels: %v", err)
	}
	set := make(map[string]bool, len(labels))
	for _, l := range labels {
		set[l] = true
	}
	for _, l := range DefaultAudioLabels {
		if !set[l] {
			t.Errorf("default label %q not present in YAMNet class map", l)
		}
	}
}
