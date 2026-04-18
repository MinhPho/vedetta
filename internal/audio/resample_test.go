package audio

import (
	"math"
	"testing"
)

func TestResampleLinear_Passthrough(t *testing.T) {
	in := []int16{0, 100, 200, 300, 400, 500}
	out := ResampleLinear(in, 16000, 16000)
	if len(out) != len(in) {
		t.Fatalf("passthrough length: got %d want %d", len(out), len(in))
	}
	for i := range in {
		if out[i] != in[i] {
			t.Fatalf("passthrough sample %d: got %d want %d", i, out[i], in[i])
		}
	}
}

func TestResampleLinear_Upsample2x(t *testing.T) {
	// 8 kHz → 16 kHz: each input sample expands to two output samples.
	in := []int16{0, 1000, 2000, 3000}
	out := ResampleLinear(in, 8000, 16000)
	if got, want := len(out), 8; got != want {
		t.Fatalf("upsample length: got %d want %d", got, want)
	}
	// Endpoints must match input
	if out[0] != 0 {
		t.Errorf("out[0]: got %d want 0", out[0])
	}
	// Midpoint between in[0]=0 and in[1]=1000 should be ~500
	if math.Abs(float64(out[1])-500) > 1 {
		t.Errorf("out[1]: got %d want ~500", out[1])
	}
	if out[2] != 1000 {
		t.Errorf("out[2]: got %d want 1000", out[2])
	}
}

func TestResampleLinear_Downsample(t *testing.T) {
	// 32 kHz → 16 kHz: half the samples
	in := make([]int16, 32)
	for i := range in {
		in[i] = int16(i * 100)
	}
	out := ResampleLinear(in, 32000, 16000)
	if got, want := len(out), 16; got != want {
		t.Fatalf("downsample length: got %d want %d", got, want)
	}
	if out[0] != 0 {
		t.Errorf("out[0]: got %d want 0", out[0])
	}
}

func TestResampleLinear_44100to16000(t *testing.T) {
	// Realistic case: AAC at 44.1 kHz → YAMNet's 16 kHz.
	// 4410 input samples (100 ms) → ~1600 output samples.
	in := make([]int16, 4410)
	for i := range in {
		in[i] = int16(math.Sin(float64(i)*0.1) * 10000)
	}
	out := ResampleLinear(in, 44100, 16000)
	want := 1600
	if got := len(out); got < want-2 || got > want+2 {
		t.Errorf("44100→16000 length: got %d want ~%d", got, want)
	}
}

func TestPCM16ToFloat32(t *testing.T) {
	in := []int16{0, math.MaxInt16, math.MinInt16, 16384}
	out := PCM16ToFloat32(in)
	if len(out) != len(in) {
		t.Fatalf("length: got %d want %d", len(out), len(in))
	}
	if out[0] != 0 {
		t.Errorf("zero: got %f want 0", out[0])
	}
	if math.Abs(float64(out[1])-1.0) > 1e-4 {
		t.Errorf("max: got %f want ~1.0", out[1])
	}
	if math.Abs(float64(out[2])-(-1.0)) > 1e-4 {
		t.Errorf("min: got %f want ~-1.0", out[2])
	}
	if math.Abs(float64(out[3])-0.5) > 1e-3 {
		t.Errorf("half: got %f want ~0.5", out[3])
	}
}

func TestStereoToMono(t *testing.T) {
	// Interleaved L,R,L,R — average to mono.
	in := []int16{100, 200, 300, 400, 500, 600}
	out := StereoToMono(in)
	want := []int16{150, 350, 550}
	if len(out) != len(want) {
		t.Fatalf("length: got %d want %d", len(out), len(want))
	}
	for i := range want {
		if out[i] != want[i] {
			t.Errorf("sample %d: got %d want %d", i, out[i], want[i])
		}
	}
}
