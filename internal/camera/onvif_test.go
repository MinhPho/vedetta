package camera

import (
	"net"
	"testing"
)

const sampleProbeResponse = `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery"
            xmlns:a="http://schemas.xmlsoap.org/ws/2004/08/addressing">
  <s:Header>
    <a:Action>http://schemas.xmlsoap.org/ws/2005/04/discovery/ProbeMatches</a:Action>
  </s:Header>
  <s:Body>
    <d:ProbeMatches>
      <d:ProbeMatch>
        <d:XAddrs>http://192.168.1.100:80/onvif/device_service</d:XAddrs>
        <d:Scopes>onvif://www.onvif.org/name/FrontDoor onvif://www.onvif.org/manufacturer/Tapo onvif://www.onvif.org/model/C320WS onvif://www.onvif.org/hardware/C320WS</d:Scopes>
      </d:ProbeMatch>
    </d:ProbeMatches>
  </s:Body>
</s:Envelope>`

const sampleProbeResponseMinimal = `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery"
            xmlns:a="http://schemas.xmlsoap.org/ws/2004/08/addressing">
  <s:Header>
    <a:Action>http://schemas.xmlsoap.org/ws/2005/04/discovery/ProbeMatches</a:Action>
  </s:Header>
  <s:Body>
    <d:ProbeMatches>
      <d:ProbeMatch>
        <d:XAddrs>http://10.0.0.50:8080/onvif/device_service</d:XAddrs>
        <d:Scopes></d:Scopes>
      </d:ProbeMatch>
    </d:ProbeMatches>
  </s:Body>
</s:Envelope>`

const sampleProbeResponseMultipleXAddrs = `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery"
            xmlns:a="http://schemas.xmlsoap.org/ws/2004/08/addressing">
  <s:Header>
    <a:Action>http://schemas.xmlsoap.org/ws/2005/04/discovery/ProbeMatches</a:Action>
  </s:Header>
  <s:Body>
    <d:ProbeMatches>
      <d:ProbeMatch>
        <d:XAddrs>http://192.168.1.200:80/onvif/device_service http://192.168.1.200:8080/onvif/device_service</d:XAddrs>
        <d:Scopes>onvif://www.onvif.org/name/Backyard%20Camera onvif://www.onvif.org/manufacturer/Reolink onvif://www.onvif.org/model/RLC-810A</d:Scopes>
      </d:ProbeMatch>
    </d:ProbeMatches>
  </s:Body>
</s:Envelope>`

const sampleProbeResponseNoMatches = `<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery">
  <s:Body>
    <d:ProbeMatches>
    </d:ProbeMatches>
  </s:Body>
</s:Envelope>`

func TestParseProbeResponse(t *testing.T) {
	remoteAddr := &net.UDPAddr{IP: net.ParseIP("192.168.1.100"), Port: 3702}

	cam, err := parseProbeResponse([]byte(sampleProbeResponse), remoteAddr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cam.IP != "192.168.1.100" {
		t.Errorf("expected IP 192.168.1.100, got %s", cam.IP)
	}
	if cam.Port != 554 {
		t.Errorf("expected port 554, got %d", cam.Port)
	}
	if cam.Name != "FrontDoor" {
		t.Errorf("expected name FrontDoor, got %s", cam.Name)
	}
	if cam.Manufacturer != "Tapo" {
		t.Errorf("expected manufacturer Tapo, got %s", cam.Manufacturer)
	}
	if cam.Model != "C320WS" {
		t.Errorf("expected model C320WS, got %s", cam.Model)
	}
	if len(cam.XAddrs) != 1 {
		t.Errorf("expected 1 XAddr, got %d", len(cam.XAddrs))
	}
	if cam.XAddrs[0] != "http://192.168.1.100:80/onvif/device_service" {
		t.Errorf("unexpected XAddr: %s", cam.XAddrs[0])
	}
}

func TestParseProbeResponseMinimal(t *testing.T) {
	remoteAddr := &net.UDPAddr{IP: net.ParseIP("10.0.0.50"), Port: 3702}

	cam, err := parseProbeResponse([]byte(sampleProbeResponseMinimal), remoteAddr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cam.IP != "10.0.0.50" {
		t.Errorf("expected IP 10.0.0.50, got %s", cam.IP)
	}
	// Should default to camera-<ip> when no name scope is present
	if cam.Name != "camera-10.0.0.50" {
		t.Errorf("expected default name, got %s", cam.Name)
	}
	if cam.Manufacturer != "" {
		t.Errorf("expected empty manufacturer, got %s", cam.Manufacturer)
	}
}

func TestParseProbeResponseMultipleXAddrs(t *testing.T) {
	remoteAddr := &net.UDPAddr{IP: net.ParseIP("192.168.1.200"), Port: 3702}

	cam, err := parseProbeResponse([]byte(sampleProbeResponseMultipleXAddrs), remoteAddr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cam.XAddrs) != 2 {
		t.Fatalf("expected 2 XAddrs, got %d", len(cam.XAddrs))
	}
	if cam.Name != "Backyard Camera" {
		t.Errorf("expected URL-decoded name 'Backyard Camera', got %s", cam.Name)
	}
	if cam.Manufacturer != "Reolink" {
		t.Errorf("expected manufacturer Reolink, got %s", cam.Manufacturer)
	}
	if cam.Model != "RLC-810A" {
		t.Errorf("expected model RLC-810A, got %s", cam.Model)
	}
}

func TestParseProbeResponseNoMatches(t *testing.T) {
	remoteAddr := &net.UDPAddr{IP: net.ParseIP("192.168.1.1"), Port: 3702}

	_, err := parseProbeResponse([]byte(sampleProbeResponseNoMatches), remoteAddr)
	if err == nil {
		t.Fatal("expected error for response with no matches")
	}
}

func TestParseProbeResponseInvalidXML(t *testing.T) {
	remoteAddr := &net.UDPAddr{IP: net.ParseIP("192.168.1.1"), Port: 3702}

	_, err := parseProbeResponse([]byte("not xml at all"), remoteAddr)
	if err == nil {
		t.Fatal("expected error for invalid XML")
	}
}

func TestGenerateConfig(t *testing.T) {
	cameras := []DiscoveredCamera{
		{
			IP:           "192.168.1.100",
			Port:         554,
			Name:         "Front Door",
			Manufacturer: "Tapo",
			Model:        "C320WS",
		},
		{
			IP:   "192.168.1.101",
			Port: 554,
			Name: "Backyard",
		},
	}

	config := GenerateConfig(cameras)

	if config == "" {
		t.Fatal("expected non-empty config")
	}

	// Check that it contains expected camera entries
	if !containsStr(config, "name: front_door") {
		t.Error("expected config to contain sanitized camera name 'front_door'")
	}
	if !containsStr(config, "name: backyard") {
		t.Error("expected config to contain camera name 'backyard'")
	}
	if !containsStr(config, "192.168.1.100") {
		t.Error("expected config to contain first camera IP")
	}
	if !containsStr(config, "192.168.1.101") {
		t.Error("expected config to contain second camera IP")
	}
	if !containsStr(config, "Tapo") {
		t.Error("expected config to contain manufacturer comment")
	}
	if !containsStr(config, "cameras:") {
		t.Error("expected config to start with cameras key")
	}
}

func TestGenerateConfigEmpty(t *testing.T) {
	config := GenerateConfig(nil)
	if config != "# No cameras discovered\n" {
		t.Errorf("expected empty config comment, got: %s", config)
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Front Door", "front_door"},
		{"backyard-cam", "backyard_cam"},
		{"Camera #1", "camera_1"},
		{"UPPERCASE", "uppercase"},
		{"already_clean", "already_clean"},
		{"", "camera"},
		{"!!!@@@", "camera"},
		{"cam 123", "cam_123"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeName(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestExtractScopeValue(t *testing.T) {
	tests := []struct {
		scope    string
		key      string
		expected string
	}{
		{"onvif://www.onvif.org/name/FrontDoor", "name/", "FrontDoor"},
		{"onvif://www.onvif.org/manufacturer/Tapo", "manufacturer/", "Tapo"},
		{"onvif://www.onvif.org/name/My%20Camera", "name/", "My Camera"},
		{"onvif://www.onvif.org/model/", "model/", ""},
		{"unrelated-scope", "name/", ""},
	}

	for _, tt := range tests {
		t.Run(tt.scope, func(t *testing.T) {
			got := extractScopeValue(tt.scope, tt.key)
			if got != tt.expected {
				t.Errorf("extractScopeValue(%q, %q) = %q, want %q", tt.scope, tt.key, got, tt.expected)
			}
		})
	}
}

func TestRTSPPatterns(t *testing.T) {
	// Verify that known brands have expected patterns
	brands := []string{"tapo", "tp-link", "reolink", "hikvision", "dahua", "amcrest"}
	for _, brand := range brands {
		patterns, ok := rtspPatterns[brand]
		if !ok {
			t.Errorf("missing RTSP patterns for brand %s", brand)
			continue
		}
		if len(patterns) < 2 {
			t.Errorf("brand %s should have at least main and sub stream patterns, got %d", brand, len(patterns))
		}

		hasMain := false
		hasSub := false
		for _, p := range patterns {
			if p.Resolution == "main" {
				hasMain = true
			}
			if p.Resolution == "sub" {
				hasSub = true
			}
		}
		if !hasMain {
			t.Errorf("brand %s missing main stream pattern", brand)
		}
		if !hasSub {
			t.Errorf("brand %s missing sub stream pattern", brand)
		}
	}

	// Generic should have patterns from multiple brands
	generic := rtspPatterns["generic"]
	if len(generic) < 4 {
		t.Errorf("generic patterns should cover multiple brands, got %d patterns", len(generic))
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
