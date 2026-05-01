package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"

	"github.com/nfsarch33/uiauto-framework/pkg/llm"
	"github.com/nfsarch33/uiauto-framework/pkg/uiauto"
)

var (
	version = "dev"
	commit  = "unknown"

	logger  *slog.Logger
	appSlog *slog.Logger
	promReg = prometheus.NewRegistry()
)

// A2ACard describes the UI Agent's capabilities for the IronClaw fleet.
type A2ACard struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Version      string   `json:"version"`
	URL          string   `json:"url"`
	Capabilities []string `json:"capabilities"`
	InputModes   []string `json:"input_modes"`
	OutputModes  []string `json:"output_modes"`
}

func defaultA2ACard(baseURL string) A2ACard {
	return A2ACard{
		Name:        "ui-agent",
		Description: "IronClaw UI Agent: self-healing browser automation with multi-model routing, DOM drift detection, and VLM verification.",
		Version:     version,
		URL:         baseURL,
		Capabilities: []string{
			"browser_automation",
			"dom_self_healing",
			"vlm_visual_verification",
			"pattern_tracking",
			"drift_detection",
			"multi_model_routing",
		},
		InputModes:  []string{"text", "json"},
		OutputModes: []string{"json"},
	}
}

// serverAgent is the subset of *uiauto.MemberAgent required by the HTTP
// handlers. Defining it as an interface lets tests inject a fake without
// launching a real browser.
type serverAgent interface {
	IsDegraded() bool
	IsConverged() bool
	CurrentTier() uiauto.ModelTier
	Navigate(url string) error
	DetectDriftAndHeal(ctx context.Context) []uiauto.HealResult
	Metrics() uiauto.AggregatedMetrics
	TaskCount() int
	RunTask(ctx context.Context, taskID string, actions []uiauto.Action) uiauto.TaskResult
}

// buildServeMux wires up the HTTP handlers used by `ui-agent serve`. It is
// extracted so unit tests can drive the endpoints with httptest + a fake
// serverAgent without binding a port or starting a browser.
func buildServeMux(card A2ACard, agent serverAgent, headless bool, patternFile string) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/.well-known/a2a-card", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(card)
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "ok",
			"version":   version,
			"time":      time.Now().UTC().Format(time.RFC3339),
			"degraded":  agent.IsDegraded(),
			"converged": agent.IsConverged(),
			"tier":      agent.CurrentTier().String(),
		})
	})

	mux.HandleFunc("/api/v1/heal", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			PageURL     string `json:"page_url"`
			Selector    string `json:"selector"`
			ElementType string `json:"element_type"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}

		if req.PageURL != "" {
			if navErr := agent.Navigate(req.PageURL); navErr != nil {
				slog.Error("heal: navigation failed", "url", req.PageURL, "error", navErr)
			}
		}

		results := agent.DetectDriftAndHeal(r.Context())

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":       "completed",
			"heal_results": results,
			"count":        len(results),
		})
	})

	mux.HandleFunc("/api/v1/status", func(w http.ResponseWriter, r *http.Request) {
		metrics := agent.Metrics()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"agent":        "ui-agent",
			"version":      version,
			"headless":     headless,
			"pattern_file": patternFile,
			"capabilities": card.Capabilities,
			"tier":         agent.CurrentTier().String(),
			"converged":    agent.IsConverged(),
			"degraded":     agent.IsDegraded(),
			"task_count":   agent.TaskCount(),
			"metrics":      metrics,
		})
	})

	mux.HandleFunc("/api/v1/run-task", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			TaskID  string          `json:"task_id"`
			Actions []uiauto.Action `json:"actions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}

		result := agent.RunTask(r.Context(), req.TaskID, req.Actions)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	})

	return mux
}

// printHealResults formats the heal results for the `ui-agent heal` CLI.
// Extracted from healCmd.RunE so tests can verify the output without launching
// a browser.
func printHealResults(out io.Writer, results []uiauto.HealResult) {
	fmt.Fprintf(out, "Self-healing results: %d repairs attempted\n", len(results))
	for i, r := range results {
		status := "FAILED"
		if r.Success {
			status = "OK"
		}
		fmt.Fprintf(out, "  [%d] %s target=%s method=%s\n", i, status, r.TargetID, r.Method)
	}
}

func main() {
	logger = slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	appSlog = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	root := &cobra.Command{
		Use:     "ui-agent",
		Short:   "IronClaw UI Agent -- self-healing browser automation",
		Version: fmt.Sprintf("%s (%s)", version, commit),
	}

	root.AddCommand(serveCmd())
	root.AddCommand(healCmd())
	root.AddCommand(statusCmd())
	root.AddCommand(demoCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func buildMemberAgent(headless bool, remoteDebugURL, patternFile, ollamaURL, smartModel string) (*uiauto.MemberAgent, error) {
	var provider llm.Provider
	if ollamaURL != "" {
		cfg := llm.Config{
			BaseURL: ollamaURL,
			APIKey:  os.Getenv("OPENAI_API_KEY"),
			Model:   smartModel,
			Timeout: 120 * time.Second,
		}
		provider = llm.NewClient(cfg)
	}

	var smartModels []string
	if smartModel != "" {
		smartModels = []string{smartModel}
	}

	agentCfg := uiauto.MemberAgentConfig{
		Headless:       headless,
		RemoteDebugURL: remoteDebugURL,
		PatternFile:    patternFile,
		LLMProvider:    provider,
		SmartModels:    smartModels,
		Logger:         appSlog,
	}

	return uiauto.NewMemberAgent(agentCfg, uiauto.WithMemberLogger(appSlog))
}

func serveCmd() *cobra.Command {
	var (
		port           int
		headless       bool
		remoteDebugURL string
		patternFile    string
		metricsPort    int
		ollamaURL      string
		smartModel     string
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the UI Agent HTTP server with A2A card, MemberAgent, and self-healing handler",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sig := make(chan os.Signal, 1)
			signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

			uiMetrics := uiauto.NewMetrics(promReg)

			agent, err := buildMemberAgent(headless, remoteDebugURL, patternFile, ollamaURL, smartModel)
			if err != nil {
				return fmt.Errorf("failed to initialize MemberAgent: %w", err)
			}
			defer agent.Close()

			baseURL := fmt.Sprintf("http://localhost:%d", port)
			card := defaultA2ACard(baseURL)

			mux := buildServeMux(card, agent, headless, patternFile)

			go func() {
				metricsMux := http.NewServeMux()
				metricsMux.Handle("/metrics", promhttp.HandlerFor(promReg, promhttp.HandlerOpts{}))
				metricsAddr := fmt.Sprintf(":%d", metricsPort)
				logger.Info("metrics server starting", "addr", metricsAddr)
				if mErr := http.ListenAndServe(metricsAddr, metricsMux); mErr != nil {
					logger.Error("metrics server failed", "error", mErr)
				}
			}()

			srv := &http.Server{
				Addr:    fmt.Sprintf(":%d", port),
				Handler: mux,
			}

			go func() {
				<-sig
				logger.Info("shutting down ui-agent")
				cancel()
				shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer shutdownCancel()
				_ = srv.Shutdown(shutdownCtx)
			}()

			logger.Info("ui-agent starting",
				"port", port,
				"headless", headless,
				"ollama_url", ollamaURL,
				"smart_model", smartModel,
				"pattern_file", patternFile,
			)

			_ = ctx
			_ = uiMetrics

			return srv.ListenAndServe()
		},
	}

	cmd.Flags().IntVar(&port, "port", 8090, "HTTP server port")
	cmd.Flags().BoolVar(&headless, "headless", true, "Run browser in headless mode")
	cmd.Flags().StringVar(&remoteDebugURL, "remote-debug-url", "", "Chrome DevTools remote debug URL")
	cmd.Flags().StringVar(&patternFile, "pattern-file", "/tmp/uiauto_patterns.json", "Path to pattern store file")
	cmd.Flags().IntVar(&metricsPort, "metrics-port", 9090, "Prometheus metrics port")
	cmd.Flags().StringVar(&ollamaURL, "ollama-url", "http://127.0.0.1:11434/v1", "Ollama API base URL")
	cmd.Flags().StringVar(&smartModel, "smart-model", "kwangsuklee/Qwen3.5-27B-Claude-4.6-Opus-Reasoning-Distilled-GGUF", "Primary model for smart discovery")

	return cmd
}

func healCmd() *cobra.Command {
	var (
		selector       string
		elementType    string
		pageURL        string
		description    string
		remoteDebugURL string
		patternFile    string
		ollamaURL      string
		smartModel     string
	)

	cmd := &cobra.Command{
		Use:   "heal",
		Short: "Run a one-shot self-healing repair on a broken selector",
		RunE: func(cmd *cobra.Command, args []string) error {
			agent, err := buildMemberAgent(true, remoteDebugURL, patternFile, ollamaURL, smartModel)
			if err != nil {
				return fmt.Errorf("failed to initialize MemberAgent: %w", err)
			}
			defer agent.Close()

			if pageURL != "" {
				if navErr := agent.Navigate(pageURL); navErr != nil {
					return fmt.Errorf("navigation failed: %w", navErr)
				}
			}

			logger.Info("heal command -- detecting and healing drift",
				"selector", selector,
				"element_type", elementType,
				"page_url", pageURL,
			)

			results := agent.DetectDriftAndHeal(context.Background())
			printHealResults(os.Stdout, results)

			return nil
		},
	}

	cmd.Flags().StringVar(&selector, "selector", "", "Broken CSS selector to repair")
	cmd.Flags().StringVar(&elementType, "element-type", "", "Element type (e.g., login_button)")
	cmd.Flags().StringVar(&pageURL, "page-url", "", "Page URL to navigate to")
	cmd.Flags().StringVar(&description, "description", "", "Human description of the element")
	cmd.Flags().StringVar(&remoteDebugURL, "remote-debug-url", "", "Chrome DevTools remote debug URL")
	cmd.Flags().StringVar(&patternFile, "pattern-file", "/tmp/uiauto_patterns.json", "Path to pattern store file")
	cmd.Flags().StringVar(&ollamaURL, "ollama-url", "http://127.0.0.1:11434/v1", "Ollama API base URL")
	cmd.Flags().StringVar(&smartModel, "smart-model", "kwangsuklee/Qwen3.5-27B-Claude-4.6-Opus-Reasoning-Distilled-GGUF", "Primary model for smart discovery")

	return cmd
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show UI Agent status and A2A card",
		RunE: func(cmd *cobra.Command, args []string) error {
			card := defaultA2ACard("http://localhost:8090")
			data, _ := json.MarshalIndent(card, "", "  ")
			fmt.Println(string(data))
			return nil
		},
	}
}
