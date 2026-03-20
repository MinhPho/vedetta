package media

import (
	"image"
	"log/slog"
	"sync"
	"time"

	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph264"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/pion/rtp"

	"github.com/rvben/vedetta/internal/rtsp"
)

// RawFrame holds a decoded RGB24 frame for detection.
type RawFrame struct {
	Data   []byte
	Width  int
	Height int
}

// DetectConsumer implements rtsp.Consumer and decodes H264 keyframes to RGB24.
type DetectConsumer struct {
	width  int
	height int

	h264Decoder *rtph264.Decoder
	h264Dec     *H264Decoder // OpenH264 pixel decoder
	sps         []byte
	pps         []byte
	useFallback bool // true when OpenH264 is unavailable

	mu         sync.Mutex
	frameCh    chan RawFrame
	lastFrame  time.Time
	frameDelay time.Duration
}

// NewDetectConsumer creates a consumer that decodes H264 keyframes for detection.
func NewDetectConsumer(width, height, fps int, track *rtsp.TrackInfo) *DetectConsumer {
	dc := &DetectConsumer{
		width:      width,
		height:     height,
		frameCh:    make(chan RawFrame, 2),
		frameDelay: time.Second / time.Duration(max(fps, 1)),
	}

	if track != nil && track.Codec == "H264" {
		dc.sps = track.SPS
		dc.pps = track.PPS

		h264Format := &format.H264{
			PayloadTyp:        96,
			PacketizationMode: 1,
			SPS:               track.SPS,
			PPS:               track.PPS,
		}
		dec, err := h264Format.CreateDecoder()
		if err != nil {
			slog.Warn("failed to create H264 RTP decoder for detection", "error", err)
		} else {
			dc.h264Decoder = dec
		}

		// Try to create OpenH264 pixel decoder
		dc.h264Dec = NewH264Decoder()
		if dc.h264Dec == nil {
			dc.useFallback = true
			slog.Warn("detection using compressed-data fallback (install libopenh264 for real decode)")
		} else {
			slog.Info("detection using OpenH264 hardware decode")
		}
	}

	return dc
}

// Frames returns the channel of decoded frames.
func (dc *DetectConsumer) Frames() <-chan RawFrame {
	return dc.frameCh
}

// Close releases decoder resources.
func (dc *DetectConsumer) Close() {
	if dc.h264Dec != nil {
		dc.h264Dec.Close()
		dc.h264Dec = nil
	}
}

// OnVideoRTP processes a video RTP packet, decoding keyframes to RGB24.
func (dc *DetectConsumer) OnVideoRTP(pkt *rtp.Packet) {
	if dc.h264Decoder == nil {
		return
	}

	au, err := dc.h264Decoder.Decode(pkt)
	if err != nil {
		return
	}

	// Rate limit
	dc.mu.Lock()
	if time.Since(dc.lastFrame) < dc.frameDelay {
		dc.mu.Unlock()
		return
	}
	dc.mu.Unlock()

	// Only decode keyframes for detection
	if !h264.IsRandomAccess(au) {
		return
	}

	// Update SPS/PPS from in-band parameters
	for _, nalu := range au {
		if len(nalu) == 0 {
			continue
		}
		typ := h264.NALUType(nalu[0] & 0x1F)
		switch typ {
		case h264.NALUTypeSPS:
			dc.sps = nalu
		case h264.NALUTypePPS:
			dc.pps = nalu
		}
	}

	if dc.sps == nil {
		return
	}

	var rgb24 []byte
	if dc.useFallback {
		rgb24 = dc.decodeIDRFallback(au)
	} else {
		rgb24 = dc.decodeIDROpenH264(au)
	}

	if rgb24 == nil {
		return
	}

	dc.mu.Lock()
	dc.lastFrame = time.Now()
	dc.mu.Unlock()

	select {
	case dc.frameCh <- RawFrame{Data: rgb24, Width: dc.width, Height: dc.height}:
	default:
	}
}

// OnAudioRTP is a no-op for detection.
func (dc *DetectConsumer) OnAudioRTP(_ *rtp.Packet) {}

// OnDisconnect is called when the source disconnects.
func (dc *DetectConsumer) OnDisconnect() {}

// decodeIDROpenH264 uses OpenH264 to decode NAL units to RGB24.
func (dc *DetectConsumer) decodeIDROpenH264(au [][]byte) []byte {
	// Build NAL unit stream with start codes for OpenH264
	var nalStream []byte
	startCode := []byte{0, 0, 0, 1}
	for _, nalu := range au {
		if len(nalu) == 0 {
			continue
		}
		nalStream = append(nalStream, startCode...)
		nalStream = append(nalStream, nalu...)
	}

	ycbcr := dc.h264Dec.Decode(nalStream)
	if ycbcr == nil {
		return nil
	}

	return ycbcrToRGB24Scaled(ycbcr, dc.width, dc.height)
}

// decodeIDRFallback uses compressed data as pseudo-pixels when OpenH264 is unavailable.
// This gives motion detection frame-to-frame variation but not real image data.
func (dc *DetectConsumer) decodeIDRFallback(au [][]byte) []byte {
	var sps h264.SPS
	if err := sps.Unmarshal(dc.sps); err != nil {
		return nil
	}

	w := int((sps.PicWidthInMbsMinus1 + 1) * 16)
	h := int((sps.PicHeightInMapUnitsMinus1 + 1) * 16)
	if !sps.FrameMbsOnlyFlag {
		h *= 2
	}

	img := image.NewRGBA(image.Rect(0, 0, w, h))

	for _, nalu := range au {
		if len(nalu) == 0 {
			continue
		}
		naluType := h264.NALUType(nalu[0] & 0x1F)
		if naluType == h264.NALUTypeIDR && len(nalu) > 1 {
			pix := img.Pix
			dataIdx := 1
			for i := 0; i < len(pix) && dataIdx < len(nalu); i += 4 {
				v := nalu[dataIdx]
				pix[i+0] = v
				pix[i+1] = v
				pix[i+2] = v
				pix[i+3] = 255
				dataIdx++
			}
			break
		}
	}

	return rgbaToRGB24Scaled(img, dc.width, dc.height)
}

// rgbaToRGB24Scaled converts an RGBA image to RGB24 bytes at the target resolution.
func rgbaToRGB24Scaled(img *image.RGBA, targetW, targetH int) []byte {
	srcBounds := img.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()

	rgb := make([]byte, targetW*targetH*3)

	for y := range targetH {
		srcY := y * srcH / targetH
		for x := range targetW {
			srcX := x * srcW / targetW
			si := srcY*img.Stride + srcX*4
			di := (y*targetW + x) * 3
			if si+2 < len(img.Pix) {
				rgb[di+0] = img.Pix[si+0]
				rgb[di+1] = img.Pix[si+1]
				rgb[di+2] = img.Pix[si+2]
			}
		}
	}

	return rgb
}
