package media

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph264"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtpmpeg4audio"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/mpeg4audio"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/mp4/codecs"
	"github.com/pion/rtp"

	"github.com/rvben/vedetta/internal/rtsp"
)

// SegmentWriter writes RTP packets into an fMP4 file.
type SegmentWriter struct {
	mu   sync.Mutex
	path string
	f    *os.File

	videoTrackID int
	audioTrackID int

	h264Format  *format.H264
	h264Decoder *rtph264.Decoder
	videoSPS    []byte
	videoPPS    []byte

	aacFormat  *format.MPEG4Audio
	aacDecoder *rtpmpeg4audio.Decoder
	aacConfig  *mpeg4audio.AudioSpecificConfig

	initWritten    bool
	seqNum         uint32
	videoDTS       uint64
	audioDTS       uint64
	startTime      time.Time
	lastVideoRTP  uint32
	hasFirstVideo bool
	hasAudio       bool
	videoTimeScale uint32
	audioTimeScale uint32
}

// NewSegmentWriter creates a new fMP4 segment writer.
func NewSegmentWriter(path string, video, audio *rtsp.TrackInfo) (*SegmentWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create segment file: %w", err)
	}

	sw := &SegmentWriter{
		path:           path,
		f:              f,
		videoTrackID:   1,
		audioTrackID:   2,
		startTime:      time.Now(),
		videoTimeScale: 90000,
		audioTimeScale: 90000,
	}

	if video != nil && video.Codec == "H264" {
		sw.videoSPS = video.SPS
		sw.videoPPS = video.PPS

		sw.h264Format = &format.H264{
			PayloadTyp:        96,
			PacketizationMode: 1,
			SPS:               video.SPS,
			PPS:               video.PPS,
		}
		dec, err := sw.h264Format.CreateDecoder()
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("create H264 decoder: %w", err)
		}
		sw.h264Decoder = dec
	}

	if audio != nil && audio.Codec == "AAC" {
		sw.hasAudio = true
		sw.audioTimeScale = uint32(audio.ClockRate)

		channels := audio.ChannelCount
		if channels <= 0 {
			channels = 1
		}
		channelConfig := uint8(channels)
		if channels == 8 {
			channelConfig = 7
		}

		sw.aacFormat = &format.MPEG4Audio{
			PayloadTyp: 97,
			Config: &mpeg4audio.AudioSpecificConfig{
				Type:          mpeg4audio.ObjectTypeAACLC,
				SampleRate:    audio.ClockRate,
				ChannelConfig: channelConfig,
			},
			SizeLength:       13,
			IndexLength:      3,
			IndexDeltaLength: 3,
		}
		dec, err := sw.aacFormat.CreateDecoder()
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("create AAC decoder: %w", err)
		}
		sw.aacDecoder = dec

		sw.aacConfig = &mpeg4audio.AudioSpecificConfig{
			Type:          mpeg4audio.ObjectTypeAACLC,
			SampleRate:    audio.ClockRate,
			ChannelConfig: channelConfig,
		}
	}

	return sw, nil
}

// WriteVideo processes a video RTP packet into the fMP4 segment.
func (sw *SegmentWriter) WriteVideo(pkt *rtp.Packet) error {
	if sw.h264Decoder == nil {
		return nil
	}

	au, err := sw.h264Decoder.Decode(pkt)
	if err != nil {
		return nil
	}

	sw.mu.Lock()
	defer sw.mu.Unlock()

	// Update SPS/PPS from in-band parameters
	for _, nalu := range au {
		if len(nalu) == 0 {
			continue
		}
		typ := h264.NALUType(nalu[0] & 0x1F)
		switch typ {
		case h264.NALUTypeSPS:
			sw.videoSPS = nalu
		case h264.NALUTypePPS:
			sw.videoPPS = nalu
		}
	}

	// Write the init segment on first keyframe
	if !sw.initWritten {
		if !h264.IsRandomAccess(au) {
			return nil
		}
		if sw.videoSPS == nil || sw.videoPPS == nil {
			return nil
		}
		if err := sw.writeInit(); err != nil {
			return err
		}
		sw.initWritten = true
		sw.lastVideoRTP = pkt.Timestamp
		sw.hasFirstVideo = true
	}

	// Compute sample duration from RTP timestamp delta
	var sampleDuration uint32
	if sw.hasFirstVideo {
		rtpDelta := pkt.Timestamp - sw.lastVideoRTP
		if rtpDelta > 0 && rtpDelta < sw.videoTimeScale*2 {
			sampleDuration = rtpDelta
		} else {
			sampleDuration = sw.videoTimeScale / 30 // ~33ms fallback
		}
	} else {
		sampleDuration = sw.videoTimeScale / 30
		sw.hasFirstVideo = true
	}
	sw.lastVideoRTP = pkt.Timestamp

	sample := &fmp4.Sample{
		Duration: sampleDuration,
	}
	if err := sample.FillH264(0, au); err != nil {
		return fmt.Errorf("fill H264 sample: %w", err)
	}

	part := fmp4.Part{
		SequenceNumber: sw.seqNum,
		Tracks: []*fmp4.PartTrack{
			{
				ID:       sw.videoTrackID,
				BaseTime: sw.videoDTS,
				Samples:  []*fmp4.Sample{sample},
			},
		},
	}

	if err := part.Marshal(sw.f); err != nil {
		return fmt.Errorf("marshal fmp4 part: %w", err)
	}

	sw.seqNum++
	sw.videoDTS += uint64(sample.Duration)

	return nil
}

// WriteAudio processes an audio RTP packet into the fMP4 segment.
func (sw *SegmentWriter) WriteAudio(pkt *rtp.Packet) error {
	if sw.aacDecoder == nil {
		return nil
	}

	aus, err := sw.aacDecoder.Decode(pkt)
	if err != nil {
		return nil
	}

	sw.mu.Lock()
	defer sw.mu.Unlock()

	if !sw.initWritten {
		return nil
	}

	// Each AAC access unit is one frame (1024 samples typically)
	for _, au := range aus {
		sample := &fmp4.Sample{
			Duration: 1024, // Standard AAC frame size in samples
			Payload:  au,
		}

		part := fmp4.Part{
			SequenceNumber: sw.seqNum,
			Tracks: []*fmp4.PartTrack{
				{
					ID:       sw.audioTrackID,
					BaseTime: sw.audioDTS,
					Samples:  []*fmp4.Sample{sample},
				},
			},
		}

		if err := part.Marshal(sw.f); err != nil {
			return fmt.Errorf("marshal audio part: %w", err)
		}

		sw.seqNum++
		sw.audioDTS += 1024
	}

	return nil
}

// Close finalizes the segment and returns its duration.
func (sw *SegmentWriter) Close() (time.Duration, error) {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	duration := time.Since(sw.startTime)

	if err := sw.f.Close(); err != nil {
		return duration, fmt.Errorf("close segment: %w", err)
	}

	return duration, nil
}

func (sw *SegmentWriter) writeInit() error {
	init := fmp4.Init{
		Tracks: []*fmp4.InitTrack{
			{
				ID:        sw.videoTrackID,
				TimeScale: sw.videoTimeScale,
				Codec: &codecs.H264{
					SPS: sw.videoSPS,
					PPS: sw.videoPPS,
				},
			},
		},
	}

	if sw.hasAudio && sw.aacConfig != nil {
		init.Tracks = append(init.Tracks, &fmp4.InitTrack{
			ID:        sw.audioTrackID,
			TimeScale: sw.audioTimeScale,
			Codec: &codecs.MPEG4Audio{
				Config: *sw.aacConfig,
			},
		})
	}

	return init.Marshal(sw.f)
}
