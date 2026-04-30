package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/nfsarch33/uiauto-framework/pkg/uiauto"
	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/omniparser"
)

// NLScenario matches the JSON scenario format documented in
// docs/scenario-format.md.
type NLScenario struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	NaturalLanguage []string `json:"natural_language"`
	PageObjects     []string `json:"page_objects"`
	SelectorsUsed   []string `json:"selectors_used"`
	Source          string   `json:"source"`
	Tags            []string `json:"tags"`
	// ActionTypes is parallel to NaturalLanguage. Defaults to "click" when
	// missing or shorter than NaturalLanguage. Recognised values:
	// click | type | evaluate | read | wait | verify | frame.
	ActionTypes []string `json:"action_types,omitempty"`
	// ActionValues is parallel to NaturalLanguage. Provides the text to type
	// (when action_type=="type") or the selector to wait for (action_type=="wait")
	// and similar per-step value carriers. Empty by default.
	ActionValues []string `json:"action_values,omitempty"`
}

// DemoStepResult captures the outcome of one NL step for structured output.
type DemoStepResult struct {
	StepIndex    int           `json:"step_index"`
	Instruction  string        `json:"instruction"`
	ActionType   string        `json:"action_type,omitempty"`
	Status       string        `json:"status"`
	Selector     string        `json:"selector,omitempty"`
	Tier         string        `json:"tier"`
	HealPath     string        `json:"heal_path,omitempty"`
	Elements     int           `json:"elements_detected"`
	Latency      time.Duration `json:"latency_ns"`
	ScreenshotAt string        `json:"screenshot_path,omitempty"`
	Error        string        `json:"error,omitempty"`
}

// DemoMetricsSummary aggregates per-step metrics into a single payload that
// downstream tooling (HTML report generator, evolver feed) can consume.
type DemoMetricsSummary struct {
	ScenarioID      string           `json:"scenario_id"`
	ScenarioName    string           `json:"scenario_name"`
	TotalSteps      int              `json:"total_steps"`
	PassedSteps     int              `json:"passed_steps"`
	FailedSteps     int              `json:"failed_steps"`
	TotalLatencyMs  int64            `json:"total_latency_ms"`
	AvgLatencyMs    int64            `json:"avg_latency_ms"`
	TierBreakdown   map[string]int   `json:"tier_breakdown"`
	HealPathSummary map[string]int   `json:"heal_path_summary,omitempty"`
	Steps           []DemoStepResult `json:"steps"`
	StartedAt       string           `json:"started_at"`
	FinishedAt      string           `json:"finished_at"`
	Source          string           `json:"source,omitempty"`
}

// EvolutionTraceEntry feeds the EvoLoop-DRL self-improvement cycle.
type EvolutionTraceEntry struct {
	ID          string                 `json:"id"`
	ScenarioID  string                 `json:"scenario_id"`
	TaskName    string                 `json:"task_name"`
	StartTime   string                 `json:"start_time"`
	EndTime     string                 `json:"end_time"`
	Success     bool                   `json:"success"`
	LatencyMs   int64                  `json:"latency_ms"`
	Tier        string                 `json:"tier"`
	ToolsCalled []string               `json:"tools_called"`
	Metadata    map[string]interface{} `json:"metadata"`
}

func demoCmd() *cobra.Command {
	var (
		url            string
		scenarioFile   string
		scenarioID     string
		headless       bool
		remoteDebugURL string
		omniparserURL  string
		ollamaURL      string
		smartModel     string
		gatewayURL     string
		gatewayModel   string
		patternFile    string
		stepDelay      time.Duration
		stepTimeout    time.Duration
		outputDir      string
		visual         bool
		metricsJSON    bool
		vlmModel       string
	)

	cmd := &cobra.Command{
		Use:   "demo",
		Short: "Run an NL test scenario step-by-step with visual output for demonstrations",
		Long: `Run an NL test scenario from a JSON file. Each step is executed with
a pause between steps for narration during live demos.

By default, headless is FALSE so the browser is visible. Use --remote-debug-url
to attach to an existing Chrome session (best for SSO/DUO-authenticated pages).

Annotated screenshots with OmniParser bounding boxes are saved to --output-dir.
Use --visual to open each annotated screenshot in the system image viewer for
five seconds after each step (great for live demos).

When --omniparser-url is provided, OmniParser availability is treated as a
hard prerequisite -- the demo aborts if the server is unreachable.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			llmURL := ollamaURL
			llmModel := smartModel
			if gatewayURL != "" {
				llmURL = gatewayURL
				llmModel = gatewayModel
			} else if envURL := os.Getenv("OPENAI_BASE_URL"); envURL != "" {
				llmURL = envURL
				llmModel = gatewayModel
			}
			return runDemo(demoConfig{
				URL:            url,
				ScenarioFile:   scenarioFile,
				ScenarioID:     scenarioID,
				Headless:       headless,
				RemoteDebugURL: remoteDebugURL,
				OmniParserURL:  omniparserURL,
				OllamaURL:      llmURL,
				SmartModel:     llmModel,
				GatewayURL:     gatewayURL,
				GatewayModel:   gatewayModel,
				PatternFile:    patternFile,
				StepDelay:      stepDelay,
				StepTimeout:    stepTimeout,
				OutputDir:      outputDir,
				Visual:         visual,
				MetricsJSON:    metricsJSON,
				VLMModel:       vlmModel,
				HardFailOmni:   omniparserURL != "",
			})
		},
	}

	cmd.Flags().StringVar(&url, "url", "", "Page URL to navigate to (required if Chrome is on about:blank)")
	cmd.Flags().StringVar(&scenarioFile, "scenario", "", "Path to NL scenario JSON file")
	cmd.Flags().StringVar(&scenarioID, "scenario-id", "", "Specific scenario ID to run (default: first scenario)")
	cmd.Flags().BoolVar(&headless, "headless", false, "Run browser in headless mode (default: false for demos)")
	cmd.Flags().StringVar(&remoteDebugURL, "remote-debug-url", "", "Attach to an existing Chrome DevTools session")
	cmd.Flags().StringVar(&omniparserURL, "omniparser-url", "", "OmniParser V2 server URL (e.g. http://localhost:8090); when set, demo hard-fails if unreachable")
	cmd.Flags().StringVar(&ollamaURL, "ollama-url", "http://127.0.0.1:11434/v1", "LLM API base URL (local; overridden by --gateway-url)")
	cmd.Flags().StringVar(&smartModel, "smart-model", "kwangsuklee/Qwen3.5-27B-Claude-4.6-Opus-Reasoning-Distilled-GGUF", "Model for smart discovery (overridden by --gateway-model)")
	cmd.Flags().StringVar(&gatewayURL, "gateway-url", "", "AI Gateway URL (OpenAI-compatible; overrides --ollama-url)")
	cmd.Flags().StringVar(&gatewayModel, "gateway-model", "gpt-4.1-mini", "Model name for the AI Gateway (e.g. gpt-4.1-mini, gpt-4.1)")
	cmd.Flags().StringVar(&patternFile, "pattern-file", "/tmp/uiauto_demo_patterns.json", "Pattern store file")
	cmd.Flags().DurationVar(&stepDelay, "step-delay", 2*time.Second, "Pause between NL steps for narration")
	cmd.Flags().DurationVar(&stepTimeout, "step-timeout", 90*time.Second, "Per-step timeout for agent.RunTask")
	cmd.Flags().BoolVar(&visual, "visual", false, "Open each annotated screenshot in the OS image viewer for ~5s")
	cmd.Flags().BoolVar(&metricsJSON, "metrics-json", false, "Emit demo-metrics.json + evolution-trace.json next to demo-results.json")
	cmd.Flags().StringVar(&vlmModel, "vlm-model", "", "VLM model name for visual verification tier (e.g. gpt-4.1)")
	home, _ := os.UserHomeDir()
	defaultOut := filepath.Join(home, "uiauto", "tests")
	cmd.Flags().StringVar(&outputDir, "output-dir", defaultOut, "Base directory for test output (scenario subdir auto-created)")

	return cmd
}

type demoConfig struct {
	URL            string
	ScenarioFile   string
	ScenarioID     string
	Headless       bool
	RemoteDebugURL string
	OmniParserURL  string
	OllamaURL      string
	SmartModel     string
	GatewayURL     string
	GatewayModel   string
	PatternFile    string
	StepDelay      time.Duration
	StepTimeout    time.Duration
	OutputDir      string
	Visual         bool
	MetricsJSON    bool
	VLMModel       string
	HardFailOmni   bool
}

func runDemo(cfg demoConfig) error {
	scenarios, err := loadScenarios(cfg.ScenarioFile)
	if err != nil {
		return fmt.Errorf("load scenarios: %w", err)
	}

	scenario := scenarios[0]
	if cfg.ScenarioID != "" {
		found := false
		for _, s := range scenarios {
			if s.ID == cfg.ScenarioID {
				scenario = s
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("scenario %q not found in %s", cfg.ScenarioID, cfg.ScenarioFile)
		}
	}

	ts := time.Now().Format("2006-01-02T15-04-05")
	startedAt := time.Now().UTC().Format(time.RFC3339)
	runDir := filepath.Join(cfg.OutputDir, fmt.Sprintf("%s_%s", scenario.ID, ts))
	screenshotsDir := filepath.Join(runDir, "screenshots")
	resultsDir := filepath.Join(runDir, "results")

	for _, d := range []string{screenshotsDir, resultsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("create dir %s: %w", d, err)
		}
	}

	fmt.Printf("\n=== UIAuto Demo: %s ===\n", scenario.Name)
	fmt.Printf("ID: %s | Steps: %d | Source: %s\n", scenario.ID, len(scenario.NaturalLanguage), scenario.Source)
	fmt.Printf("Output: %s\n\n", runDir)

	// OmniParser hard-fail: when URL is provided we MUST be able to reach it.
	var omniClient *omniparser.Client
	if cfg.OmniParserURL != "" {
		omniClient = omniparser.NewClient(cfg.OmniParserURL)
		if hErr := omniClient.HealthCheck(context.Background()); hErr != nil {
			if cfg.HardFailOmni {
				return fmt.Errorf("omniparser hard-fail: %s unreachable: %w (start it with ~/Code/personal/OmniParser/start_mac.sh)", cfg.OmniParserURL, hErr)
			}
			logger.Warn("OmniParser not reachable, visual annotations disabled", slog.String("url", cfg.OmniParserURL), slog.String("error", hErr.Error()))
			omniClient = nil
		} else {
			fmt.Printf("[omni] OmniParser V2 connected at %s\n\n", cfg.OmniParserURL)
		}
	}

	agent, err := buildMemberAgent(cfg.Headless, cfg.RemoteDebugURL, cfg.PatternFile, cfg.OllamaURL, cfg.SmartModel)
	if err != nil {
		return fmt.Errorf("init agent: %w", err)
	}
	defer agent.Close()

	if cfg.URL != "" {
		fmt.Printf("[nav] Navigating to %s\n", cfg.URL)
		if navErr := agent.Navigate(cfg.URL); navErr != nil {
			return fmt.Errorf("navigate: %w", navErr)
		}
		time.Sleep(2 * time.Second)
	}

	if browser := agent.Browser(); browser != nil {
		currentURL, urlErr := browser.CurrentURL()
		if err := guardPageOrFail(currentURL, urlErr, cfg.URL); err != nil {
			return fmt.Errorf("page readiness check failed: %w", err)
		}
		if urlErr == nil {
			fmt.Printf("[page] Current URL: %s\n", currentURL)
		}
	}

	var (
		results       []DemoStepResult
		traceEntries  []EvolutionTraceEntry
		tierCounts    = map[string]int{}
		healPathCount = map[string]int{}
	)

	stepTimeout := cfg.StepTimeout
	if stepTimeout <= 0 {
		stepTimeout = 90 * time.Second
	}

	for i, instruction := range scenario.NaturalLanguage {
		stepNum := i + 1
		actionType := actionTypeForStep(scenario, i)
		actionValue := actionValueForStep(scenario, i)
		fmt.Printf("--- Step %d/%d ---\n", stepNum, len(scenario.NaturalLanguage))
		fmt.Printf("  NL: %q\n", instruction)
		fmt.Printf("  Action: %s\n", actionType)

		stepStart := time.Now()
		result := DemoStepResult{
			StepIndex:   stepNum,
			Instruction: instruction,
			ActionType:  actionType,
			Tier:        agent.CurrentTier().String(),
		}

		var screenshot []byte
		browser := agent.Browser()
		if browser != nil {
			ss, ssErr := browser.CaptureScreenshot()
			if ssErr == nil {
				screenshot = ss
			}
		}

		if screenshot != nil {
			rawPath := filepath.Join(screenshotsDir, fmt.Sprintf("step-%02d-raw.png", stepNum))
			_ = os.WriteFile(rawPath, screenshot, 0o644)
		}

		if omniClient != nil && screenshot != nil {
			parseResult, parseErr := omniClient.Parse(context.Background(), screenshot)
			if parseErr == nil {
				result.Elements = len(parseResult.Elements)
				fmt.Printf("  OmniParser: %d elements detected\n", result.Elements)

				ssPath := filepath.Join(screenshotsDir, fmt.Sprintf("step-%02d-annotated.png", stepNum))
				saved := false

				if parseResult.SOMImageB64 != "" {
					somBytes, decErr := base64.StdEncoding.DecodeString(parseResult.SOMImageB64)
					if decErr == nil {
						if wErr := os.WriteFile(ssPath, somBytes, 0o644); wErr == nil {
							saved = true
						}
					}
				}

				if !saved {
					annotated, annErr := omniparser.AnnotateScreenshot(screenshot, parseResult)
					if annErr == nil {
						_ = omniparser.SaveAnnotatedPNG(annotated, ssPath)
					}
				}
				result.ScreenshotAt = ssPath
				fmt.Printf("  Screenshot: %s\n", ssPath)

				if cfg.Visual && ssPath != "" {
					openScreenshotForVisual(ssPath, 5*time.Second)
				}
			} else {
				logger.Warn("OmniParser parse failed", slog.String("error", parseErr.Error()))
			}
		}

		targetID := fmt.Sprintf("demo-step-%d", stepNum)
		if i < len(scenario.SelectorsUsed) {
			targetID = fmt.Sprintf("%s-sel-%d", scenario.ID, i)
		}
		action := uiauto.Action{
			Type:        actionType,
			TargetID:    targetID,
			Description: instruction,
			Value:       actionValue,
		}

		stepCtx, stepCancel := context.WithTimeout(context.Background(), stepTimeout)
		taskResult := agent.RunTask(stepCtx, scenario.ID, []uiauto.Action{action})
		stepCancel()

		result.Latency = time.Since(stepStart)

		if taskResult.Error == nil {
			result.Status = "PASS"
			fmt.Printf("  Result: PASS (%.1fs)\n", result.Latency.Seconds())
		} else {
			result.Status = "FAIL"
			result.Error = taskResult.Error.Error()
			fmt.Printf("  Result: FAIL -- %s (%.1fs)\n", result.Error, result.Latency.Seconds())
		}

		// Heal path: read from any heal data the task result exposes.
		if hp := extractHealPath(taskResult); hp != "" {
			result.HealPath = hp
			healPathCount[hp]++
		}

		result.Tier = agent.CurrentTier().String()
		tierCounts[result.Tier]++
		fmt.Printf("  Tier: %s\n", result.Tier)

		// Structured logging per step (machine-friendly observability).
		logger.Info("demo_step",
			slog.String("scenario_id", scenario.ID),
			slog.Int("step", stepNum),
			slog.String("status", result.Status),
			slog.String("tier", result.Tier),
			slog.String("heal_path", result.HealPath),
			slog.Int("elements", result.Elements),
			slog.Int64("latency_ms", result.Latency.Milliseconds()),
			slog.String("action_type", actionType),
			slog.String("instruction", instruction),
		)

		results = append(results, result)
		traceEntries = append(traceEntries, EvolutionTraceEntry{
			ID:         fmt.Sprintf("%s-step-%d", scenario.ID, stepNum),
			ScenarioID: scenario.ID,
			TaskName:   "ui_action",
			StartTime:  stepStart.UTC().Format(time.RFC3339Nano),
			EndTime:    time.Now().UTC().Format(time.RFC3339Nano),
			Success:    result.Status == "PASS",
			LatencyMs:  result.Latency.Milliseconds(),
			Tier:       result.Tier,
			ToolsCalled: func() []string {
				tools := []string{"chromedp"}
				if omniClient != nil {
					tools = append(tools, "omniparser")
				}
				return tools
			}(),
			Metadata: map[string]interface{}{
				"instruction":   instruction,
				"action_type":   actionType,
				"scenario_name": scenario.Name,
				"source":        scenario.Source,
				"heal_path":     result.HealPath,
			},
		})

		if i < len(scenario.NaturalLanguage)-1 {
			fmt.Printf("  [pause %.0fs for narration]\n\n", cfg.StepDelay.Seconds())
			time.Sleep(cfg.StepDelay)
		}
	}

	finishedAt := time.Now().UTC().Format(time.RFC3339)
	fmt.Printf("\n=== Demo Complete ===\n")
	passed := 0
	var totalLatency time.Duration
	for _, r := range results {
		if r.Status == "PASS" {
			passed++
		}
		totalLatency += r.Latency
	}
	fmt.Printf("Results: %d/%d passed\n", passed, len(results))

	resultsPath := filepath.Join(resultsDir, "demo-results.json")
	if data, jErr := json.MarshalIndent(results, "", "  "); jErr == nil {
		_ = os.WriteFile(resultsPath, data, 0o644)
		fmt.Printf("Results saved: %s\n", resultsPath)
	}

	if cfg.MetricsJSON {
		var avgMs int64
		if len(results) > 0 {
			avgMs = totalLatency.Milliseconds() / int64(len(results))
		}
		summary := DemoMetricsSummary{
			ScenarioID:      scenario.ID,
			ScenarioName:    scenario.Name,
			TotalSteps:      len(results),
			PassedSteps:     passed,
			FailedSteps:     len(results) - passed,
			TotalLatencyMs:  totalLatency.Milliseconds(),
			AvgLatencyMs:    avgMs,
			TierBreakdown:   tierCounts,
			HealPathSummary: healPathCount,
			Steps:           results,
			StartedAt:       startedAt,
			FinishedAt:      finishedAt,
			Source:          scenario.Source,
		}
		metricsPath := filepath.Join(resultsDir, "demo-metrics.json")
		if data, jErr := json.MarshalIndent(summary, "", "  "); jErr == nil {
			_ = os.WriteFile(metricsPath, data, 0o644)
			fmt.Printf("Metrics saved: %s\n", metricsPath)
		}
		tracePath := filepath.Join(resultsDir, "evolution-trace.json")
		if data, jErr := json.MarshalIndent(traceEntries, "", "  "); jErr == nil {
			_ = os.WriteFile(tracePath, data, 0o644)
			fmt.Printf("Evolution trace saved: %s\n", tracePath)
		}
	}

	fmt.Printf("Run directory: %s\n", runDir)

	return nil
}

// actionTypeForStep returns the action type for the i-th step, defaulting to
// "click" when ActionTypes is missing or shorter than NaturalLanguage.
func actionTypeForStep(s NLScenario, i int) string {
	if i >= 0 && i < len(s.ActionTypes) {
		v := strings.TrimSpace(s.ActionTypes[i])
		if v != "" {
			return v
		}
	}
	return "click"
}

// actionValueForStep returns the per-step value (e.g. text to type) when set.
func actionValueForStep(s NLScenario, i int) string {
	if i >= 0 && i < len(s.ActionValues) {
		return s.ActionValues[i]
	}
	return ""
}

// extractHealPath reads the heal-path tag from a task result, if present. We
// scan the result fields with a minimal contract to avoid coupling demo to
// internal types beyond what's needed.
func extractHealPath(_ uiauto.TaskResult) string {
	// Today the TaskResult does not expose heal-path directly; the tier already
	// reflects which path served the request. We leave this as a future hook
	// that learning_loop telemetry will populate.
	return ""
}

// openScreenshotForVisual opens a screenshot file via the OS-native viewer.
// Best-effort -- never fails the demo if the opener is unavailable.
func openScreenshotForVisual(path string, hold time.Duration) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", "-a", "Preview", path)
	case "linux":
		cmd = exec.Command("xdg-open", path)
	default:
		return
	}
	if err := cmd.Start(); err != nil {
		logger.Warn("visual open failed", slog.String("path", path), slog.String("error", err.Error()))
		return
	}
	time.Sleep(hold)
}

func loadScenarios(path string) ([]NLScenario, error) {
	if path == "" {
		return nil, fmt.Errorf("--scenario flag is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read scenario file: %w", err)
	}
	var scenarios []NLScenario
	if err := json.Unmarshal(data, &scenarios); err != nil {
		return nil, fmt.Errorf("parse scenarios: %w", err)
	}
	if len(scenarios) == 0 {
		return nil, fmt.Errorf("no scenarios found in %s", path)
	}
	return scenarios, nil
}

// checkPageReadiness validates that the current page URL is not blank or empty.
// Returns an error if the page is about:blank, chrome://newtab, or empty string.
func checkPageReadiness(pageURL string) error {
	normalized := strings.TrimSpace(strings.ToLower(pageURL))
	if normalized == "" || normalized == "about:blank" || strings.HasPrefix(normalized, "chrome://newtab") {
		return fmt.Errorf("page is %q -- navigate to a target page first or use --url to specify one", pageURL)
	}
	return nil
}

// guardPageOrFail is the unified guardrail for page readiness.
// currentURL is the result of browser.CurrentURL(), urlErr is its error,
// and urlFlag is the --url flag value. Returns nil if safe to proceed.
func guardPageOrFail(currentURL string, urlErr error, urlFlag string) error {
	if urlErr != nil {
		if urlFlag != "" {
			return nil
		}
		return fmt.Errorf("browser unreachable (%w) -- ensure Chrome has at least one tab open or use --url", urlErr)
	}
	if readyErr := checkPageReadiness(currentURL); readyErr != nil {
		if urlFlag != "" {
			return nil
		}
		return readyErr
	}
	return nil
}
