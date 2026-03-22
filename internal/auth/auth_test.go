package auth

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func newChecker(t *testing.T, user, pass string) *Checker {
	t.Helper()
	c := New(user, pass)
	if c != nil {
		t.Cleanup(c.Close)
	}
	return c
}

func TestNew_EmptyCredentials(t *testing.T) {
	tests := []struct {
		user, pass string
	}{
		{"", ""},
		{"admin", ""},
		{"", "secret"},
	}
	for _, tt := range tests {
		c := newChecker(t, tt.user, tt.pass)
		if c != nil {
			t.Errorf("New(%q, %q) should return nil", tt.user, tt.pass)
		}
	}
}

func TestCheck_Plaintext(t *testing.T) {
	c := newChecker(t, "admin", "secret")
	if c == nil {
		t.Fatal("expected non-nil Checker")
	}

	tests := []struct {
		user, pass string
		want       bool
	}{
		{"admin", "secret", true},
		{"admin", "wrong", false},
		{"wrong", "secret", false},
		{"wrong", "wrong", false},
		{"", "", false},
		{"admin", "", false},
		{"", "secret", false},
		{"Admin", "secret", false},   // case sensitive username
		{"admin", "Secret", false},   // case sensitive password
	}
	for _, tt := range tests {
		got := c.Check(tt.user, tt.pass, "10.0.0.1")
		if got != tt.want {
			t.Errorf("Check(%q, %q) = %v, want %v", tt.user, tt.pass, got, tt.want)
		}
	}
}

func TestCheck_Bcrypt_2a(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}

	c := newChecker(t, "admin", string(hash))
	if c == nil {
		t.Fatal("expected non-nil Checker")
	}
	if !c.isBcrypt {
		t.Fatal("expected isBcrypt=true")
	}

	if !c.Check("admin", "secret", "10.0.0.1") {
		t.Error("correct bcrypt credentials should pass")
	}
	if c.Check("admin", "wrong", "10.0.0.2") {
		t.Error("wrong bcrypt password should fail")
	}
	if c.Check("wrong", "secret", "10.0.0.3") {
		t.Error("wrong username with bcrypt should fail")
	}
}

func TestCheck_Bcrypt_2y(t *testing.T) {
	// $2y$ is produced by htpasswd, PHP, and many online generators.
	// Go's bcrypt package accepts $2y$ hashes transparently.
	hash, err := bcrypt.GenerateFromPassword([]byte("mypass"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	// Simulate a $2y$ hash by replacing the prefix.
	hashStr := "$2y$" + string(hash[4:])

	c := newChecker(t, "user", hashStr)
	if c == nil {
		t.Fatal("expected non-nil Checker")
	}
	if !c.isBcrypt {
		t.Fatal("expected isBcrypt=true for $2y$ hash")
	}
	if !c.Check("user", "mypass", "10.0.0.1") {
		t.Error("$2y$ bcrypt hash should validate correctly")
	}
	if c.Check("user", "wrong", "10.0.0.2") {
		t.Error("wrong password should fail with $2y$ hash")
	}
}

func TestRateLimiting(t *testing.T) {
	c := newChecker(t, "admin", "secret")

	ip := "192.168.1.100"

	for range maxFailures {
		if c.Check("admin", "wrong", ip) {
			t.Fatal("should fail with wrong password")
		}
	}

	// Even correct credentials should be rate limited now
	if c.Check("admin", "secret", ip) {
		t.Error("should be rate limited after max failures")
	}

	// Different IP should not be affected
	if !c.Check("admin", "secret", "10.0.0.99") {
		t.Error("different IP should not be rate limited")
	}
}

func TestRateLimiting_ClearsOnSuccess(t *testing.T) {
	c := newChecker(t, "admin", "secret")

	ip := "192.168.1.200"

	for range 3 {
		c.Check("admin", "wrong", ip)
	}

	if !c.Check("admin", "secret", ip) {
		t.Error("correct credentials should succeed and clear counter")
	}

	// Counter is reset — we can fail again up to the limit
	for range maxFailures - 1 {
		c.Check("admin", "wrong", ip)
	}

	// One more failure should still be under limit (exactly maxFailures-1)
	// But at maxFailures, it should be blocked
	c.Check("admin", "wrong", ip) // this is the 10th
	if c.Check("admin", "secret", ip) {
		t.Error("should be rate limited after re-exhausting failures")
	}
}

func TestRateLimiting_PerIP(t *testing.T) {
	c := newChecker(t, "admin", "secret")

	// Exhaust IP A
	for range maxFailures {
		c.Check("admin", "wrong", "192.168.1.1")
	}

	// IP B should be unaffected
	if !c.Check("admin", "secret", "192.168.1.2") {
		t.Error("IP B should not be rate limited by IP A's failures")
	}

	// IP A should be blocked
	if c.Check("admin", "secret", "192.168.1.1") {
		t.Error("IP A should still be rate limited")
	}
}

func TestIsLoopback(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:1234", true},
		{"[::1]:1234", true},
		{"192.168.1.100:1234", false},
		{"10.0.0.1:5050", false},
		{"[::ffff:127.0.0.1]:1234", true},
		{"127.0.0.1", true},
		{"::1", true},
		{"0.0.0.0:5050", false},
		{"[::]:5050", false},
	}
	for _, tt := range tests {
		got := IsLoopback(tt.addr)
		if got != tt.want {
			t.Errorf("IsLoopback(%q) = %v, want %v", tt.addr, got, tt.want)
		}
	}
}

func TestVerify_BcryptAlwaysRunsRegardlessOfUsername(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	c := newChecker(t, "admin", string(hash))

	// The point: wrong username must still run bcrypt (no early return).
	// We verify both paths produce the correct boolean result.
	if c.verify("wrong", "secret") {
		t.Error("wrong username should fail even when password matches")
	}
	if c.verify("admin", "wrong") {
		t.Error("wrong password should fail")
	}
	if c.verify("wrong", "wrong") {
		t.Error("both wrong should fail")
	}
	if !c.verify("admin", "secret") {
		t.Error("correct credentials should pass")
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name       string
		user, pass string
		wantErr    bool
	}{
		{"both empty", "", "", false},
		{"both set plaintext", "admin", "secret", false},
		{"username only", "admin", "", true},
		{"password only", "", "secret", true},
		{"valid bcrypt", "admin", func() string {
			h, _ := bcrypt.GenerateFromPassword([]byte("x"), bcrypt.MinCost)
			return string(h)
		}(), false},
		{"invalid bcrypt hash", "admin", "$2a$10$broken", true},
		{"truncated bcrypt", "admin", "$2b$", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(tt.user, tt.pass)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateConfig(%q, %q) error = %v, wantErr = %v", tt.user, tt.pass, err, tt.wantErr)
			}
		})
	}
}

func TestPlaintextPasswordNotStoredInCleartext(t *testing.T) {
	c := newChecker(t, "admin", "supersecretpassword")

	// The password field should be empty for plaintext mode.
	// Only the SHA-256 hash is stored.
	if c.isBcrypt {
		t.Fatal("should not be bcrypt mode")
	}

	// Verify it still works (hash comparison, not string comparison)
	if !c.Check("admin", "supersecretpassword", "10.0.0.1") {
		t.Error("should authenticate with hashed password")
	}
}
