package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/browseruse"
)

// browserUseRun drives one natural-language task through the browser-use
// executor lane (containers/browser-use) and prints the run's evidence:
// the final result plus the history the eval harness grades on. The task
// executes against an existing Chromium over CDP — the lane never spawns
// a browser.
func browserUseRun(ctx context.Context, svcURL string, req browseruse.RunRequest) (map[string]any, error) {
	client := browseruse.New(svcURL)

	// Version probe is advisory: stamp drift and a missing default CDP
	// endpoint are recorded, never fatal to the run by themselves.
	var health map[string]any
	if h, err := client.Health(ctx); err == nil {
		health = map[string]any{"browser_use": h.BrowserUse, "cdp_configured": h.CDPConfigured}
	}

	res, err := client.Run(ctx, req)
	if err != nil {
		return nil, err
	}
	ev := map[string]any{
		"task":         req.Task,
		"url":          req.URL,
		"ok":           res.OK,
		"final_result": res.FinalResult,
		"steps":        res.Steps,
		"duration_s":   res.DurationSec,
		"errors":       res.Errors,
		"urls_visited": res.URLsVisited,
		"browser_use":  res.BrowserUse,
		"service":      svcURL,
		"captured":     time.Now().UTC().Format(time.RFC3339),
	}
	if health != nil {
		ev["service_health"] = health
	}
	return ev, nil
}

func browserUseRunCmd() *cobra.Command {
	var svcURL, task, pageURL, cdpURL, model, llmBaseURL, timeoutStr string
	var maxSteps int
	cmd := &cobra.Command{
		Use:   "browser-use-run",
		Short: "Run an NL task through the browser-use executor lane (attaches to an existing browser over CDP)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			timeout, err := time.ParseDuration(timeoutStr)
			if err != nil || timeout <= 0 {
				return fmt.Errorf("invalid --timeout %q", timeoutStr)
			}
			if task == "" {
				return fmt.Errorf("--task is required")
			}
			if maxSteps < 1 || maxSteps > 200 {
				return fmt.Errorf("invalid --max-steps %d: must be within [1,200]", maxSteps)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			evidence, err := browserUseRun(ctx, svcURL, browseruse.RunRequest{
				Task:     task,
				URL:      pageURL,
				MaxSteps: maxSteps,
				CDPURL:   cdpURL,
				Model:    model,
				BaseURL:  llmBaseURL,
			})
			if err != nil {
				return err
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(evidence)
		},
	}
	cmd.Flags().StringVar(&task, "task", "", "natural-language task to execute (required)")
	cmd.Flags().StringVar(&pageURL, "url", "", "starting page URL for the task")
	cmd.Flags().StringVar(&svcURL, "browser-use", "http://127.0.0.1:8091", "browser-use executor service base URL")
	cmd.Flags().StringVar(&cdpURL, "cdp-url", "", "CDP endpoint of the browser to attach (overrides the service default)")
	cmd.Flags().StringVar(&model, "model", "", "LLM model for this run (overrides the service default)")
	cmd.Flags().StringVar(&llmBaseURL, "llm-base-url", "", "OpenAI-compatible LLM base URL for this run (overrides the service default)")
	cmd.Flags().IntVar(&maxSteps, "max-steps", 25, "maximum agent steps before the task is cut off")
	cmd.Flags().StringVar(&timeoutStr, "timeout", "5m", "overall run timeout")
	_ = cmd.MarkFlagRequired("task")
	return cmd
}
