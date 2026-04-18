// Package audio provides PCM utilities for the sound recognition pipeline:
// channel mixdown, sample-rate conversion, and codec decoding helpers.
package audio

import "math"

// WindowSamples is YAMNet's input shape: 0.96 s of audio at 16 kHz mono,
// rounded up to the bundled CPU model preset. Defined once here so producers
// (media.AudioConsumer) and consumers (detect.YAMNetBackend) cannot drift.
const WindowSamples = 15600

// TargetRate is YAMNet's expected sample rate.
const TargetRate = 16000

// ResampleLinear converts in (PCM int16, single channel) from srcRate to dstRate
// using linear interpolation. Returns a fresh slice; in is not modified.
//
// Linear interpolation is sufficient for YAMNet input (the model is trained on
// real-world recordings with significant noise; the resampling artifacts from
// a polyphase filter would be in the noise floor anyway).
func ResampleLinear(in []int16, srcRate, dstRate int) []int16 {
	if srcRate == dstRate || len(in) == 0 {
		out := make([]int16, len(in))
		copy(out, in)
		return out
	}
	outLen := int(int64(len(in)) * int64(dstRate) / int64(srcRate))
	out := make([]int16, outLen)
	step := float64(srcRate) / float64(dstRate)
	for i := 0; i < outLen; i++ {
		srcPos := float64(i) * step
		idx := int(srcPos)
		frac := srcPos - float64(idx)
		if idx+1 >= len(in) {
			out[i] = in[len(in)-1]
			continue
		}
		a := float64(in[idx])
		b := float64(in[idx+1])
		out[i] = int16(math.Round(a + (b-a)*frac))
	}
	return out
}

// PCM16ToFloat32 converts int16 PCM samples to float32 in [-1, 1].
// YAMNet expects float32 samples in this range.
func PCM16ToFloat32(in []int16) []float32 {
	out := make([]float32, len(in))
	const scale = 1.0 / 32768.0
	for i, s := range in {
		out[i] = float32(float64(s) * scale)
	}
	return out
}

// StereoToMono averages interleaved L,R int16 PCM samples to mono.
// If the input length is odd, the trailing sample is dropped.
func StereoToMono(in []int16) []int16 {
	out := make([]int16, len(in)/2)
	for i := range out {
		out[i] = int16((int32(in[2*i]) + int32(in[2*i+1])) / 2)
	}
	return out
}
