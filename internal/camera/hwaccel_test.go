package camera

import (
	"testing"
)

func TestParseHWAccels_MacOS(t *testing.T) {
	output := `Hardware acceleration methods:
videotoolbox
`
	result := parseHWAccels(output)

	if !result["videotoolbox"] {
		t.Error("expected videotoolbox to be available")
	}
	if result["cuda"] {
		t.Error("expected cuda to not be available")
	}
}

func TestParseHWAccels_Linux(t *testing.T) {
	output := `Hardware acceleration methods:
cuda
vaapi
vdpau
`
	result := parseHWAccels(output)

	if !result["cuda"] {
		t.Error("expected cuda to be available")
	}
	if !result["vaapi"] {
		t.Error("expected vaapi to be available")
	}
	if !result["vdpau"] {
		t.Error("expected vdpau to be available")
	}
	if result["videotoolbox"] {
		t.Error("expected videotoolbox to not be available")
	}
}

func TestParseHWAccels_Empty(t *testing.T) {
	output := `Hardware acceleration methods:
`
	result := parseHWAccels(output)

	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

func TestParseHWAccels_NoHeader(t *testing.T) {
	// Malformed output with no header
	output := `some random output
`
	result := parseHWAccels(output)

	if len(result) != 0 {
		t.Errorf("expected empty map for malformed output, got %v", result)
	}
}

func TestHWAccel_FFmpegArgs_Videotoolbox(t *testing.T) {
	hw := &HWAccel{
		Name:       "videotoolbox",
		DecodeArgs: decodeArgsFor("videotoolbox"),
		Available:  true,
	}

	args := hw.FFmpegArgs()
	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d: %v", len(args), args)
	}
	if args[0] != "-hwaccel" || args[1] != "videotoolbox" {
		t.Errorf("unexpected args: %v", args)
	}
}

func TestHWAccel_FFmpegArgs_CUDA(t *testing.T) {
	hw := &HWAccel{
		Name:       "cuda",
		DecodeArgs: decodeArgsFor("cuda"),
		Available:  true,
	}

	args := hw.FFmpegArgs()
	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d: %v", len(args), args)
	}
	if args[0] != "-hwaccel" || args[1] != "cuda" ||
		args[2] != "-hwaccel_output_format" || args[3] != "cuda" {
		t.Errorf("unexpected args: %v", args)
	}
}

func TestHWAccel_FFmpegArgs_VAAPI(t *testing.T) {
	hw := &HWAccel{
		Name:       "vaapi",
		DecodeArgs: decodeArgsFor("vaapi"),
		Available:  true,
	}

	args := hw.FFmpegArgs()
	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d: %v", len(args), args)
	}
	if args[0] != "-hwaccel" || args[1] != "vaapi" ||
		args[2] != "-hwaccel_output_format" || args[3] != "vaapi" {
		t.Errorf("unexpected args: %v", args)
	}
}

func TestHWAccel_FFmpegArgs_Nil(t *testing.T) {
	var hw *HWAccel
	args := hw.FFmpegArgs()

	if args != nil {
		t.Errorf("expected nil args for nil HWAccel, got %v", args)
	}
}

func TestDecodeArgsFor_Unknown(t *testing.T) {
	args := decodeArgsFor("unknown_backend")
	if len(args) != 2 {
		t.Fatalf("expected 2 args for unknown backend, got %d", len(args))
	}
	if args[0] != "-hwaccel" || args[1] != "unknown_backend" {
		t.Errorf("unexpected args: %v", args)
	}
}
