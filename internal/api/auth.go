package api

import (
	"net"
	"net/http"

	"github.com/rvben/vedetta/internal/auth"
)

// authMiddleware wraps an http.Handler with HTTP Basic Auth.
// Localhost connections are exempt. Returns next unmodified if checker is nil.
func authMiddleware(checker *auth.Checker, next http.Handler) http.Handler {
	if checker == nil {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth.IsLoopback(r.RemoteAddr) {
			next.ServeHTTP(w, r)
			return
		}

		user, pass, ok := r.BasicAuth()
		if !ok || !checker.Check(user, pass, remoteIP(r)) {
			w.Header().Set("WWW-Authenticate", `Basic realm="vedetta"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// remoteIP extracts the IP from r.RemoteAddr, stripping the port.
func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
