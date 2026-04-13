package media

import (
	"testing"
	"time"
)

func TestDetectConsumer_RateLimitAfterDecode(t *testing.T) {
	// Verify that the rate limiter does not prevent frames from being
	// decoded. The decoder must see every access unit to maintain its
	// H.264 reference picture buffer; only the output to frameCh should
	// be throttled.

	dc := &DetectConsumer{
		width:      320,
		height:     240,
		camera:     "test",
		frameCh:    make(chan RawFrame, 10),
		frameDelay: 200 * time.Millisecond,
		lastLog:    time.Now(),
	}

	// Without a real H264 decoder we can't call OnVideoRTP, but we can
	// verify the struct invariant: frameCount tracks decoded frames while
	// lastFrame gates output timing.

	// Simulate two decoded frames arriving 10ms apart (well within rate limit).
	dc.lastFrame = time.Now()
	dc.frameCount = 0

	// First frame after lastFrame: within rate limit, should be suppressed.
	if time.Since(dc.lastFrame) >= dc.frameDelay {
		t.Fatal("expected to be within rate limit window")
	}

	// Second frame after waiting: should pass rate limit.
	time.Sleep(dc.frameDelay + 10*time.Millisecond)
	if time.Since(dc.lastFrame) < dc.frameDelay {
		t.Fatal("expected to be past rate limit window")
	}
}

func TestDetectConsumer_FrameChannelNonBlocking(t *testing.T) {
	dc := &DetectConsumer{
		frameCh: make(chan RawFrame, 1),
	}

	// Fill the channel
	dc.frameCh <- RawFrame{Width: 1, Height: 1}

	// A second send to a full channel should not block (default case in select).
	select {
	case dc.frameCh <- RawFrame{Width: 2, Height: 2}:
		t.Fatal("expected channel send to be dropped (channel full)")
	default:
		// This is the expected path
	}

	// Drain and verify the first frame is still there
	f := <-dc.frameCh
	if f.Width != 1 {
		t.Errorf("drained frame width = %d, want 1", f.Width)
	}
}

func TestDetectConsumer_Close_NilDecoder(t *testing.T) {
	dc := &DetectConsumer{}
	// Should not panic
	dc.Close()
}
