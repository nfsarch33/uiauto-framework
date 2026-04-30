package regression

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/signal"
)

func mockChecker(ctx context.Context, site SiteConfig) (*SiteResult, error) {
	checks := make([]SelectorCheckResult, len(site.Selectors))
	passed := 0
	for i, sel := range site.Selectors {
		found := i%3 != 2 // every 3rd selector "drifts"
		checks[i] = SelectorCheckResult{
			Selector: sel,
			Found:    found,
			Healed:   !found,
			NewSel:   "",
		}
		if found {
			passed++
		}
	}
	return &SiteResult{
		SiteID:       site.ID,
		SiteName:     site.Name,
		TotalChecks:  len(site.Selectors),
		Passed:       passed,
		Failed:       len(site.Selectors) - passed,
		Drifted:      len(site.Selectors) - passed,
		Duration:     10 * time.Millisecond,
		CheckResults: checks,
	}, nil
}

func failingChecker(ctx context.Context, site SiteConfig) (*SiteResult, error) {
	if site.ID == "fail-site" {
		return nil, fmt.Errorf("connection refused")
	}
	return mockChecker(ctx, site)
}

func TestSuiteRunConcurrent(t *testing.T) {
	sites := DefaultSites()
	suite := NewSuite(sites, mockChecker, 3)

	results := suite.Run(context.Background())
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}

	for _, r := range results {
		if r.TotalChecks == 0 {
			t.Errorf("site %s: no checks", r.SiteID)
		}
		if r.Duration == 0 {
			t.Errorf("site %s: zero duration", r.SiteID)
		}
	}
}

func TestSuiteWithSignals(t *testing.T) {
	emitter := signal.NewEmitter()
	handler, getter := signal.CollectorHandler()
	emitter.On(handler)

	sites := DefaultSites()[:2]
	suite := NewSuite(sites, mockChecker, 2, WithEmitter(emitter))

	results := suite.Run(context.Background())
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	signals := getter()
	hasTestSignal := false
	for _, s := range signals {
		if s.Category == signal.CategoryTest {
			hasTestSignal = true
		}
	}
	if !hasTestSignal {
		t.Error("expected test signal from suite")
	}
}

func TestSuitePartialFailure(t *testing.T) {
	sites := []SiteConfig{
		{ID: "ok-site", Name: "OK", BaseURL: "http://ok", Selectors: []string{"#a"}},
		{ID: "fail-site", Name: "Fail", BaseURL: "http://fail", Selectors: []string{"#b"}},
	}
	suite := NewSuite(sites, failingChecker, 2)

	results := suite.Run(context.Background())
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	// One should have real results, one should be partial
	hasReal := false
	for _, r := range results {
		if r.TotalChecks > 0 {
			hasReal = true
		}
	}
	if !hasReal {
		t.Error("expected at least one site with real results")
	}
}

func TestSuiteContextCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	slowChecker := func(ctx context.Context, site SiteConfig) (*SiteResult, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(1 * time.Second):
			return mockChecker(ctx, site)
		}
	}

	sites := DefaultSites()
	suite := NewSuite(sites, slowChecker, 3)
	results := suite.Run(ctx)

	// Should complete (possibly with errors) within timeout
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
}

func TestDefaultSites(t *testing.T) {
	sites := DefaultSites()
	if len(sites) != 3 {
		t.Fatalf("DefaultSites() = %d, want 3", len(sites))
	}
	ids := map[string]bool{}
	for _, s := range sites {
		if s.ID == "" || s.Name == "" || s.BaseURL == "" {
			t.Errorf("incomplete site: %+v", s)
		}
		if len(s.Selectors) == 0 {
			t.Errorf("site %s has no selectors", s.ID)
		}
		if ids[s.ID] {
			t.Errorf("duplicate site ID: %s", s.ID)
		}
		ids[s.ID] = true
	}
}
