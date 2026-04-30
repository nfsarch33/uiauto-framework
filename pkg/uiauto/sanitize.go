package uiauto

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// MaxSelectorLength caps CSS selector inputs.
const MaxSelectorLength = 4096

// MaxDescriptionLength caps free-text description inputs.
const MaxDescriptionLength = 2048

// MaxURLLength caps URL inputs.
const MaxURLLength = 8192

// MaxHTMLLength caps HTML inputs for fingerprinting and analysis.
const MaxHTMLLength = 10 * 1024 * 1024 // 10 MB

// SanitizeSelector validates and truncates a CSS selector input.
func SanitizeSelector(s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("empty selector")
	}
	if !utf8.ValidString(s) {
		return "", fmt.Errorf("invalid UTF-8 in selector")
	}
	if len(s) > MaxSelectorLength {
		return "", fmt.Errorf("selector exceeds %d bytes", MaxSelectorLength)
	}
	if containsDangerousChars(s) {
		return "", fmt.Errorf("selector contains potentially dangerous characters")
	}
	return strings.TrimSpace(s), nil
}

// SanitizeDescription validates and truncates a description string.
func SanitizeDescription(s string) string {
	if !utf8.ValidString(s) {
		return ""
	}
	s = strings.TrimSpace(s)
	if len(s) > MaxDescriptionLength {
		s = s[:MaxDescriptionLength]
	}
	return s
}

// SanitizeURL performs basic URL validation.
func SanitizeURL(u string) (string, error) {
	if u == "" {
		return "", fmt.Errorf("empty URL")
	}
	if len(u) > MaxURLLength {
		return "", fmt.Errorf("URL exceeds %d bytes", MaxURLLength)
	}
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return "", fmt.Errorf("URL must start with http:// or https://")
	}
	return u, nil
}

// SanitizeHTML validates HTML input size.
func SanitizeHTML(h string) (string, error) {
	if len(h) > MaxHTMLLength {
		return "", fmt.Errorf("HTML exceeds %d bytes", MaxHTMLLength)
	}
	return h, nil
}

func containsDangerousChars(s string) bool {
	for _, pattern := range []string{
		"javascript:", "data:", "vbscript:",
		"<script", "</script", "onerror=", "onload=",
	} {
		if strings.Contains(strings.ToLower(s), pattern) {
			return true
		}
	}
	return false
}
