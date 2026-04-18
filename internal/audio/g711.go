package audio

// G.711 decode tables. G.711 is an 8-bit, 8 kHz, mono, sample-by-sample
// codec used widely by SIP/RTP audio and (in some configurations) by IP
// cameras. There are two flavors: μ-law (PCMU, RTP payload type 0) and
// A-law (PCMA, RTP payload type 8). Both decode by lookup — no state.
//
// The lookup tables follow ITU-T G.711 and match the reference decodings
// shipped with libsndfile / SoX.

var ulawTable [256]int16
var alawTable [256]int16

func init() {
	for i := 0; i < 256; i++ {
		ulawTable[i] = ulawDecode(byte(i))
		alawTable[i] = alawDecode(byte(i))
	}
}

// DecodeULaw converts μ-law bytes to int16 PCM. One sample per input byte.
func DecodeULaw(in []byte) []int16 {
	out := make([]int16, len(in))
	for i, b := range in {
		out[i] = ulawTable[b]
	}
	return out
}

// DecodeALaw converts A-law bytes to int16 PCM. One sample per input byte.
func DecodeALaw(in []byte) []int16 {
	out := make([]int16, len(in))
	for i, b := range in {
		out[i] = alawTable[b]
	}
	return out
}

// ulawDecode implements the ITU-T G.711 μ-law expansion algorithm.
// Input bits are inverted on the wire; segment + mantissa form the
// magnitude; the high bit is the sign.
func ulawDecode(b byte) int16 {
	b = ^b
	sign := b & 0x80
	exponent := (b >> 4) & 0x07
	mantissa := b & 0x0F
	sample := int32(mantissa)<<3 + 0x84
	sample <<= exponent
	sample -= 0x84
	if sign != 0 {
		sample = -sample
	}
	return int16(sample)
}

// alawDecode implements the ITU-T G.711 A-law expansion algorithm.
// A-law samples are XORed with 0x55 on the wire; the result is split
// into sign + segment + quantized step.
func alawDecode(b byte) int16 {
	b ^= 0x55
	sign := b & 0x80
	exponent := (b >> 4) & 0x07
	mantissa := b & 0x0F
	var sample int32
	if exponent == 0 {
		sample = int32(mantissa)<<4 + 8
	} else {
		sample = (int32(mantissa)<<4 + 0x108) << (exponent - 1)
	}
	if sign == 0 {
		sample = -sample
	}
	return int16(sample)
}
