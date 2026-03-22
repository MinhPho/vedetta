package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rvben/vedetta/internal/auth"
)

var okHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func TestAuthMiddleware_Disabled(t *testing.T) {
	handler := authMiddleware(nil, okHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("disabled auth: status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestAuthMiddleware_LocalhostExempt(t *testing.T) {
	checker := auth.New("admin", "secret")
	handler := authMiddleware(checker, okHandler)

	for _, addr := range []string{"127.0.0.1:54321", "[::1]:54321"} {
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		req.RemoteAddr = addr
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("localhost exempt (%s): status = %d, want %d", addr, w.Code, http.StatusOK)
		}
	}
}

func TestAuthMiddleware_NoCredentials(t *testing.T) {
	checker := auth.New("admin", "secret")
	handler := authMiddleware(checker, okHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.RemoteAddr = "10.0.0.5:54321"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("no credentials: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if w.Header().Get("WWW-Authenticate") == "" {
		t.Error("missing WWW-Authenticate header")
	}
}

func TestAuthMiddleware_WrongCredentials(t *testing.T) {
	checker := auth.New("admin", "secret")
	handler := authMiddleware(checker, okHandler)

	tests := []struct {
		name, user, pass string
	}{
		{"wrong password", "admin", "wrong"},
		{"wrong username", "user", "secret"},
		{"both wrong", "user", "wrong"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
			req.RemoteAddr = "10.0.0.5:54321"
			req.SetBasicAuth(tt.user, tt.pass)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestAuthMiddleware_CorrectCredentials(t *testing.T) {
	checker := auth.New("admin", "secret")
	handler := authMiddleware(checker, okHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.RemoteAddr = "10.0.0.5:54321"
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("correct credentials: status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestAuthMiddleware_RateLimited(t *testing.T) {
	checker := auth.New("admin", "secret")
	handler := authMiddleware(checker, okHandler)

	// Exhaust rate limit from a single IP
	for range 10 {
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		req.RemoteAddr = "10.0.0.99:54321"
		req.SetBasicAuth("admin", "wrong")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	// Even correct credentials should be blocked
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.RemoteAddr = "10.0.0.99:54321"
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("rate limited: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}
