//go:build e2e

package uiauto

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func isE2ETarget(url string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func TestE2E_SelfHealingCascade(t *testing.T) {
	sauceURL := os.Getenv("SAUCE_DEMO_URL")
	if sauceURL == "" {
		sauceURL = "http://localhost:8081"
	}
	if !isE2ETarget(sauceURL) {
		t.Skip("sauce-demo not available at", sauceURL)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	dir := t.TempDir()
	tracker, err := NewPatternTracker(filepath.Join(dir, "store.json"), filepath.Join(dir, "drift"))
	if err != nil {
		t.Fatalf("create tracker: %v", err)
	}

	healer := NewSelfHealer(tracker, nil, nil, nil)

	t.Run("pattern_registration", func(t *testing.T) {
		err := tracker.RegisterPattern(ctx, "login-button", "#login-button", "login button", "<button id='login-button'>Login</button>")
		if err != nil {
			t.Fatalf("register pattern: %v", err)
		}
	})

	t.Run("heal_cascade", func(t *testing.T) {
		result := healer.Heal(ctx, "login-button")
		if result.TargetID != "login-button" {
			t.Errorf("heal result target = %s, want login-button", result.TargetID)
		}
	})
}

func TestE2E_MultiSiteConcurrent(t *testing.T) {
	sites := map[string]string{
		"sauce-demo":       os.Getenv("SAUCE_DEMO_URL"),
		"d2l-mock":         os.Getenv("D2L_URL"),
		"woocommerce-mock": os.Getenv("WOOCOMMERCE_URL"),
	}

	if sites["sauce-demo"] == "" {
		sites["sauce-demo"] = "http://localhost:8081"
	}
	if sites["d2l-mock"] == "" {
		sites["d2l-mock"] = "http://localhost:8082"
	}
	if sites["woocommerce-mock"] == "" {
		sites["woocommerce-mock"] = "http://localhost:8083"
	}

	available := 0
	for _, url := range sites {
		if isE2ETarget(url) {
			available++
		}
	}
	if available < 2 {
		t.Skipf("need at least 2 mock sites, only %d available", available)
	}

	type siteResult struct {
		name    string
		latency time.Duration
		err     error
	}

	results := make(chan siteResult, len(sites))

	for name, url := range sites {
		if !isE2ETarget(url) {
			continue
		}
		go func(n, u string) {
			start := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			dir := t.TempDir()
			tracker, err := NewPatternTracker(filepath.Join(dir, "store.json"), filepath.Join(dir, "drift"))
			if err != nil {
				results <- siteResult{name: n, err: err}
				return
			}

			_ = tracker.RegisterPattern(ctx, n+"-nav", ".nav-link", "navigation link", "<a class='nav-link'>Nav</a>")

			healer := NewSelfHealer(tracker, nil, nil, nil)
			healer.Heal(ctx, n+"-nav")

			results <- siteResult{name: n, latency: time.Since(start)}
		}(name, url)
	}

	for i := 0; i < available; i++ {
		r := <-results
		if r.err != nil {
			t.Errorf("site %s failed: %v", r.name, r.err)
		} else {
			t.Logf("site %s completed in %v", r.name, r.latency)
		}
	}
}

func TestE2E_EnduranceCycle(t *testing.T) {
	if os.Getenv("E2E_ENDURANCE") == "" {
		t.Skip("set E2E_ENDURANCE=1 for 1-hour endurance test")
	}

	sauceURL := os.Getenv("SAUCE_DEMO_URL")
	if sauceURL == "" {
		sauceURL = "http://localhost:8081"
	}
	if !isE2ETarget(sauceURL) {
		t.Skip("sauce-demo not available")
	}

	duration := 1 * time.Hour
	ctx, cancel := context.WithTimeout(context.Background(), duration+5*time.Minute)
	defer cancel()

	dir := t.TempDir()
	tracker, err := NewPatternTracker(filepath.Join(dir, "store.json"), filepath.Join(dir, "drift"))
	if err != nil {
		t.Fatalf("create tracker: %v", err)
	}

	_ = tracker.RegisterPattern(ctx, "endurance-target", "#main-content", "main content", "<div id='main-content'></div>")

	healer := NewSelfHealer(tracker, nil, nil, nil)

	start := time.Now()
	cycles := 0
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			healer.Heal(ctx, "endurance-target")
			cycles++
			if cycles%10 == 0 {
				t.Logf("endurance: %d cycles, elapsed %v", cycles, time.Since(start))
			}
		case <-ctx.Done():
			t.Logf("endurance complete: %d cycles in %v", cycles, time.Since(start))
			return
		}

		if time.Since(start) >= duration {
			t.Logf("endurance complete: %d cycles in %v", cycles, time.Since(start))
			return
		}
	}
}
