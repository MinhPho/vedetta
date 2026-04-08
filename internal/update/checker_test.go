package update

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"v0.2.0", "v0.3.0", -1},
		{"v0.3.0", "v0.2.0", 1},
		{"v0.3.0", "v0.3.0", 0},
		{"v1.0.0", "v0.99.99", 1},
		{"v0.1.0", "v0.1.1", -1},
		{"0.2.0", "0.3.0", -1},
		{"v1.2.3", "v1.2.3", 0},
	}
	for _, tt := range tests {
		got := compareSemver(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("compareSemver(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestIsNewer(t *testing.T) {
	tests := []struct {
		current, latest string
		want            bool
	}{
		{"v0.2.0", "v0.3.0", true},
		{"v0.3.0", "v0.3.0", false},
		{"v0.4.0", "v0.3.0", false},
		{"dev", "v0.1.0", true},
		{"dev", "v999.0.0", true},
	}
	for _, tt := range tests {
		got := isNewer(tt.current, tt.latest)
		if got != tt.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
		}
	}
}

func TestChecker_FetchAndStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name": "v0.3.0", "html_url": "https://github.com/rvben/vedetta/releases/tag/v0.3.0"}`))
	}))
	defer server.Close()

	origURL := githubLatestURL
	githubLatestURL = server.URL
	defer func() { githubLatestURL = origURL }()

	db := &mockSettingsStore{data: make(map[string]string)}
	checker := New("v0.2.0", time.Hour, db)

	status := checker.CheckNow()
	if !status.UpdateAvailable {
		t.Fatal("expected update available")
	}
	if status.Latest != "v0.3.0" {
		t.Fatalf("expected v0.3.0, got %s", status.Latest)
	}
	if status.Dismissed {
		t.Fatal("should not be dismissed")
	}

	if err := checker.Dismiss(); err != nil {
		t.Fatalf("dismiss error: %v", err)
	}
	status = checker.Status()
	if !status.Dismissed {
		t.Fatal("should be dismissed after Dismiss()")
	}
}

func TestChecker_DismissClearsOnNewerVersion(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if callCount <= 1 {
			w.Write([]byte(`{"tag_name": "v0.3.0", "html_url": "https://github.com/rvben/vedetta/releases/tag/v0.3.0"}`))
		} else {
			w.Write([]byte(`{"tag_name": "v0.4.0", "html_url": "https://github.com/rvben/vedetta/releases/tag/v0.4.0"}`))
		}
	}))
	defer server.Close()

	origURL := githubLatestURL
	githubLatestURL = server.URL
	defer func() { githubLatestURL = origURL }()

	db := &mockSettingsStore{data: make(map[string]string)}
	checker := New("v0.2.0", time.Hour, db)

	checker.CheckNow()
	checker.Dismiss()
	status := checker.Status()
	if !status.Dismissed {
		t.Fatal("should be dismissed")
	}

	checker.CheckNow()
	status = checker.Status()
	if status.Dismissed {
		t.Fatal("should not be dismissed for newer version")
	}
	if status.Latest != "v0.4.0" {
		t.Fatalf("expected v0.4.0, got %s", status.Latest)
	}
}

type mockSettingsStore struct {
	data map[string]string
}

func (m *mockSettingsStore) GetSetting(key string) (string, error) {
	return m.data[key], nil
}

func (m *mockSettingsStore) SetSetting(key, value string) error {
	m.data[key] = value
	return nil
}

func (m *mockSettingsStore) DeleteSetting(key string) error {
	delete(m.data, key)
	return nil
}
