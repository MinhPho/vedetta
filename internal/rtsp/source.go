package rtsp

import (
	"context"
	"crypto/tls"
	"log/slog"
	"sync"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/pion/rtp"
)

// Source wraps a gortsplib RTSP client, providing reconnection and consumer fan-out.
type Source struct {
	url string

	mu         sync.RWMutex
	consumers  []Consumer
	videoTrack *TrackInfo
	audioTrack *TrackInfo
	connected  bool
}

// NewSource creates a new RTSP source for the given URL.
func NewSource(url string) *Source {
	return &Source{url: url}
}

// URL returns the RTSP URL of this source.
func (s *Source) URL() string {
	return s.url
}

// AddConsumer registers a consumer to receive RTP packets.
func (s *Source) AddConsumer(c Consumer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consumers = append(s.consumers, c)
}

// RemoveConsumer unregisters a consumer.
func (s *Source) RemoveConsumer(c Consumer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.consumers {
		if existing == c {
			s.consumers = append(s.consumers[:i], s.consumers[i+1:]...)
			break
		}
	}
}

// ConsumerCount returns the number of active consumers.
func (s *Source) ConsumerCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.consumers)
}

// VideoTrack returns the video track info, or nil if not yet connected.
func (s *Source) VideoTrack() *TrackInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.videoTrack
}

// AudioTrack returns the audio track info, or nil if not available.
func (s *Source) AudioTrack() *TrackInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.audioTrack
}

// SetVideoTrack sets the video track info (for testing).
func (s *Source) SetVideoTrack(ti *TrackInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.videoTrack = ti
}

// SetAudioTrack sets the audio track info (for testing).
func (s *Source) SetAudioTrack(ti *TrackInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audioTrack = ti
}

// Connected returns whether the source is currently connected.
func (s *Source) Connected() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.connected
}

// Connect starts reading from the RTSP stream, reconnecting on failure.
// Blocks until ctx is cancelled.
func (s *Source) Connect(ctx context.Context) {
	backoff := 5 * time.Second
	const maxBackoff = 2 * time.Minute

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		err := s.connectOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			// Successful connection ended cleanly (e.g., server closed).
			// Reset backoff for quick reconnect.
			backoff = time.Second
		}

		if err != nil {
			slog.Error("RTSP connection error, reconnecting",
				"url", SanitizeURL(s.url),
				"error", err,
				"retry_in", backoff,
			)
		} else {
			slog.Info("RTSP connection closed, reconnecting", "url", SanitizeURL(s.url))
		}

		s.notifyDisconnect()

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		backoff = time.Duration(float64(backoff) * 1.5)
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (s *Source) notifyDisconnect() {
	s.mu.Lock()
	s.connected = false
	consumers := make([]Consumer, len(s.consumers))
	copy(consumers, s.consumers)
	s.mu.Unlock()

	for _, c := range consumers {
		c.OnDisconnect()
	}
}

func (s *Source) connectOnce(ctx context.Context) error {
	u, err := base.ParseURL(s.url)
	if err != nil {
		return err
	}

	proto := gortsplib.ProtocolTCP
	client := &gortsplib.Client{
		Scheme:   u.Scheme,
		Host:     u.Host,
		Protocol: &proto,
		TLSConfig: &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // UniFi Protect uses self-signed certs
		},
	}

	if err := client.Start(); err != nil {
		return err
	}
	defer client.Close()

	desc, _, err := client.Describe(u)
	if err != nil {
		return err
	}

	s.extractTracks(desc)

	if err := client.SetupAll(desc.BaseURL, desc.Medias); err != nil {
		return err
	}

	// Register a single RTP handler that dispatches by media type.
	// OnPacketRTPAny sets one global handler — calling it in a loop
	// would replace the previous handler on each iteration.
	client.OnPacketRTPAny(func(medi *description.Media, _ format.Format, pkt *rtp.Packet) {
		if medi.Type == description.MediaTypeVideo {
			s.fanOutVideo(pkt)
		} else {
			s.fanOutAudio(pkt)
		}
	})

	if _, err := client.Play(nil); err != nil {
		return err
	}

	s.mu.Lock()
	s.connected = true
	s.mu.Unlock()

	videoCodec, audioCodec := "none", "none"
	if vt := s.VideoTrack(); vt != nil {
		videoCodec = vt.Codec
	}
	if at := s.AudioTrack(); at != nil {
		audioCodec = at.Codec
	}
	slog.Info("RTSP connected",
		"url", SanitizeURL(s.url),
		"video_codec", videoCodec,
		"audio_codec", audioCodec,
	)

	// Wait blocks until the client encounters a fatal error or is closed
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- client.Wait()
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-waitDone:
		return err
	}
}

func (s *Source) extractTracks(desc *description.Session) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, media := range desc.Medias {
		for _, forma := range media.Formats {
			switch f := forma.(type) {
			case *format.H264:
				ti := &TrackInfo{
					Codec:     "H264",
					ClockRate: f.ClockRate(),
					IsVideo:     true,
					PayloadType: f.PayloadType(),
				}
				if f.SPS != nil {
					ti.SPS = make([]byte, len(f.SPS))
					copy(ti.SPS, f.SPS)
				}
				if f.PPS != nil {
					ti.PPS = make([]byte, len(f.PPS))
					copy(ti.PPS, f.PPS)
				}
				s.videoTrack = ti

			case *format.MPEG4Audio:
				channels := 1
				if f.Config != nil && f.Config.ChannelConfig > 0 {
					channels = int(f.Config.ChannelConfig)
					if channels == 7 {
						channels = 8
					}
				}
				s.audioTrack = &TrackInfo{
					Codec:        "AAC",
					ClockRate:    f.ClockRate(),
					PayloadType:  f.PayloadType(),
					ChannelCount: channels,
				}

			case *format.G711:
				codec := "PCMU"
				if !f.MULaw {
					codec = "PCMA"
				}
				s.audioTrack = &TrackInfo{
					Codec:        codec,
					ClockRate:    f.ClockRate(),
					PayloadType:  f.PayloadType(),
					ChannelCount: f.ChannelCount,
				}
			}
		}
	}
}

func (s *Source) fanOutVideo(pkt *rtp.Packet) {
	start := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.consumers {
		c.OnVideoRTP(pkt)
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		slog.Warn("slow fanOutVideo", "url", SanitizeURL(s.url), "elapsed", elapsed, "consumers", len(s.consumers))
	}
}

func (s *Source) fanOutAudio(pkt *rtp.Packet) {
	start := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.consumers {
		c.OnAudioRTP(pkt)
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		slog.Warn("slow fanOutAudio", "url", SanitizeURL(s.url), "elapsed", elapsed, "consumers", len(s.consumers))
	}
}
