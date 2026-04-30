package uiauto

import (
	"strings"
	"testing"
)

func TestSanitizeSelector_Valid(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"button.login", "button.login"},
		{"  #main  ", "#main"},
		{"[data-testid='submit']", "[data-testid='submit']"},
		{"div > p:nth-child(2)", "div > p:nth-child(2)"},
	}
	for _, tt := range tests {
		got, err := SanitizeSelector(tt.input)
		if err != nil {
			t.Errorf("SanitizeSelector(%q) error: %v", tt.input, err)
		}
		if got != tt.want {
			t.Errorf("SanitizeSelector(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSanitizeSelector_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"too_long", strings.Repeat("a", MaxSelectorLength+1)},
		{"javascript", "javascript:alert(1)"},
		{"script_tag", "<script>alert(1)</script>"},
		{"onerror", "img[onerror='alert(1)']"},
	}
	for _, tt := range tests {
		_, err := SanitizeSelector(tt.input)
		if err == nil {
			t.Errorf("SanitizeSelector(%q) should have returned error", tt.name)
		}
	}
}

func TestSanitizeDescription(t *testing.T) {
	if got := SanitizeDescription("  hello world  "); got != "hello world" {
		t.Errorf("got %q", got)
	}
	long := strings.Repeat("x", MaxDescriptionLength+100)
	if got := SanitizeDescription(long); len(got) > MaxDescriptionLength {
		t.Errorf("length %d > %d", len(got), MaxDescriptionLength)
	}
	if got := SanitizeDescription(""); got != "" {
		t.Errorf("empty should stay empty, got %q", got)
	}
}

func TestSanitizeURL(t *testing.T) {
	valid := []string{
		"https://example.com",
		"http://localhost:8090/api/v1/status",
		"https://d2l.deakin.edu.au/d2l/le/content/12345",
	}
	for _, u := range valid {
		got, err := SanitizeURL(u)
		if err != nil {
			t.Errorf("SanitizeURL(%q) error: %v", u, err)
		}
		if got != u {
			t.Errorf("SanitizeURL(%q) = %q", u, got)
		}
	}

	invalid := []string{
		"",
		"ftp://files.example.com",
		"javascript:void(0)",
		strings.Repeat("https://a", MaxURLLength),
	}
	for _, u := range invalid {
		_, err := SanitizeURL(u)
		if err == nil {
			t.Errorf("SanitizeURL(%q) should return error", u[:minInt(len(u), 50)])
		}
	}
}

func TestSanitizeHTML(t *testing.T) {
	small := "<div>hello</div>"
	got, err := SanitizeHTML(small)
	if err != nil || got != small {
		t.Errorf("SanitizeHTML small: err=%v got=%q", err, got)
	}

	big := strings.Repeat("x", MaxHTMLLength+1)
	_, err = SanitizeHTML(big)
	if err == nil {
		t.Error("SanitizeHTML should reject oversized HTML")
	}
}

func TestContainsDangerousChars(t *testing.T) {
	dangerous := []string{
		"javascript:alert(1)",
		"<script>evil</script>",
		"img onerror=hack",
		"JAVASCRIPT:void(0)",
		"data:text/html,<h1>hi</h1>",
		"vbscript:run",
	}
	for _, s := range dangerous {
		if !containsDangerousChars(s) {
			t.Errorf("containsDangerousChars(%q) = false, want true", s)
		}
	}

	safe := []string{
		"button.login",
		"#main-content",
		"div > p.active",
		"[aria-label='Submit']",
	}
	for _, s := range safe {
		if containsDangerousChars(s) {
			t.Errorf("containsDangerousChars(%q) = true, want false", s)
		}
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
