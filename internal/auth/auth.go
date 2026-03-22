package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	maxFailures     = 10
	failureWindow   = 5 * time.Minute
	cleanupInterval = time.Minute
)

// Checker validates credentials and tracks failed attempts per IP.
type Checker struct {
	username   string
	passHash   [sha256.Size]byte // SHA-256 of plaintext password (avoids storing cleartext)
	bcryptHash []byte            // non-nil when password is a bcrypt hash
	isBcrypt   bool

	mu       sync.Mutex
	failures map[string]*failureRecord
	done     chan struct{}
}

type failureRecord struct {
	count   int
	firstAt time.Time
}

// New creates a Checker. Returns nil if username or password is empty.
func New(username, password string) *Checker {
	if username == "" || password == "" {
		return nil
	}

	c := &Checker{
		username: username,
		isBcrypt: strings.HasPrefix(password, "$2a$") ||
			strings.HasPrefix(password, "$2b$") ||
			strings.HasPrefix(password, "$2y$"),
		failures: make(map[string]*failureRecord),
		done:     make(chan struct{}),
	}

	if c.isBcrypt {
		c.bcryptHash = []byte(password)
	} else {
		c.passHash = sha256.Sum256([]byte(password))
	}

	go c.cleanupLoop()
	slog.Info("authentication enabled", "username", username, "bcrypt", c.isBcrypt)
	return c
}

// ValidateConfig checks that auth configuration is valid.
// Returns an error describing the problem, or nil if valid.
func ValidateConfig(username, password string) error {
	if (username != "") != (password != "") {
		return fmt.Errorf("auth: both username and password must be set")
	}
	if username == "" {
		return nil
	}
	isBcrypt := strings.HasPrefix(password, "$2a$") ||
		strings.HasPrefix(password, "$2b$") ||
		strings.HasPrefix(password, "$2y$")
	if isBcrypt {
		// Verify the hash is structurally valid by trying a comparison.
		// bcrypt.CompareHashAndPassword returns bcrypt.ErrHashTooShort or
		// similar for malformed hashes, and bcrypt.ErrMismatchedHashAndPassword
		// for valid hashes with wrong input.
		err := bcrypt.CompareHashAndPassword([]byte(password), []byte("probe"))
		if err != nil && err != bcrypt.ErrMismatchedHashAndPassword {
			return fmt.Errorf("auth: invalid bcrypt hash: %w", err)
		}
	}
	return nil
}

// Check validates username and password. Returns true on success.
// Logs and rate-limits failed attempts by remoteIP.
func (c *Checker) Check(user, pass, remoteIP string) bool {
	if c.isRateLimited(remoteIP) {
		slog.Warn("auth rate limited", "ip", remoteIP)
		return false
	}

	ok := c.verify(user, pass)
	if !ok {
		c.recordFailure(remoteIP)
		slog.Warn("auth failed", "ip", remoteIP, "username", user)
		return false
	}

	c.clearFailures(remoteIP)
	return true
}

// Close stops the background cleanup goroutine.
func (c *Checker) Close() {
	close(c.done)
}

// verify performs constant-time credential comparison.
// When bcrypt is configured, always runs bcrypt to prevent timing leaks on username.
func (c *Checker) verify(user, pass string) bool {
	userOK := subtle.ConstantTimeCompare([]byte(user), []byte(c.username)) == 1

	if c.isBcrypt {
		passOK := bcrypt.CompareHashAndPassword(c.bcryptHash, []byte(pass)) == nil
		return userOK && passOK
	}

	inputHash := sha256.Sum256([]byte(pass))
	passOK := subtle.ConstantTimeCompare(inputHash[:], c.passHash[:]) == 1
	return userOK && passOK
}

// IsLoopback returns true if the address is a loopback IP.
func IsLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (c *Checker) isRateLimited(ip string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	rec, ok := c.failures[ip]
	if !ok {
		return false
	}
	if time.Since(rec.firstAt) > failureWindow {
		delete(c.failures, ip)
		return false
	}
	return rec.count >= maxFailures
}

func (c *Checker) recordFailure(ip string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	rec, ok := c.failures[ip]
	if !ok || time.Since(rec.firstAt) > failureWindow {
		c.failures[ip] = &failureRecord{count: 1, firstAt: time.Now()}
		return
	}
	rec.count++
}

func (c *Checker) clearFailures(ip string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.failures, ip)
}

func (c *Checker) cleanupLoop() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			c.mu.Lock()
			now := time.Now()
			for ip, rec := range c.failures {
				if now.Sub(rec.firstAt) > failureWindow {
					delete(c.failures, ip)
				}
			}
			c.mu.Unlock()
		}
	}
}
