package media

import (
	"log/slog"
	"sync"
	"time"

	"github.com/pion/rtp"

	"github.com/rvben/vedetta/internal/audio"
)

const (
	// YAMNet input: 0.96 s of audio at 16 kHz = 15360 samples. We round up to
	// 15600 (the window size used by Google's reference TFLite YAMNet preset)
	// to match the bundled CPU model's expected input shape.
	AudioWindowSamples = 15600
	// AudioTargetRate is YAMNet's expected sample rate.
	AudioTargetRate = 16000
)

// AudioDecoder turns RTP audio packets into PCM int16 samples plus metadata.
// Implementations are codec-specific (G.711, AAC, etc.). They are owned by the
// AudioConsumer and closed by AudioConsumer.Close.
type AudioDecoder interface {
	Decode(pkt *rtp.Packet) (samples []int16, sampleRate int, channels int, err error)
	Close() error
}

// AudioConsumer implements rtsp.Consumer for the sound recognition pipeline.
// It decodes audio RTP packets, mixes down to mono, resamples to 16 kHz, and
// dispatches fixed-length float32 windows on Windows() for downstream
// classification.
//
// Window dispatch is non-blocking: if the classifier is slow, the oldest
// window is dropped. We prefer fresh audio over a backed-up queue.
type AudioConsumer struct {
	camera  string
	decoder AudioDecoder

	mu     sync.Mutex
	buffer []int16 // accumulated 16 kHz mono PCM
	closed bool

	windowCh chan []float32

	// Diagnostics
	pktCount    uint64
	decodeErrs  uint64
	winsEmitted uint64
	winsDropped uint64
	lastLog     time.Time
}

// NewAudioConsumer constructs a consumer wrapping decoder. The caller must
// call Close to release decoder resources.
func NewAudioConsumer(camera string, decoder AudioDecoder) *AudioConsumer {
	return &AudioConsumer{
		camera:   camera,
		decoder:  decoder,
		windowCh: make(chan []float32, 2),
		lastLog:  time.Now(),
	}
}

// Windows returns the channel of dispatched audio windows. Each window is
// AudioWindowSamples float32 samples in [-1, 1] at 16 kHz mono.
func (c *AudioConsumer) Windows() <-chan []float32 { return c.windowCh }

// Close releases the underlying decoder. It is safe to call multiple times.
func (c *AudioConsumer) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.mu.Unlock()
	if err := c.decoder.Close(); err != nil {
		slog.Warn("audio decoder close error", "camera", c.camera, "error", err)
	}
}

// OnVideoRTP is a no-op for the audio consumer.
func (c *AudioConsumer) OnVideoRTP(_ *rtp.Packet) {}

// OnDisconnect is a no-op; the buffer is preserved across reconnects so we
// don't lose nearly-complete windows on transient drops.
func (c *AudioConsumer) OnDisconnect() {}

// OnAudioRTP decodes a packet, normalizes to 16 kHz mono PCM, and dispatches
// any complete windows.
func (c *AudioConsumer) OnAudioRTP(pkt *rtp.Packet) {
	samples, srcRate, channels, err := c.decoder.Decode(pkt)

	c.mu.Lock()
	c.pktCount++
	if err != nil {
		c.decodeErrs++
		c.mu.Unlock()
		return
	}
	if len(samples) == 0 {
		c.mu.Unlock()
		return
	}

	mono := samples
	if channels >= 2 {
		mono = audio.StereoToMono(samples)
	}
	if srcRate != AudioTargetRate {
		mono = audio.ResampleLinear(mono, srcRate, AudioTargetRate)
	}
	c.buffer = append(c.buffer, mono...)

	for len(c.buffer) >= AudioWindowSamples {
		win := audio.PCM16ToFloat32(c.buffer[:AudioWindowSamples])
		c.buffer = c.buffer[AudioWindowSamples:]
		select {
		case c.windowCh <- win:
			c.winsEmitted++
		default:
			// Drain the oldest queued window and replace with this one so we
			// always deliver the freshest audio available.
			select {
			case <-c.windowCh:
				c.winsDropped++
			default:
			}
			c.windowCh <- win
			c.winsEmitted++
		}
	}

	if time.Since(c.lastLog) >= 5*time.Minute {
		slog.Info("audio status",
			"camera", c.camera,
			"rtp_packets", c.pktCount,
			"decode_errors", c.decodeErrs,
			"windows_emitted", c.winsEmitted,
			"windows_dropped", c.winsDropped,
		)
		c.pktCount = 0
		c.decodeErrs = 0
		c.winsEmitted = 0
		c.winsDropped = 0
		c.lastLog = time.Now()
	}
	c.mu.Unlock()
}
