package rtsp

import (
	"context"
	"testing"
)

func TestSanitizeURL_RedactsCredentialsAndSecrets(t *testing.T) {
	raw := "rtsp://user:pass@example.com/live?token=abc123&profile=main#frag"
	got := SanitizeURL(raw)
	want := "rtsp://example.com/live?profile=main&token=REDACTED"
	if got != want {
		t.Fatalf("SanitizeURL() = %q, want %q", got, want)
	}
}

func TestSanitizeURL_Invalid(t *testing.T) {
	if got := SanitizeURL("://bad"); got != "rtsp://***@<invalid>" {
		t.Fatalf("SanitizeURL() = %q, want invalid placeholder", got)
	}
}

func TestHubGetOrCreate_ReturnsSameSource(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := NewHub(ctx)
	defer hub.Close()

	url := "rtsp://test:554/stream1"
	s1 := hub.GetOrCreate(url)
	s2 := hub.GetOrCreate(url)

	if s1 != s2 {
		t.Fatal("GetOrCreate returned different sources for same URL")
	}
}

func TestHubGetOrCreate_DifferentURLs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := NewHub(ctx)
	defer hub.Close()

	s1 := hub.GetOrCreate("rtsp://test:554/stream1")
	s2 := hub.GetOrCreate("rtsp://test:554/stream2")

	if s1 == s2 {
		t.Fatal("GetOrCreate returned same source for different URLs")
	}
}

func TestHubGet_ReturnsNilForUnknown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := NewHub(ctx)
	defer hub.Close()

	if s := hub.Get("rtsp://nonexistent:554/stream"); s != nil {
		t.Fatal("Get returned non-nil for unknown URL")
	}
}

func TestHubGet_ReturnsExisting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := NewHub(ctx)
	defer hub.Close()

	url := "rtsp://test:554/stream1"
	created := hub.GetOrCreate(url)
	got := hub.Get(url)

	if got != created {
		t.Fatal("Get didn't return the source created by GetOrCreate")
	}
}

func TestHubClose_ClearsAllSources(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := NewHub(ctx)
	hub.GetOrCreate("rtsp://test:554/stream1")
	hub.GetOrCreate("rtsp://test:554/stream2")

	hub.Close()

	if s := hub.Get("rtsp://test:554/stream1"); s != nil {
		t.Fatal("source still present after Close")
	}
}
