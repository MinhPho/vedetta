package stream

import (
	"net/http/httptest"
	"testing"
)

func TestOriginAllowed_SameHost(t *testing.T) {
	req := httptest.NewRequest("GET", "http://vedetta.local/api/cameras/front/mse/ws", nil)
	req.Host = "vedetta.local"
	req.Header.Set("Origin", "http://vedetta.local")

	if !originAllowed(req, nil) {
		t.Fatal("expected same-host origin to be allowed")
	}
}

func TestOriginAllowed_ExplicitAllowlist(t *testing.T) {
	req := httptest.NewRequest("GET", "http://127.0.0.1/api/cameras/front/mse/ws", nil)
	req.Host = "127.0.0.1"
	req.Header.Set("Origin", "https://app.example.com")

	if !originAllowed(req, []string{"https://app.example.com"}) {
		t.Fatal("expected allowlisted origin to be allowed")
	}
}

func TestOriginAllowed_RejectsMismatchedOrigin(t *testing.T) {
	req := httptest.NewRequest("GET", "http://vedetta.local/api/cameras/front/mse/ws", nil)
	req.Host = "vedetta.local"
	req.Header.Set("Origin", "https://evil.example.com")

	if originAllowed(req, nil) {
		t.Fatal("expected mismatched origin to be rejected")
	}
}
