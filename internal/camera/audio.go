package camera

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/rvben/vedetta/internal/detect"
	"github.com/rvben/vedetta/internal/media"
	"github.com/rvben/vedetta/internal/rtsp"
)

// AudioDetectorAPI is the subset of *detect.AudioDetector that Camera depends
// on. Defined as an interface so the audio loop can be unit-tested with a
// fake detector that returns canned events.
type AudioDetectorAPI interface {
	Detect(camera string, window []float32) []detect.AudioEvent
}

// audioTrackWaitTimeout caps how long runAudio waits for the RTSP source to
// publish its AudioTrack. Cameras without audio (or with audio negotiation
// failures) get logged once and skipped — the video pipeline keeps running.
const audioTrackWaitTimeout = 30 * time.Second

// pickAudioDecoder returns a media.AudioDecoder for the given codec, or nil
// if the codec is not supported in v1. Codecs are matched against the strings
// produced by rtsp.Source.extractTracks ("PCMU", "PCMA", "AAC", ...).
func pickAudioDecoder(codec string) media.AudioDecoder {
	switch codec {
	case "PCMU":
		return media.NewG711Decoder(false)
	case "PCMA":
		return media.NewG711Decoder(true)
	default:
		return nil
	}
}

// runAudio is the per-camera entry point for the sound recognition pipeline.
// It waits for the RTSP source's audio track, picks a decoder, attaches an
// AudioConsumer, and drains its windows through the shared AudioDetector.
//
// The detector and source are passed in (not pulled from the receiver) so the
// loop body itself stays free of network setup and can be exercised in tests.
func (c *Camera) runAudio(ctx context.Context, source *rtsp.Source, detector AudioDetectorAPI) {
	if detector == nil {
		return
	}

	audioTrack := waitForAudioTrack(ctx, source, audioTrackWaitTimeout)
	if audioTrack == nil {
		return
	}

	decoder := pickAudioDecoder(audioTrack.Codec)
	if decoder == nil {
		slog.Warn("audio codec not supported for sound recognition v1",
			"camera", c.config.Name,
			"codec", audioTrack.Codec,
		)
		return
	}

	consumer := media.NewAudioConsumer(c.config.Name, decoder)
	source.AddConsumer(consumer)
	defer source.RemoveConsumer(consumer)
	defer consumer.Close()

	slog.Info("sound recognition started",
		"camera", c.config.Name,
		"codec", audioTrack.Codec,
	)

	c.audioWindowLoop(ctx, consumer.Windows(), detector)
}

// waitForAudioTrack polls source for an audio track, giving up after timeout.
func waitForAudioTrack(ctx context.Context, source *rtsp.Source, timeout time.Duration) *rtsp.TrackInfo {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		if at := source.AudioTrack(); at != nil {
			return at
		}
		select {
		case <-ctx.Done():
			return nil
		case <-deadline.C:
			return nil
		case <-ticker.C:
		}
	}
}

// audioWindowLoop receives 16 kHz mono float32 windows from windows, runs
// each through detector, and emits a camera.Event per surviving label. The
// emit is non-blocking: if the events channel is full, the event is dropped
// and a warning is logged so we never wedge the audio path.
func (c *Camera) audioWindowLoop(ctx context.Context, windows <-chan []float32, detector AudioDetectorAPI) {
	for {
		select {
		case <-ctx.Done():
			return
		case window, ok := <-windows:
			if !ok {
				return
			}
			audioEvents := detector.Detect(c.config.Name, window)
			for _, ae := range audioEvents {
				ev := c.buildAudioEvent(ae)
				select {
				case c.events <- ev:
				default:
					slog.Warn("audio event dropped: events channel full",
						"camera", c.config.Name,
						"label", ae.Label,
					)
				}
			}
		}
	}
}

// buildAudioEvent constructs a camera.Event for a YAMNet detection. Audio
// events have no bounding box (Box stays at zero); when the camera has a
// recent video frame cached, attach it as the snapshot so MQTT/UI get a
// thumbnail.
func (c *Camera) buildAudioEvent(ae detect.AudioEvent) Event {
	now := time.Now()
	id := fmt.Sprintf("%s-aud-%d", c.config.Name, now.UnixNano())
	ev := Event{
		ID:         id,
		CameraName: c.config.Name,
		Label:      ae.Label,
		Score:      ae.Score,
		Timestamp:  now,
	}
	if c.eventSnapDir != "" {
		if snap := c.LastSnapshot(); snap != nil {
			ev.SnapshotPath = filepath.Join(c.eventSnapDir, c.config.Name, id+".jpg")
			ev.SnapshotImage = snap
		}
	}
	return ev
}
