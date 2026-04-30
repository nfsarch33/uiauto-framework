package regression

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/parallel"
	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/signal"
)

// SiteConfig defines a target site for regression testing.
type SiteConfig struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	// Selectors are the expected CSS selectors to validate on this site.
	Selectors []string `json:"selectors"`
}

// SiteResult captures the regression test outcome for one site.
type SiteResult struct {
	SiteID       string                `json:"site_id"`
	SiteName     string                `json:"site_name"`
	TotalChecks  int                   `json:"total_checks"`
	Passed       int                   `json:"passed"`
	Failed       int                   `json:"failed"`
	Drifted      int                   `json:"drifted"`
	Duration     time.Duration         `json:"duration"`
	CheckResults []SelectorCheckResult `json:"check_results"`
}

// SelectorCheckResult captures one selector's check outcome.
type SelectorCheckResult struct {
	Selector string `json:"selector"`
	Found    bool   `json:"found"`
	Healed   bool   `json:"healed"`
	NewSel   string `json:"new_selector,omitempty"`
}

// SiteChecker is the function signature for checking a single site.
// Implementations use chromedp or mock logic.
type SiteChecker func(ctx context.Context, site SiteConfig) (*SiteResult, error)

// Suite orchestrates multi-site regression testing with parallel execution.
type Suite struct {
	sites   []SiteConfig
	checker SiteChecker
	pool    *parallel.Pool
	emitter *signal.Emitter
	logger  *slog.Logger
}

// SuiteOption configures the suite.
type SuiteOption func(*Suite)

// WithEmitter attaches a signal emitter to the suite.
func WithEmitter(e *signal.Emitter) SuiteOption {
	return func(s *Suite) { s.emitter = e }
}

// WithSuiteLogger sets a structured logger.
func WithSuiteLogger(l *slog.Logger) SuiteOption {
	return func(s *Suite) { s.logger = l }
}

// NewSuite creates a multi-site regression suite.
func NewSuite(sites []SiteConfig, checker SiteChecker, workers int, opts ...SuiteOption) *Suite {
	s := &Suite{
		sites:   sites,
		checker: checker,
		pool:    parallel.NewPool(parallel.WithWorkers(workers)),
		logger:  slog.Default(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Run executes the regression suite across all sites concurrently.
func (s *Suite) Run(ctx context.Context) []SiteResult {
	tasks := make([]parallel.Task, len(s.sites))
	var mu sync.Mutex
	resultMap := make(map[string]*SiteResult)

	for i, site := range s.sites {
		siteCopy := site
		tasks[i] = parallel.Task{
			ID:   siteCopy.ID,
			Name: fmt.Sprintf("regression-%s", siteCopy.Name),
			Fn: func(ctx context.Context) error {
				result, err := s.checker(ctx, siteCopy)
				if err != nil {
					return err
				}
				mu.Lock()
				resultMap[siteCopy.ID] = result
				mu.Unlock()
				return nil
			},
		}
	}

	poolResults := s.pool.Run(ctx, tasks)

	var results []SiteResult
	for _, pr := range poolResults {
		if sr, ok := resultMap[pr.TaskID]; ok {
			results = append(results, *sr)
		} else {
			results = append(results, SiteResult{
				SiteID:   pr.TaskID,
				SiteName: pr.Name,
				Duration: pr.Duration,
			})
		}
	}

	// Emit summary signal
	if s.emitter != nil {
		totalPassed, totalFailed := 0, 0
		var totalDur time.Duration
		for _, r := range results {
			totalPassed += r.Passed
			totalFailed += r.Failed
			totalDur += r.Duration
		}
		signal.EmitTestResult(s.emitter, signal.TestEvent{
			Suite:    "multi-site-regression",
			Passed:   totalPassed,
			Failed:   totalFailed,
			Duration: totalDur,
		})
	}

	return results
}

// DefaultSites returns the three standard regression targets.
func DefaultSites() []SiteConfig {
	return []SiteConfig{
		{
			ID: "sauce-demo", Name: "Sauce Demo",
			BaseURL:   "https://www.saucedemo.com",
			Selectors: []string{"#user-name", "#password", "#login-button", ".inventory_list"},
		},
		{
			ID: "d2l-lms", Name: "D2L Brightspace LMS",
			BaseURL:   "https://d2l.deakin.edu.au",
			Selectors: []string{"#userName", "#password", ".d2l-login-button", ".d2l-navigation"},
		},
		{
			ID: "woocommerce", Name: "WooCommerce Store",
			BaseURL:   "https://store.example.com",
			Selectors: []string{".woocommerce-products-header", ".product", ".add_to_cart_button", ".cart-contents"},
		},
	}
}
