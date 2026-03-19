package camera

import (
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
)

// HWAccel represents a hardware video acceleration backend.
type HWAccel struct {
	Name       string
	DecodeArgs []string
	Available  bool
}

// DetectHWAccel probes ffmpeg for available hardware acceleration backends
// and returns the best one, or nil for CPU-only decoding.
func DetectHWAccel() *HWAccel {
	output, err := exec.Command("ffmpeg", "-hwaccels").CombinedOutput()
	if err != nil {
		slog.Warn("failed to query ffmpeg hwaccels", "error", err)
		return nil
	}

	available := parseHWAccels(string(output))

	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{"videotoolbox"}
	case "linux":
		candidates = []string{"cuda", "vaapi"}
	default:
		return nil
	}

	for _, name := range candidates {
		if !available[name] {
			continue
		}

		hw := &HWAccel{
			Name:       name,
			DecodeArgs: decodeArgsFor(name),
			Available:  true,
		}

		if testHWAccel(hw) {
			return hw
		}

		slog.Warn("hwaccel listed but failed validation", "name", name)
	}

	return nil
}

// FFmpegArgs returns the ffmpeg arguments to prepend before -i for hardware decoding.
// Returns nil if the receiver is nil (CPU fallback).
func (h *HWAccel) FFmpegArgs() []string {
	if h == nil {
		return nil
	}
	return h.DecodeArgs
}

// parseHWAccels parses the output of `ffmpeg -hwaccels` into a set of available backend names.
func parseHWAccels(output string) map[string]bool {
	result := make(map[string]bool)
	lines := strings.Split(output, "\n")

	// The output format is:
	//   Hardware acceleration methods:
	//   videotoolbox
	//   ...
	pastHeader := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Hardware acceleration") {
			pastHeader = true
			continue
		}
		if pastHeader {
			result[line] = true
		}
	}
	return result
}

// decodeArgsFor returns the ffmpeg decode arguments for a given hwaccel backend.
func decodeArgsFor(name string) []string {
	switch name {
	case "videotoolbox":
		return []string{"-hwaccel", "videotoolbox"}
	case "cuda":
		return []string{"-hwaccel", "cuda", "-hwaccel_output_format", "cuda"}
	case "vaapi":
		return []string{"-hwaccel", "vaapi", "-hwaccel_output_format", "vaapi"}
	default:
		return []string{"-hwaccel", name}
	}
}

// testHWAccel validates that a hwaccel backend works by running a short ffmpeg decode.
func testHWAccel(hw *HWAccel) bool {
	// Generate a tiny test input with lavfi and decode using the hwaccel
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
	}
	args = append(args, hw.DecodeArgs...)
	args = append(args,
		"-f", "lavfi",
		"-i", "nullsrc=s=64x64:d=0.1",
		"-frames:v", "1",
		"-f", "null",
		"-",
	)

	cmd := exec.Command("ffmpeg", args...)
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}
