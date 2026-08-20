package uiauto

import (
	"os/exec"
	"testing"
)

// requireChrome skips browser-driven tests on hosts without a Chrome-family
// binary. chromedp defers launching the browser until the first action, so
// the existing "could not start browser" skip guards never fired — tests
// hard-failed at Navigate with exec: "google-chrome": not found on any
// machine without Chrome (CI runners, headless WSL).
func requireChrome(t *testing.T) {
	t.Helper()
	for _, bin := range []string{
		"google-chrome", "google-chrome-stable", "chromium",
		"chromium-browser", "chrome", "headless-shell",
	} {
		if _, err := exec.LookPath(bin); err == nil {
			return
		}
	}
	t.Skip("no Chrome-family binary on PATH; skipping browser-driven test")
}
