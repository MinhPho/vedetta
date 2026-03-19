package stream

import (
	"fmt"
	"os/exec"
)

// StartRTPStream starts an ffmpeg process that reads from an RTSP URL
// and outputs H.264 RTP packets to the given local UDP port.
func StartRTPStream(rtspURL string, rtpPort int) (*exec.Cmd, error) {
	cmd := exec.Command("ffmpeg",
		"-hide_banner",
		"-loglevel", "error",
		"-rtsp_transport", "tcp",
		"-i", rtspURL,
		"-c:v", "copy",
		"-an",
		"-f", "rtp",
		fmt.Sprintf("rtp://127.0.0.1:%d", rtpPort),
	)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ffmpeg RTP: %w", err)
	}

	return cmd, nil
}
