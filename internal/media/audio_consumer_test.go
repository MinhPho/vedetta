package media

import (
	"testing"

	"github.com/pion/rtp"
)

// fakeDecoder emits a fixed PCM batch per RTP packet. It is used to drive the
// AudioConsumer windowing logic without depending on a real codec.
type fakeDecoder struct {
	samples    []int16
	sampleRate int
	channels   int
	err        error
	closed     bool
}

func (f *fakeDecoder) Decode(_ *rtp.Packet) ([]int16, int, int, error) {
	if f.err != nil {
		return nil, 0, 0, f.err
	}
	return f.samples, f.sampleRate, f.channels, nil
}

func (f *fakeDecoder) Close() error { f.closed = true; return nil }

func TestAudioConsumer_DispatchesWindowAfter15600Samples(t *testing.T) {
	// 16 kHz mono input — no resampling needed, every packet adds to the buffer.
	dec := &fakeDecoder{
		samples:    make([]int16, 1000),
		sampleRate: 16000,
		channels:   1,
	}
	for i := range dec.samples {
		dec.samples[i] = 1000
	}

	c := NewAudioConsumer("test-cam", dec)
	defer c.Close()

	// 15 packets * 1000 samples = 15000 — not enough for a window
	for i := 0; i < 15; i++ {
		c.OnAudioRTP(&rtp.Packet{})
	}
	select {
	case <-c.Windows():
		t.Fatal("window dispatched before 15600 samples accumulated")
	default:
	}

	// 16th packet brings total to 16000 — enough for one window (15600 samples)
	c.OnAudioRTP(&rtp.Packet{})
	select {
	case win := <-c.Windows():
		if got, want := len(win), AudioWindowSamples; got != want {
			t.Fatalf("window length: got %d want %d", got, want)
		}
		// All samples were 1000 / 32768 ≈ 0.0305
		want := float32(1000) / 32768.0
		for i, v := range win {
			if v != want {
				t.Fatalf("window[%d]: got %f want %f", i, v, want)
				break
			}
		}
	default:
		t.Fatal("expected a window after 16000 samples")
	}
}

func TestAudioConsumer_ResamplesAndMixesDown(t *testing.T) {
	// 8 kHz stereo input → must be mixed to mono and upsampled to 16 kHz.
	// 1 packet of 7800 samples (3900 stereo frames) at 8 kHz → 3900 mono samples
	// → 7800 samples at 16 kHz. So 2 packets fill one window.
	dec := &fakeDecoder{
		samples:    make([]int16, 7800),
		sampleRate: 8000,
		channels:   2,
	}
	for i := range dec.samples {
		dec.samples[i] = int16((i % 100) * 10)
	}

	c := NewAudioConsumer("test-cam", dec)
	defer c.Close()

	c.OnAudioRTP(&rtp.Packet{})
	select {
	case <-c.Windows():
		t.Fatal("window dispatched too early after first packet")
	default:
	}

	c.OnAudioRTP(&rtp.Packet{})
	select {
	case win := <-c.Windows():
		if got, want := len(win), AudioWindowSamples; got != want {
			t.Fatalf("window length: got %d want %d", got, want)
		}
	default:
		t.Fatal("expected a window after second packet")
	}
}

func TestAudioConsumer_SkipsOnDecodeError(t *testing.T) {
	dec := &fakeDecoder{err: errFake}
	c := NewAudioConsumer("test-cam", dec)
	defer c.Close()

	for i := 0; i < 100; i++ {
		c.OnAudioRTP(&rtp.Packet{})
	}
	select {
	case <-c.Windows():
		t.Fatal("window dispatched despite decode errors")
	default:
	}
}

func TestAudioConsumer_NonBlockingDispatch(t *testing.T) {
	// If downstream isn't reading, oldest window is dropped — caller never blocks.
	dec := &fakeDecoder{
		samples:    make([]int16, AudioWindowSamples),
		sampleRate: 16000,
		channels:   1,
	}
	c := NewAudioConsumer("test-cam", dec)
	defer c.Close()

	// Push enough audio to overflow the window channel several times.
	for i := 0; i < 10; i++ {
		c.OnAudioRTP(&rtp.Packet{})
	}
	// Should not have blocked. Drain channel.
	drained := 0
	for {
		select {
		case <-c.Windows():
			drained++
		default:
			if drained == 0 {
				t.Fatal("expected at least one window")
			}
			return
		}
	}
}

type fakeError string

func (e fakeError) Error() string { return string(e) }

var errFake = fakeError("fake decode error")
