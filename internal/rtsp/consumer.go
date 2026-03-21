package rtsp

import "github.com/pion/rtp"

// Consumer receives RTP packets from a Source.
type Consumer interface {
	OnVideoRTP(pkt *rtp.Packet)
	OnAudioRTP(pkt *rtp.Packet)
	OnDisconnect()
}

// TrackInfo describes a media track discovered via SDP.
type TrackInfo struct {
	Codec        string // "H264", "AAC", "PCMA", etc.
	ClockRate    int
	IsVideo      bool
	PayloadType  uint8  // RTP payload type from upstream SDP
	SPS, PPS     []byte // H264-specific
	ChannelCount int    // Audio channel count (1=mono, 2=stereo)
}
