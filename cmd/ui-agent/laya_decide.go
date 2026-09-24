package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/nfsarch33/uiauto-framework/pkg/uiauto"
	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/laya"
)

// layaDecideRun is the decision-driven UI loop: the browser lane (chromedp,
// or the shared CDP session when chromeDebug is set) captures the page, the
// laya decision service returns typed decisions (action choice + state
// assertion), and the evidence map carries everything a reviewer or the
// runs-export chain needs. Separated from the cobra wiring so the loop is
// testable against a fixture page.
func layaDecideRun(ctx context.Context, url, layaURL, questionSet, chromeDebug string, minConfidence float64) (map[string]any, error) {
	questions, err := laya.QuestionSet(questionSet)
	if err != nil {
		return nil, err
	}

	var agent *uiauto.BrowserAgent
	if chromeDebug != "" {
		agent, err = uiauto.NewBrowserAgentWithRemote(chromeDebug)
	} else {
		// Explicit path: hosts without a system Chrome resolve the
		// Playwright cache; hosts with one pass "" and keep the default
		// allocator behaviour.
		agent, err = uiauto.NewBrowserAgentWithChromePath(chromePath(), true)
	}
	if err != nil {
		return nil, fmt.Errorf("attach browser: %w", err)
	}
	defer agent.Close()

	// Plain Navigate plus a settle retry. NavigateWithConfig's waiter
	// wraps the session context in its own timeout, and the first
	// chromedp.Run binds the target to that context--after the timeout
	// every later Run returns context canceled (observed live against the
	// board console and the fixture page). The one-shot re-capture keeps
	// SPA shells honest without paying that cost.
	if err := agent.Navigate(url); err != nil {
		return nil, fmt.Errorf("navigate %s: %w", url, err)
	}
	dom, err := agent.CaptureDOM()
	if err != nil {
		return nil, fmt.Errorf("capture: %w", err)
	}
	state := laya.StateFromHTML(dom)
	if state["visible_text"] == "" { // SPA shell: give the render one beat
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(3 * time.Second):
		}
		dom, err = agent.CaptureDOM()
		if err != nil {
			return nil, fmt.Errorf("re-capture: %w", err)
		}
		state = laya.StateFromHTML(dom)
	}
	// The client's own rule -- an empty answer set is an error, not a
	// verdict -- applies symmetrically to the input: deciding on a page
	// with no visible text is worse than failing, so refuse.
	if state["visible_text"] == "" {
		return nil, fmt.Errorf("page state is empty after re-capture (SPA shell or blank page); refusing to decide on no evidence")
	}
	state["url"] = url

	client := laya.New(layaURL)
	answers, err := client.Decide(ctx, state, questions)
	if err != nil {
		return nil, err
	}
	// The version probe is advisory: stamp drift between the running
	// container and the pinned build is recorded, never fatal to a run.
	version := ""
	if h, herr := client.Health(ctx); herr == nil && h.Laya != "" {
		version = h.Laya
	}
	return decideEvidence(url, layaURL, state, answers, version, minConfidence), nil
}

// decideEvidence shapes the run's evidence map: decisions plus the
// confidence-floor flags the probation ledger grades on, the service
// version for stamp-drift checks, and the capture metadata. Pure (aside
// from the timestamp) so the wire shape is unit-testable without a
// browser.
func decideEvidence(url, layaURL string, state laya.State, answers map[string]laya.Answer, layaVersion string, minConfidence float64) map[string]any {
	below := make(map[string]bool, len(answers))
	for name, a := range answers {
		below[name] = a.BelowFloor(minConfidence)
	}
	ev := map[string]any{
		"url":                    url,
		"title":                  state["title"],
		"text_bytes":             len(state["visible_text"]),
		"decisions":              answers,
		"laya":                   layaURL,
		"min_confidence":         minConfidence,
		"confidence_below_floor": below,
		"captured":               time.Now().UTC().Format(time.RFC3339),
	}
	if layaVersion != "" {
		ev["laya_version"] = layaVersion
	}
	return ev
}

// chromePath resolves the explicit Chromium binary for the exec
// allocator; overridable for tests via HLXN_CHROME_PATH.
func chromePath() string { return uiauto.ResolveChromePath() }

// layaDecideCmd is the operator surface over layaDecideRun.
func layaDecideCmd() *cobra.Command {
	var url, layaURL, questionSet, chromeDebug, timeoutStr string
	var minConfidence float64
	cmd := &cobra.Command{
		Use:   "laya-decide",
		Short: "Capture a page, decide next action via the laya typed-decision service",
		RunE: func(cmd *cobra.Command, _ []string) error {
			timeout, err := time.ParseDuration(timeoutStr)
			if err != nil || timeout <= 0 {
				return fmt.Errorf("invalid --timeout %q", timeoutStr)
			}
			if minConfidence < 0 || minConfidence > 1 {
				return fmt.Errorf("invalid --min-confidence %f: must be within [0,1]", minConfidence)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			evidence, err := layaDecideRun(ctx, url, layaURL, questionSet, chromeDebug, minConfidence)
			if err != nil {
				return err
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(evidence)
		},
	}
	cmd.Flags().StringVar(&url, "url", "", "page to capture and decide on (required)")
	cmd.Flags().StringVar(&layaURL, "laya", "http://127.0.0.1:8090", "laya decision service base URL")
	cmd.Flags().StringVar(&questionSet, "question-set", "checkout_recovery", "decision schema: checkout_recovery | board_health")
	cmd.Flags().StringVar(&chromeDebug, "chrome-debug", "", "attach the shared Chrome CDP session at this debug URL instead of spawning headless")
	cmd.Flags().StringVar(&timeoutStr, "timeout", "90s", "overall run timeout")
	cmd.Flags().Float64Var(&minConfidence, "min-confidence", 0.5, "record decisions under this confidence as confidence_below_floor in evidence (recorded, not enforced, during probation)")
	_ = cmd.MarkFlagRequired("url")
	return cmd
}
