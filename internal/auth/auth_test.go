package auth

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rvben/vedetta/internal/config"
	"github.com/rvben/vedetta/internal/storage"
	"golang.org/x/crypto/bcrypt"
)

func newChecker(t *testing.T, apiCfg config.APIConfig) *Checker {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}

	db, err := storage.New(":memory:")
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	c := New(config.AuthConfig{
		Users: []config.AuthUser{{
			Username:     "admin",
			PasswordHash: string(hash),
		}},
	}, apiCfg, db)
	t.Cleanup(c.Close)
	return c
}

func TestValidateConfigRejectsMalformedHash(t *testing.T) {
	err := ValidateConfig(config.AuthConfig{
		Users: []config.AuthUser{{
			Username:     "admin",
			PasswordHash: "not-a-bcrypt-hash",
		}},
	})
	if err == nil {
		t.Fatal("expected malformed hash validation error")
	}
}

func TestCheckRateLimitIsPerIP(t *testing.T) {
	c := newChecker(t, config.APIConfig{Exposure: "lan"})

	for range maxFailures {
		if c.Check("admin", "wrong", "10.0.0.1") {
			t.Fatal("wrong password should fail")
		}
	}
	if c.Check("admin", "secret", "10.0.0.1") {
		t.Fatal("same IP should be rate limited")
	}
	if !c.Check("admin", "secret", "10.0.0.2") {
		t.Fatal("different IP should not be rate limited")
	}
}

func TestSessionAuthenticationAndCSRF(t *testing.T) {
	c := newChecker(t, config.APIConfig{Exposure: "lan"})

	session, err := c.Login("admin", "secret", "10.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	rr := httptest.NewRecorder()
	c.SetSessionCookies(rr, httptest.NewRequest(http.MethodPost, "http://vedetta.local/api/auth/login", nil), session)

	req := httptest.NewRequest(http.MethodPost, "/api/cameras", nil)
	for _, cookie := range rr.Result().Cookies() {
		req.AddCookie(cookie)
	}

	principal, err := c.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if principal == nil || principal.Kind != AuthKindSession {
		t.Fatalf("expected session principal, got %+v", principal)
	}

	if c.RequireCSRF(req, principal) {
		t.Fatal("POST without X-CSRF-Token should fail")
	}
	req.Header.Set("X-CSRF-Token", session.CSRFToken)
	if !c.RequireCSRF(req, principal) {
		t.Fatal("POST with matching CSRF token should pass")
	}
}

func TestSetSessionCookies_LANHTTPDoesNotForceSecure(t *testing.T) {
	c := newChecker(t, config.APIConfig{Exposure: "lan"})

	session, err := c.Login("admin", "secret", "10.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "http://vedetta.local/api/auth/login", nil)
	rr := httptest.NewRecorder()
	c.SetSessionCookies(rr, req, session)

	for _, cookie := range rr.Result().Cookies() {
		if cookie.Secure {
			t.Fatalf("cookie %q unexpectedly marked Secure on plain HTTP LAN request", cookie.Name)
		}
	}
}

func TestSetSessionCookies_SecureTransportUsesSecureCookies(t *testing.T) {
	c := newChecker(t, config.APIConfig{Exposure: "lan"})

	session, err := c.Login("admin", "secret", "10.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "https://vedetta.local/api/auth/login", nil)
	req.TLS = &tls.ConnectionState{}
	rr := httptest.NewRecorder()
	c.SetSessionCookies(rr, req, session)

	for _, cookie := range rr.Result().Cookies() {
		if !cookie.Secure {
			t.Fatalf("cookie %q should be Secure on HTTPS requests", cookie.Name)
		}
	}
}

func TestBearerTokenAuthentication(t *testing.T) {
	c := newChecker(t, config.APIConfig{Exposure: "lan"})

	token, rawToken, err := c.CreateToken("admin", "integration", []string{"api:read"}, "10.0.0.1")
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	if token.ID == 0 {
		t.Fatal("expected token ID")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)

	principal, err := c.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if principal == nil || principal.Kind != AuthKindToken {
		t.Fatalf("expected token principal, got %+v", principal)
	}
	if !principal.HasAnyScope("api:read") {
		t.Fatal("expected api:read scope")
	}
	if principal.Allows(http.MethodDelete, "/api/events") {
		t.Fatal("read-only token should not allow DELETE")
	}
}

func TestRequestIsSecureWithTrustedProxy(t *testing.T) {
	c := newChecker(t, config.APIConfig{
		Exposure:       "internet",
		TrustedProxies: []string{"127.0.0.1/32"},
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-Proto", "https")
	if !c.RequestIsSecure(req) {
		t.Fatal("trusted proxy with X-Forwarded-Proto=https should be secure")
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.9:1234"
	req.Header.Set("X-Forwarded-Proto", "https")
	if c.RequestIsSecure(req) {
		t.Fatal("untrusted proxy should not be treated as secure")
	}
}
