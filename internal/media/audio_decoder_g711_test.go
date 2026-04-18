package media

import (
	"testing"

	"github.com/pion/rtp"
)

func TestG711Decoder_ULaw(t *testing.T) {
	dec := NewG711Decoder(false) // false = μ-law
	defer func() { _ = dec.Close() }()

	pkt := &rtp.Packet{Payload: []byte{0xFF, 0xFF, 0xFF, 0xFF}}
	samples, rate, channels, err := dec.Decode(pkt)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rate != 8000 {
		t.Errorf("rate: got %d want 8000", rate)
	}
	if channels != 1 {
		t.Errorf("channels: got %d want 1", channels)
	}
	if len(samples) != 4 {
		t.Fatalf("samples len: got %d want 4", len(samples))
	}
	for i, s := range samples {
		if s != 0 {
			t.Errorf("sample[%d]: got %d want 0 (μ-law silence)", i, s)
		}
	}
}

func TestG711Decoder_ALaw(t *testing.T) {
	dec := NewG711Decoder(true) // true = A-law
	defer func() { _ = dec.Close() }()

	pkt := &rtp.Packet{Payload: []byte{0xD5, 0xD5}}
	samples, rate, _, err := dec.Decode(pkt)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rate != 8000 {
		t.Errorf("rate: got %d want 8000", rate)
	}
	if len(samples) != 2 {
		t.Fatalf("samples len: got %d want 2", len(samples))
	}
}

func TestG711Decoder_EmptyPayload(t *testing.T) {
	dec := NewG711Decoder(false)
	defer func() { _ = dec.Close() }()

	samples, _, _, err := dec.Decode(&rtp.Packet{Payload: nil})
	if err != nil {
		t.Fatalf("empty payload should not error, got %v", err)
	}
	if len(samples) != 0 {
		t.Errorf("expected empty samples, got %d", len(samples))
	}
}
