package detect

import (
	"bytes"
	_ "embed"
	"encoding/csv"
	"fmt"
	"io"
)

// audiomodel/yamnet_class_map.csv is the official AudioSet ontology mapping
// shipped with YAMNet. The first column is the model output index, the third
// is the human-readable display name. We rely on row order matching index
// order — ParseYAMNetLabels validates this.
//
//go:embed audiomodel/yamnet_class_map.csv
var embeddedYAMNetClassMap []byte

// DefaultAudioLabels is the curated security-focused subset enabled when no
// explicit allowlist is configured. Every name here matches a display_name in
// the bundled YAMNet class map (verified by TestDefaultAudioLabels_AllInOntology).
var DefaultAudioLabels = []string{
	"Bark",
	"Shatter",
	"Smoke detector, smoke alarm",
	"Fire alarm",
	"Alarm",
	"Siren",
	"Civil defense siren",
	"Shout",
	"Screaming",
	"Gunshot, gunfire",
	"Baby cry, infant cry",
}

// ParseYAMNetLabels reads a YAMNet class map CSV (header: index,mid,display_name)
// and returns the display names in index order. Indices must be contiguous
// starting from 0; a gap or out-of-order row is an error because the model
// output position would no longer match the returned slice.
func ParseYAMNetLabels(r io.Reader) ([]string, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = 3
	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	if len(header) < 3 || header[0] != "index" || header[2] != "display_name" {
		return nil, fmt.Errorf("unexpected header: %v", header)
	}

	var labels []string
	expected := 0
	for {
		row, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}
		var idx int
		if _, err := fmt.Sscanf(row[0], "%d", &idx); err != nil {
			return nil, fmt.Errorf("parse index %q: %w", row[0], err)
		}
		if idx != expected {
			return nil, fmt.Errorf("expected index %d, got %d", expected, idx)
		}
		labels = append(labels, row[2])
		expected++
	}
	return labels, nil
}

// EmbeddedYAMNetLabels returns the bundled AudioSet ontology display names.
func EmbeddedYAMNetLabels() ([]string, error) {
	return ParseYAMNetLabels(bytes.NewReader(embeddedYAMNetClassMap))
}
