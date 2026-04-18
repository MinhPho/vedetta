package audio

import "testing"

func TestDecodeULaw_Silence(t *testing.T) {
	// μ-law silence is 0xFF (positive zero) and 0x7F (negative zero); the
	// canonical "quiet" code is 0xFF which decodes to 0.
	in := []byte{0xFF, 0xFF, 0xFF}
	out := DecodeULaw(in)
	if len(out) != 3 {
		t.Fatalf("length: got %d want 3", len(out))
	}
	for i, s := range out {
		if s != 0 {
			t.Errorf("silence[%d]: got %d want 0", i, s)
		}
	}
}

func TestDecodeULaw_KnownValues(t *testing.T) {
	// μ-law has well-known reference decodings. Spot-check a handful.
	cases := []struct {
		in   byte
		want int16
	}{
		// Per ITU-T G.711 μ-law (and the SoX/libsndfile reference table):
		// 0xFF and 0x7F decode to "zero" (positive and negative zero); the
		// extreme bytes flip sign vs. naive intuition because μ-law inverts
		// every bit on the wire.
		{0xFF, 0},
		{0x7F, 0},
		{0x80, 32124},
		{0x00, -32124},
	}
	for _, c := range cases {
		got := DecodeULaw([]byte{c.in})[0]
		if got != c.want {
			t.Errorf("μ-law 0x%02X: got %d want %d", c.in, got, c.want)
		}
	}
}

func TestDecodeALaw_Silence(t *testing.T) {
	// A-law silence is 0xD5 (positive zero region).
	in := []byte{0xD5, 0xD5}
	out := DecodeALaw(in)
	for i, s := range out {
		if s != 8 && s != 0 {
			t.Errorf("silence[%d]: got %d want 0 or 8 (A-law has small step)", i, s)
		}
	}
}

func TestDecodeALaw_RangeIsBounded(t *testing.T) {
	// Sweep all 256 byte values and assert decoded samples stay within int16.
	in := make([]byte, 256)
	for i := range in {
		in[i] = byte(i)
	}
	out := DecodeALaw(in)
	if len(out) != 256 {
		t.Fatalf("length: got %d want 256", len(out))
	}
	// Symmetry: value pair (i ^ 0x80) should produce roughly opposite signs.
	for i := byte(0); i < 128; i++ {
		positive := DecodeALaw([]byte{i ^ 0x80 ^ 0x55})[0]
		negative := DecodeALaw([]byte{i ^ 0x55})[0]
		if (positive > 0) == (negative > 0) && positive != 0 {
			t.Errorf("A-law symmetry broken at i=%d: pos=%d neg=%d", i, positive, negative)
		}
	}
}
