package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rvben/vedetta/internal/artifact"
)

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func withOpenH264InstallTestHooks(t *testing.T, cacheRoot string, spec openH264InstallSpec) {
	t.Helper()

	oldDownload := openH264Download
	oldCacheRoot := openH264CacheRoot
	oldPlatform := openH264Platform
	oldSpecFunc := openH264InstallSpecFunc
	oldExtract := openH264ExtractLibrary
	oldLoadLibrary := openh264LoadLibrary
	oldCloseLibrary := openh264CloseLibrary
	oldCodecVersion := openh264CodecVersion
	oldLibPaths := openh264LibPathsFn

	t.Cleanup(func() {
		openH264Download = oldDownload
		openH264CacheRoot = oldCacheRoot
		openH264Platform = oldPlatform
		openH264InstallSpecFunc = oldSpecFunc
		openH264ExtractLibrary = oldExtract
		openh264LoadLibrary = oldLoadLibrary
		openh264CloseLibrary = oldCloseLibrary
		openh264CodecVersion = oldCodecVersion
		openh264LibPathsFn = oldLibPaths
		openH264InstallRunning.Store(false)
		resetOpenH264State()
	})

	openH264CacheRoot = func() string { return cacheRoot }
	openH264Platform = func() string { return "test/platform" }
	openH264InstallSpecFunc = func(platform string) (openH264InstallSpec, bool) {
		if platform != "test/platform" {
			return openH264InstallSpec{}, false
		}
		return spec, true
	}
	openh264LibPathsFn = func() []string { return nil }
	openh264CloseLibrary = func() error { return nil }
	openh264CodecVersion = func() string { return openh264Version }
	t.Setenv("OPENH264_LIB", "")
	resetOpenH264State()
}

func TestInstallOpenH264WritesVerifiedLibraryAndLoadsIt(t *testing.T) {
	cacheRoot := t.TempDir()
	archiveData := []byte("archive-bytes")
	libraryData := []byte("verified-openh264-library")
	spec := openH264InstallSpec{
		url:         "https://example.test/libopenh264.so.bz2",
		filename:    "libopenh264.so.bz2",
		archiveSHA:  sha256Hex(archiveData),
		librarySHA:  sha256Hex(libraryData),
		libraryName: "libopenh264.so",
	}
	withOpenH264InstallTestHooks(t, cacheRoot, spec)

	var loadedPath string
	openH264Download = func(_ context.Context, got artifact.Spec) ([]byte, error) {
		if got.URL != spec.url {
			t.Fatalf("download URL = %q, want %q", got.URL, spec.url)
		}
		if got.SHA256 != spec.archiveSHA {
			t.Fatalf("archive checksum = %q, want %q", got.SHA256, spec.archiveSHA)
		}
		return archiveData, nil
	}
	openH264ExtractLibrary = func(got []byte) ([]byte, error) {
		if !bytes.Equal(got, archiveData) {
			t.Fatalf("extract input = %q, want %q", string(got), string(archiveData))
		}
		return libraryData, nil
	}
	openh264LoadLibrary = func(path string) error {
		loadedPath = path
		return nil
	}

	status, err := InstallOpenH264(context.Background())
	if err != nil {
		t.Fatalf("InstallOpenH264() error = %v", err)
	}
	if !status.Available {
		t.Fatalf("status.Available = false, want true")
	}
	if !status.Installed {
		t.Fatalf("status.Installed = false, want true")
	}
	if status.Source != "installed" {
		t.Fatalf("status.Source = %q, want %q", status.Source, "installed")
	}

	wantPath := filepath.Join(cacheRoot, spec.libraryName)
	if loadedPath != wantPath {
		t.Fatalf("loaded path = %q, want %q", loadedPath, wantPath)
	}
	gotData, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read installed library: %v", err)
	}
	if !bytes.Equal(gotData, libraryData) {
		t.Fatalf("installed library = %q, want %q", string(gotData), string(libraryData))
	}
}

func TestOpenH264StatusInfoMissingOnSupportedPlatformDoesNotReportError(t *testing.T) {
	cacheRoot := t.TempDir()
	spec := openH264InstallSpec{
		url:         "https://example.test/libopenh264.so.bz2",
		filename:    "libopenh264.so.bz2",
		archiveSHA:  strings.Repeat("0", 64),
		librarySHA:  strings.Repeat("1", 64),
		libraryName: "libopenh264.so",
	}
	withOpenH264InstallTestHooks(t, cacheRoot, spec)

	status := OpenH264StatusInfo()
	if !status.Supported {
		t.Fatalf("status.Supported = false, want true")
	}
	if status.Available {
		t.Fatalf("status.Available = true, want false")
	}
	if status.Installed {
		t.Fatalf("status.Installed = true, want false")
	}
	if status.Error != "" {
		t.Fatalf("status.Error = %q, want empty", status.Error)
	}
}

func TestOpenH264StatusInfoRemovesCorruptInstalledArtifact(t *testing.T) {
	cacheRoot := t.TempDir()
	expectedLibrary := []byte("expected-library")
	spec := openH264InstallSpec{
		url:         "https://example.test/libopenh264.so.bz2",
		filename:    "libopenh264.so.bz2",
		archiveSHA:  strings.Repeat("0", 64),
		librarySHA:  sha256Hex(expectedLibrary),
		libraryName: "libopenh264.so",
	}
	withOpenH264InstallTestHooks(t, cacheRoot, spec)

	corruptPath := filepath.Join(cacheRoot, spec.libraryName)
	if err := os.WriteFile(corruptPath, []byte("corrupt"), 0o644); err != nil {
		t.Fatalf("write corrupt library: %v", err)
	}
	openh264LoadLibrary = func(string) error {
		return errors.New("should not attempt to load corrupt artifact")
	}

	status := OpenH264StatusInfo()
	if status.Installed {
		t.Fatalf("status.Installed = true, want false")
	}
	if !strings.Contains(status.Error, "failed verification") {
		t.Fatalf("status.Error = %q, want verification failure", status.Error)
	}
	if _, err := os.Stat(corruptPath); !os.IsNotExist(err) {
		t.Fatalf("corrupt artifact still exists, stat err = %v", err)
	}
}
