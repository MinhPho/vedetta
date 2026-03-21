package stream

import (
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"github.com/rvben/vedetta/internal/rtsp"
)

// StreamManager manages per-camera WebRTC sessions with direct RTP forwarding.
type StreamManager struct {
	hub *rtsp.Hub
	mu  sync.Mutex
	// One webrtcConsumer per camera URL, shared across all peers watching that camera.
	consumers map[string]*webrtcConsumer
}

type peerState struct {
	pc    *webrtc.PeerConnection
	track *webrtc.TrackLocalStaticRTP

	mu           sync.Mutex
	seqOffset    uint16
	tsOffset     uint32
	started      bool
	keyframeSeen bool
}

func (p *peerState) write(pkt *rtp.Packet) error {
	p.mu.Lock()
	if !p.keyframeSeen {
		if isKeyframe(pkt) {
			p.keyframeSeen = true
		} else {
			p.mu.Unlock()
			return nil
		}
	}

	if !p.started {
		p.seqOffset = -pkt.Header.SequenceNumber
		p.tsOffset = -pkt.Header.Timestamp
		p.started = true
	}
	seq := pkt.Header.SequenceNumber + p.seqOffset
	ts := pkt.Header.Timestamp + p.tsOffset
	p.mu.Unlock()

	clone := *pkt
	clone.Header.SequenceNumber = seq
	clone.Header.Timestamp = ts
	return p.track.WriteRTP(&clone)
}

// isKeyframe checks if an RTP packet contains the start of an H264 IDR frame.
func isKeyframe(pkt *rtp.Packet) bool {
	if len(pkt.Payload) < 2 {
		return false
	}

	nalType := pkt.Payload[0] & 0x1f

	switch {
	case nalType >= 1 && nalType <= 23:
		// Single NAL unit: type 5 = IDR, type 7 = SPS
		return nalType == 5 || nalType == 7
	case nalType == 24:
		// STAP-A: check first NAL inside
		if len(pkt.Payload) < 4 {
			return false
		}
		innerNALType := pkt.Payload[3] & 0x1f
		return innerNALType == 5 || innerNALType == 7
	case nalType == 28:
		// FU-A: check start bit and NAL type
		startBit := pkt.Payload[1] & 0x80
		fuNALType := pkt.Payload[1] & 0x1f
		return startBit != 0 && (fuNALType == 5 || fuNALType == 7)
	}

	return false
}

// webrtcConsumer implements rtsp.Consumer and forwards RTP to WebRTC peers.
type webrtcConsumer struct {
	mu    sync.RWMutex
	peers []*peerState
}

func (wc *webrtcConsumer) OnVideoRTP(pkt *rtp.Packet) {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	for _, p := range wc.peers {
		if err := p.write(pkt); err != nil {
			slog.Debug("failed to write RTP to peer", "error", err)
		}
	}
}

func (wc *webrtcConsumer) OnAudioRTP(_ *rtp.Packet) {}
func (wc *webrtcConsumer) OnDisconnect()             {}

func (wc *webrtcConsumer) addPeer(peer *peerState) {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	wc.peers = append(wc.peers, peer)
}

func (wc *webrtcConsumer) removePeer(peer *peerState) int {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	for i, p := range wc.peers {
		if p == peer {
			wc.peers = append(wc.peers[:i], wc.peers[i+1:]...)
			break
		}
	}
	return len(wc.peers)
}

// NewStreamManager creates a stream manager that uses an RTSP Hub for direct forwarding.
func NewStreamManager(hub *rtsp.Hub) *StreamManager {
	return &StreamManager{
		hub:       hub,
		consumers: make(map[string]*webrtcConsumer),
	}
}

// HandleOffer processes a WebRTC SDP offer and returns an SDP answer.
func (sm *StreamManager) HandleOffer(cameraName, rtspURL string, offer webrtc.SessionDescription) (*webrtc.SessionDescription, error) {
	// Build H264 codec capability with profile-level-id from camera SPS
	sdpFmtpLine := "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42001f"
	source := sm.hub.GetOrCreate(rtspURL)
	if vt := source.VideoTrack(); vt != nil && len(vt.SPS) >= 3 {
		profileLevelID := hex.EncodeToString(vt.SPS[1:4])
		sdpFmtpLine = fmt.Sprintf("level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=%s", profileLevelID)
	}

	// Register only the H264 codec we'll actually send.
	// This ensures the SDP answer contains exactly one codec, so the browser
	// knows which payload type to expect.
	me := &webrtc.MediaEngine{}
	if err := me.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeH264,
			ClockRate:   90000,
			SDPFmtpLine: sdpFmtpLine,
		},
		PayloadType: 96,
	}, webrtc.RTPCodecTypeVideo); err != nil {
		return nil, fmt.Errorf("register codec: %w", err)
	}

	// Force IPv4 only — IPv6 UDP causes packet loss on some networks
	se := webrtc.SettingEngine{}
	se.SetIPFilter(func(ip net.IP) bool {
		return ip.To4() != nil
	})
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})

	api := webrtc.NewAPI(webrtc.WithMediaEngine(me), webrtc.WithSettingEngine(se))
	pc, err := api.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create peer connection: %w", err)
	}

	videoTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeH264,
			ClockRate:   90000,
			SDPFmtpLine: sdpFmtpLine,
		},
		"video",
		fmt.Sprintf("vedetta-%s", cameraName),
	)
	if err != nil {
		_ = pc.Close()
		return nil, fmt.Errorf("create video track: %w", err)
	}

	if _, err := pc.AddTrack(videoTrack); err != nil {
		_ = pc.Close()
		return nil, fmt.Errorf("add track: %w", err)
	}

	peer := &peerState{pc: pc, track: videoTrack}

	if sm.hub == nil {
		_ = pc.Close()
		return nil, fmt.Errorf("no RTSP hub configured")
	}

	// Get or create the consumer for this RTSP URL
	consumer := sm.getOrCreateConsumer(rtspURL)
	consumer.addPeer(peer)

	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		slog.Info("WebRTC ICE state changed", "camera", cameraName, "state", state.String())
		if state == webrtc.ICEConnectionStateFailed || state == webrtc.ICEConnectionStateDisconnected || state == webrtc.ICEConnectionStateClosed {
			remaining := consumer.removePeer(peer)
			_ = pc.Close()

			// Remove consumer from Hub if no peers remain
			if remaining == 0 {
				sm.mu.Lock()
				source := sm.hub.Get(rtspURL)
				if source != nil {
					source.RemoveConsumer(consumer)
				}
				delete(sm.consumers, rtspURL)
				sm.mu.Unlock()
			}
		}
	})

	if err := pc.SetRemoteDescription(offer); err != nil {
		consumer.removePeer(peer)
		_ = pc.Close()
		return nil, fmt.Errorf("set remote description: %w", err)
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		consumer.removePeer(peer)
		_ = pc.Close()
		return nil, fmt.Errorf("create answer: %w", err)
	}

	if err := pc.SetLocalDescription(answer); err != nil {
		consumer.removePeer(peer)
		_ = pc.Close()
		return nil, fmt.Errorf("set local description: %w", err)
	}

	// Wait for ICE gathering to complete
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	<-gatherComplete

	return pc.LocalDescription(), nil
}

func (sm *StreamManager) getOrCreateConsumer(rtspURL string) *webrtcConsumer {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if c, ok := sm.consumers[rtspURL]; ok {
		return c
	}

	c := &webrtcConsumer{}
	sm.consumers[rtspURL] = c

	// Register with the Hub's source
	source := sm.hub.GetOrCreate(rtspURL)
	source.AddConsumer(c)

	return c
}

// Close shuts down all sessions and peer connections.
func (sm *StreamManager) Close() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for url, consumer := range sm.consumers {
		consumer.mu.Lock()
		for _, peer := range consumer.peers {
			_ = peer.pc.Close()
		}
		consumer.peers = nil
		consumer.mu.Unlock()

		if sm.hub != nil {
			if source := sm.hub.Get(url); source != nil {
				source.RemoveConsumer(consumer)
			}
		}
	}
	sm.consumers = make(map[string]*webrtcConsumer)
}
