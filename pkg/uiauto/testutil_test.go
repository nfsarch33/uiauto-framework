package uiauto

import (
	"os"
	"testing"
)

func skipWithoutBrowser(t *testing.T) {
	t.Helper()
	if os.Getenv("CI") != "" {
		t.Skip("skipping browser test in CI")
	}
	if testing.Short() {
		t.Skip("skipping browser test in short mode")
	}
}
