package uiauto

import (
	"testing"
	"time"
)

func TestDefaultDiscoveryConfig(t *testing.T) {
	cfg := DefaultDiscoveryConfig()
	if len(cfg.ElementTypes) == 0 {
		t.Error("ElementTypes should not be empty")
	}
	if cfg.MaxElements != 50 {
		t.Errorf("MaxElements = %d, want 50", cfg.MaxElements)
	}
	if cfg.MinConfidence != 0.5 {
		t.Errorf("MinConfidence = %f, want 0.5", cfg.MinConfidence)
	}
	if cfg.ScanTimeout != 30*time.Second {
		t.Errorf("ScanTimeout = %v, want 30s", cfg.ScanTimeout)
	}
	if cfg.UseVLM {
		t.Error("UseVLM should be false by default")
	}
}

func TestSanitizeID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"button.login", "button_login"},
		{"#main-content", "_main-content"},
		{"a[href='test']", "a_href__test__"},
		{"simple", "simple"},
		{"", ""},
	}

	for _, tt := range tests {
		got := sanitizeID(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSanitizeID_MaxLength(t *testing.T) {
	long := ""
	for i := 0; i < 100; i++ {
		long += "a"
	}
	got := sanitizeID(long)
	if len(got) > 64 {
		t.Errorf("sanitizeID output length = %d, want <= 64", len(got))
	}
}

func TestDiscoveredElement_Fields(t *testing.T) {
	elem := DiscoveredElement{
		ID:          "button_submit",
		Selector:    "button[type=submit]",
		Description: "Submit button",
		ElementType: "button",
		Confidence:  0.85,
	}
	if elem.ID != "button_submit" {
		t.Errorf("ID = %q", elem.ID)
	}
	if elem.Confidence != 0.85 {
		t.Errorf("Confidence = %f", elem.Confidence)
	}
}

func TestDiscoveryResult_Fields(t *testing.T) {
	result := DiscoveryResult{
		URL:             "https://example.com",
		TotalCandidates: 10,
		Registered:      8,
		Duration:        5 * time.Second,
		Errors:          []string{"one error"},
	}
	if result.URL != "https://example.com" {
		t.Errorf("URL = %q", result.URL)
	}
	if result.TotalCandidates != 10 {
		t.Errorf("TotalCandidates = %d", result.TotalCandidates)
	}
	if len(result.Errors) != 1 {
		t.Errorf("Errors len = %d", len(result.Errors))
	}
}

func TestNewDiscoveryMode(t *testing.T) {
	cfg := DefaultDiscoveryConfig()
	dm := NewDiscoveryMode(nil, nil, nil, cfg, nil)
	if dm == nil {
		t.Fatal("NewDiscoveryMode returned nil")
	}
	if dm.vlm != nil {
		t.Error("vlm should be nil initially")
	}
	if dm.config.MaxElements != 50 {
		t.Errorf("config.MaxElements = %d", dm.config.MaxElements)
	}
}
