package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
func layaDecideRun(ctx context.Context, url, layaURL, questionSet, chromeDebug string) (map[string]any, error) {
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
	// chromedp.Run binds the target to that context -- after the timeout
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
	state["url"] = url

	answers, err := laya.New(layaURL).Decide(ctx, state, questions)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"url":        url,
		"title":      state["title"],
		"text_bytes": len(state["visible_text"]),
		"decisions":  answers,
		"laya":       layaURL,
		"captured":   time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// chromePath resolves the explicit Chromium binary for the exec
// allocator; overridable for tests via HLXN_CHROME_PATH.
func chromePath() string { return uiauto.ResolveChromePath() }

// layaDecideCmd is the operator surface over layaDecideRun.
func layaDecideCmd() *cobra.Command {
	var url, layaURL, questionSet, chromeDebug, timeoutStr string
	cmd := &cobra.Command{
		Use:   "laya-decide",
		Short: "Capture a page, decide next action via the laya typed-decision service",
		RunE: func(cmd *cobra.Command, _ []string) error {
			timeout, err := time.ParseDuration(timeoutStr)
			if err != nil || timeout <= 0 {
				return fmt.Errorf("invalid --timeout %q", timeoutStr)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			evidence, err := layaDecideRun(ctx, url, layaURL, questionSet, chromeDebug)
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
	_ = cmd.MarkFlagRequired("url")
	return cmd
}

var _ io.Writer = io.Discard // keep io import stable for future evidence sinks
