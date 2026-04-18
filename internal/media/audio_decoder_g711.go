package media

import (
	"github.com/pion/rtp"

	"github.com/rvben/vedetta/internal/audio"
)

// G711Decoder decodes G.711 (PCMU or PCMA) RTP audio. G.711 is stateless and
// sample-aligned: one 8-bit byte → one int16 sample at 8 kHz mono. There is no
// session state to maintain across packets.
type G711Decoder struct {
	aLaw bool
}

// NewG711Decoder constructs a decoder. aLaw=true selects PCMA, false selects PCMU.
func NewG711Decoder(aLaw bool) *G711Decoder {
	return &G711Decoder{aLaw: aLaw}
}

// Decode converts the packet payload into int16 PCM samples at 8 kHz mono.
func (d *G711Decoder) Decode(pkt *rtp.Packet) ([]int16, int, int, error) {
	if len(pkt.Payload) == 0 {
		return nil, 8000, 1, nil
	}
	var samples []int16
	if d.aLaw {
		samples = audio.DecodeALaw(pkt.Payload)
	} else {
		samples = audio.DecodeULaw(pkt.Payload)
	}
	return samples, 8000, 1, nil
}

// Close releases decoder resources. G.711 has none.
func (d *G711Decoder) Close() error { return nil }
