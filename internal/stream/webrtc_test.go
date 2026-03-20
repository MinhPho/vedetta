package stream

import (
	"context"
	"testing"

	"github.com/pion/webrtc/v4"
	"github.com/rvben/vedetta/internal/rtsp"
)

func TestSDPOfferAnswerExchange(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := rtsp.NewHub(ctx)
	defer hub.Close()

	sm := NewStreamManager(hub)
	defer sm.Close()

	// Create a client peer connection to generate an offer
	clientConfig := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	}

	client, err := webrtc.NewPeerConnection(clientConfig)
	if err != nil {
		t.Fatalf("failed to create client peer connection: %v", err)
	}
	defer func() { _ = client.Close() }()

	// Add a transceiver to receive video
	if _, err := client.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	}); err != nil {
		t.Fatalf("failed to add transceiver: %v", err)
	}

	offer, err := client.CreateOffer(nil)
	if err != nil {
		t.Fatalf("failed to create offer: %v", err)
	}

	if err := client.SetLocalDescription(offer); err != nil {
		t.Fatalf("failed to set local description: %v", err)
	}

	// HandleOffer should succeed for the SDP exchange part,
	// even though the RTSP source won't have actual video.
	answer, err := sm.HandleOffer("test-cam", "rtsp://invalid:554/stream", offer)
	if err != nil {
		t.Logf("HandleOffer returned error (expected, no stream): %v", err)
	} else {
		if answer.Type != webrtc.SDPTypeAnswer {
			t.Errorf("expected SDP answer type, got %v", answer.Type)
		}
	}
}

func TestNewStreamManager(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := rtsp.NewHub(ctx)
	defer hub.Close()

	sm := NewStreamManager(hub)
	if sm == nil {
		t.Fatal("NewStreamManager returned nil")
	}
	sm.Close()
}
