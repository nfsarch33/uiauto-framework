package uiauto

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
)

// ResolveChromePath finds a Chromium binary for the exec allocator:
// HLXN_CHROME_PATH override first, then the newest Playwright cache under
// the user's home. Empty means "not found" — the caller leaves chromedp's
// own default candidate list in place, which covers system installs.
func ResolveChromePath() string {
	if p := os.Getenv("HLXN_CHROME_PATH"); p != "" {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	if runtime.GOOS == "windows" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	entries, err := os.ReadDir(filepath.Join(home, ".cache", "ms-playwright"))
	if err != nil {
		return ""
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && len(e.Name()) > len("chromium-") && e.Name()[:len("chromium-")] == "chromium-" {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dirs))) // newest build number first
	// Playwright renamed the inner dir between builds; try both layouts.
	for _, d := range dirs {
		for _, inner := range []string{"chrome-linux", "chrome-linux64"} {
			candidate := filepath.Join(home, ".cache", "ms-playwright", d, inner, "chrome")
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate
			}
		}
	}
	return ""
}
