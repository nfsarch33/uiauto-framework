package uiauto

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestResolveChromePathOrder pins browser discovery: explicit override
// wins, otherwise the Playwright cache is used when its binary exists,
// otherwise empty (chromedp's own default search applies).
func TestResolveChromePathOrder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix path test")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "chrome")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("override wins", func(t *testing.T) {
		t.Setenv("HLXN_CHROME_PATH", fake)
		if got := ResolveChromePath(); got != fake {
			t.Fatalf("got %q, want override", got)
		}
	})

	t.Run("playwright cache hit", func(t *testing.T) {
		t.Setenv("HLXN_CHROME_PATH", "")
		home := t.TempDir()
		cache := filepath.Join(home, ".cache", "ms-playwright", "chromium-999", "chrome-linux", "chrome")
		if err := os.MkdirAll(filepath.Dir(cache), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cache, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HOME", home)
		if got := ResolveChromePath(); got != cache {
			t.Fatalf("got %q, want playwright cache path", got)
		}
	})

	t.Run("nothing found yields empty", func(t *testing.T) {
		t.Setenv("HLXN_CHROME_PATH", "")
		t.Setenv("HOME", t.TempDir())
		if got := ResolveChromePath(); got != "" {
			t.Fatalf("got %q, want empty", got)
		}
	})
}
