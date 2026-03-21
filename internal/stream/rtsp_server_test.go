package stream

import (
	"testing"

	"github.com/rvben/vedetta/internal/rtsp"
)

func TestParseCameraName(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/front_door", "front_door"},
		{"/front_door/", "front_door"},
		{"/front_door/trackID=0", "front_door"},
		{"/kids_bedroom_2/trackID=1", "kids_bedroom_2"},
		{"/garage", "garage"},
		{"garage", "garage"},
		{"/", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := parseCameraName(tt.path)
		if got != tt.want {
			t.Errorf("parseCameraName(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestBuildDescription_NoTracks(t *testing.T) {
	source := rtsp.NewSource("rtsp://test:554/stream")

	desc, video, audio := buildDescription(source)
	if desc != nil || video != nil || audio != nil {
		t.Error("expected nil description for source with no tracks")
	}
}

func TestBuildDescription_H264Only(t *testing.T) {
	source := rtsp.NewSource("rtsp://test:554/stream")
	source.SetVideoTrack(&rtsp.TrackInfo{
		Codec:       "H264",
		ClockRate:   90000,
		IsVideo:     true,
		PayloadType: 96,
		SPS:         []byte{0x67, 0x42, 0x00, 0x1f},
		PPS:         []byte{0x68, 0xce, 0x38, 0x80},
	})

	desc, video, audio := buildDescription(source)
	if desc == nil {
		t.Fatal("expected non-nil description")
	}
	if video == nil {
		t.Fatal("expected non-nil video media")
	}
	if audio != nil {
		t.Error("expected nil audio media")
	}
	if len(desc.Medias) != 1 {
		t.Errorf("expected 1 media, got %d", len(desc.Medias))
	}
	if video.Formats[0].PayloadType() != 96 {
		t.Errorf("expected PT 96, got %d", video.Formats[0].PayloadType())
	}
}

func TestBuildDescription_H264WithAAC(t *testing.T) {
	source := rtsp.NewSource("rtsp://test:554/stream")
	source.SetVideoTrack(&rtsp.TrackInfo{
		Codec:       "H264",
		ClockRate:   90000,
		IsVideo:     true,
		PayloadType: 96,
	})
	source.SetAudioTrack(&rtsp.TrackInfo{
		Codec:        "AAC",
		ClockRate:    16000,
		PayloadType:  97,
		ChannelCount: 1,
	})

	desc, video, audio := buildDescription(source)
	if desc == nil || video == nil || audio == nil {
		t.Fatal("expected non-nil description, video, and audio")
	}
	if len(desc.Medias) != 2 {
		t.Errorf("expected 2 medias, got %d", len(desc.Medias))
	}
	if audio.Formats[0].PayloadType() != 97 {
		t.Errorf("expected audio PT 97, got %d", audio.Formats[0].PayloadType())
	}
}

func TestBuildDescription_H264WithPCMU(t *testing.T) {
	source := rtsp.NewSource("rtsp://test:554/stream")
	source.SetVideoTrack(&rtsp.TrackInfo{
		Codec:       "H264",
		ClockRate:   90000,
		IsVideo:     true,
		PayloadType: 96,
	})
	source.SetAudioTrack(&rtsp.TrackInfo{
		Codec:        "PCMU",
		ClockRate:    8000,
		PayloadType:  0,
		ChannelCount: 1,
	})

	desc, video, audio := buildDescription(source)
	if desc == nil || video == nil || audio == nil {
		t.Fatal("expected non-nil description, video, and audio")
	}
	if audio.Formats[0].PayloadType() != 0 {
		t.Errorf("expected audio PT 0 for PCMU, got %d", audio.Formats[0].PayloadType())
	}
}

func TestBuildDescription_H264WithPCMA(t *testing.T) {
	source := rtsp.NewSource("rtsp://test:554/stream")
	source.SetVideoTrack(&rtsp.TrackInfo{
		Codec:       "H264",
		ClockRate:   90000,
		IsVideo:     true,
		PayloadType: 96,
	})
	source.SetAudioTrack(&rtsp.TrackInfo{
		Codec:        "PCMA",
		ClockRate:    8000,
		PayloadType:  8,
		ChannelCount: 1,
	})

	desc, video, audio := buildDescription(source)
	if desc == nil || video == nil || audio == nil {
		t.Fatal("expected non-nil description, video, and audio")
	}
	if audio.Formats[0].PayloadType() != 8 {
		t.Errorf("expected audio PT 8 for PCMA, got %d", audio.Formats[0].PayloadType())
	}
}

func TestBuildDescription_ZeroPayloadTypeDefaults(t *testing.T) {
	source := rtsp.NewSource("rtsp://test:554/stream")
	source.SetVideoTrack(&rtsp.TrackInfo{
		Codec:       "H264",
		ClockRate:   90000,
		IsVideo:     true,
		PayloadType: 0, // upstream didn't set it
	})
	source.SetAudioTrack(&rtsp.TrackInfo{
		Codec:        "AAC",
		ClockRate:    16000,
		PayloadType:  0, // upstream didn't set it
		ChannelCount: 1,
	})

	desc, video, audio := buildDescription(source)
	if desc == nil || video == nil || audio == nil {
		t.Fatal("expected non-nil description, video, and audio")
	}
	if video.Formats[0].PayloadType() != 96 {
		t.Errorf("expected video PT default to 96, got %d", video.Formats[0].PayloadType())
	}
	if audio.Formats[0].PayloadType() != 97 {
		t.Errorf("expected audio PT default to 97, got %d", audio.Formats[0].PayloadType())
	}
}
