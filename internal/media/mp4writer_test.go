package media

import (
	"image"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/pion/rtp"

	"github.com/rvben/vedetta/internal/rtsp"
)

func TestSegmentWriter_WriteAndClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.mp4")

	sps := []byte{0x67, 0x42, 0x00, 0x0a, 0xf8, 0x41, 0xa2}
	pps := []byte{0x68, 0xce, 0x38, 0x80}

	video := &rtsp.TrackInfo{
		Codec:     "H264",
		ClockRate: 90000,
		IsVideo:   true,
		SPS:       sps,
		PPS:       pps,
	}

	sw, err := NewSegmentWriter(path, video, nil)
	if err != nil {
		t.Fatalf("NewSegmentWriter: %v", err)
	}

	dur, err := sw.Close()
	if err != nil {
		t.Fatalf("Close: %v", err)
	}

	if dur <= 0 {
		t.Errorf("duration = %v, want > 0", dur)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info == nil {
		t.Fatal("file doesn't exist")
	}
}

func TestSegmentWriter_VideoOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "video_only.mp4")

	video := &rtsp.TrackInfo{
		Codec:     "H264",
		ClockRate: 90000,
		IsVideo:   true,
		SPS:       []byte{0x67, 0x42, 0x00, 0x0a, 0xf8, 0x41, 0xa2},
		PPS:       []byte{0x68, 0xce, 0x38, 0x80},
	}

	sw, err := NewSegmentWriter(path, video, nil)
	if err != nil {
		t.Fatalf("NewSegmentWriter: %v", err)
	}

	err = sw.WriteAudio(&rtp.Packet{Payload: []byte{0xFF}})
	if err != nil {
		t.Errorf("WriteAudio should be no-op without audio track, got: %v", err)
	}

	_, err = sw.Close()
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestSegmentWriter_WithAudio(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "av.mp4")

	video := &rtsp.TrackInfo{
		Codec:     "H264",
		ClockRate: 90000,
		IsVideo:   true,
		SPS:       []byte{0x67, 0x42, 0x00, 0x0a, 0xf8, 0x41, 0xa2},
		PPS:       []byte{0x68, 0xce, 0x38, 0x80},
	}
	audio := &rtsp.TrackInfo{
		Codec:     "AAC",
		ClockRate: 48000,
	}

	sw, err := NewSegmentWriter(path, video, audio)
	if err != nil {
		t.Fatalf("NewSegmentWriter: %v", err)
	}

	if !sw.hasAudio {
		t.Error("expected hasAudio to be true")
	}

	_, err = sw.Close()
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestSegmentWriter_NilTrack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nil.mp4")

	sw, err := NewSegmentWriter(path, nil, nil)
	if err != nil {
		t.Fatalf("NewSegmentWriter: %v", err)
	}

	err = sw.WriteVideo(&rtp.Packet{Payload: []byte{0x65, 0x88}})
	if err != nil {
		t.Errorf("WriteVideo with nil track: %v", err)
	}

	_, err = sw.Close()
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestSegmentWriter_WaitsForKeyframe(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keyframe.mp4")

	video := &rtsp.TrackInfo{
		Codec:     "H264",
		ClockRate: 90000,
		IsVideo:   true,
		SPS:       []byte{0x67, 0x42, 0x00, 0x0a, 0xf8, 0x41, 0xa2},
		PPS:       []byte{0x68, 0xce, 0x38, 0x80},
	}

	sw, err := NewSegmentWriter(path, video, nil)
	if err != nil {
		t.Fatalf("NewSegmentWriter: %v", err)
	}
	defer sw.Close()

	if sw.initWritten {
		t.Error("init should not be written before first keyframe")
	}
}

func TestIsRandomAccess(t *testing.T) {
	idr := [][]byte{{0x65, 0x88, 0x00}}
	if !h264.IsRandomAccess(idr) {
		t.Error("expected IDR to be random access")
	}

	nonIdr := [][]byte{{0x41, 0x9a, 0x00}}
	if h264.IsRandomAccess(nonIdr) {
		t.Error("expected non-IDR to not be random access")
	}
}

func TestYCbCrToRGB24Scaled(t *testing.T) {
	t.Run("identity", func(t *testing.T) {
		rgb := ycbcrToRGB24Scaled(createTestYCbCr(4, 4), 4, 4)
		if len(rgb) != 4*4*3 {
			t.Errorf("expected %d bytes, got %d", 4*4*3, len(rgb))
		}
	})

	t.Run("downscale", func(t *testing.T) {
		rgb := ycbcrToRGB24Scaled(createTestYCbCr(8, 8), 4, 4)
		if len(rgb) != 4*4*3 {
			t.Errorf("expected %d bytes, got %d", 4*4*3, len(rgb))
		}
	})
}

func createTestYCbCr(w, h int) *image.YCbCr {
	return &image.YCbCr{
		Y:              make([]byte, w*h),
		Cb:             make([]byte, (w/2)*(h/2)),
		Cr:             make([]byte, (w/2)*(h/2)),
		YStride:        w,
		CStride:        w / 2,
		SubsampleRatio: image.YCbCrSubsampleRatio420,
		Rect:           image.Rect(0, 0, w, h),
	}
}

func TestSegmentWriter_Duration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dur.mp4")

	video := &rtsp.TrackInfo{
		Codec:     "H264",
		ClockRate: 90000,
		IsVideo:   true,
		SPS:       []byte{0x67, 0x42, 0x00, 0x0a, 0xf8, 0x41, 0xa2},
		PPS:       []byte{0x68, 0xce, 0x38, 0x80},
	}

	sw, err := NewSegmentWriter(path, video, nil)
	if err != nil {
		t.Fatalf("NewSegmentWriter: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	dur, err := sw.Close()
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if dur < 50*time.Millisecond {
		t.Errorf("duration = %v, expected >= 50ms", dur)
	}
}
