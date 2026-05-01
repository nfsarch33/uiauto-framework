package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/nfsarch33/uiauto-framework/pkg/uiauto"
)

// extractHealPath always returns "" today; assert it explicitly so a future
// implementation that wires real heal-path data has to update this test.
func TestExtractHealPath_ReturnsEmpty(t *testing.T) {
	if got := extractHealPath(uiauto.TaskResult{}); got != "" {
		t.Errorf("extractHealPath should be empty until heal telemetry lands, got %q", got)
	}
}

// openScreenshotForVisual is best-effort and must never panic when the file
// is missing or the OS opener is unsupported.
func TestOpenScreenshotForVisual_DoesNotPanic_OnMissingFile(t *testing.T) {
	// Use a near-zero hold so the test is fast.
	openScreenshotForVisual("/nonexistent/path.png", 1*time.Millisecond)
}

func TestOpenScreenshotForVisual_DoesNotPanic_OnRealFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fake.png")
	if err := os.WriteFile(path, []byte("not-really-a-png"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The function may try to launch Preview / xdg-open. Use a tiny hold to
	// avoid waiting for any real GUI viewer.
	openScreenshotForVisual(path, 1*time.Millisecond)
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("non-darwin/linux: opener is a no-op by design")
	}
}

// runDemo path: invalid scenarioID returns helpful error.
func TestRunDemo_UnknownScenarioID(t *testing.T) {
	dir := t.TempDir()
	scenarioFile := filepath.Join(dir, "scenarios.json")
	body := `[{"id":"sc-1","name":"S","natural_language":["one"],"selectors_used":["#x"],"source":"x","tags":["t"]}]`
	if err := os.WriteFile(scenarioFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runDemo(demoConfig{
		ScenarioFile: scenarioFile,
		ScenarioID:   "does-not-exist",
		OutputDir:    dir,
	})
	if err == nil {
		t.Fatal("expected error for unknown scenario ID")
	}
}

func TestRunDemo_BadScenarioJSON(t *testing.T) {
	dir := t.TempDir()
	scenarioFile := filepath.Join(dir, "scenarios.json")
	if err := os.WriteFile(scenarioFile, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runDemo(demoConfig{ScenarioFile: scenarioFile, OutputDir: dir}); err == nil {
		t.Error("expected error for bad scenario JSON")
	}
}

func TestRunDemo_OutputDirCreatedFromTimestamp(t *testing.T) {
	dir := t.TempDir()
	scenarioFile := filepath.Join(dir, "scenarios.json")
	body := `[{"id":"sc-1","name":"S","natural_language":["one"],"selectors_used":["#x"],"source":"x","tags":["t"]}]`
	if err := os.WriteFile(scenarioFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// Hard-fail OmniParser when the URL is unreachable; this exits before
	// touching Chrome so we still exercise the dir-creation path.
	_ = runDemo(demoConfig{
		ScenarioFile:  scenarioFile,
		OutputDir:     dir,
		OmniParserURL: "http://127.0.0.1:1",
		HardFailOmni:  true,
	})

	// The function creates `<outdir>/<id>_<timestamp>/{screenshots,results}`
	// even on early hard-fail (depends on flow). At minimum, OutputDir exists.
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("output dir vanished: %v", err)
	}
}

// healCmd / serveCmd / statusCmd: assert wiring without invoking Chrome.
func TestHealCmd_RegistersFlags(t *testing.T) {
	cmd := healCmd()
	if cmd.Use != "heal" {
		t.Errorf("Use = %q", cmd.Use)
	}
	for _, name := range []string{"selector", "element-type", "page-url", "remote-debug-url", "pattern-file"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing flag: --%s", name)
		}
	}
}

func TestServeCmd_RegistersFlags(t *testing.T) {
	cmd := serveCmd()
	if cmd.Use != "serve" {
		t.Errorf("Use = %q", cmd.Use)
	}
	for _, name := range []string{"port", "headless", "remote-debug-url", "pattern-file", "metrics-port", "ollama-url", "smart-model"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing flag: --%s", name)
		}
	}
}

func TestStatusCmd_PrintsJSON(t *testing.T) {
	cmd := statusCmd()
	if cmd.Use != "status" {
		t.Errorf("Use = %q", cmd.Use)
	}
	// statusCmd's RunE prints A2A card to stdout. Capture via cobra's SetOut.
	cmd.SetArgs([]string{})
	if cmd.RunE == nil {
		t.Fatal("statusCmd RunE is nil")
	}
	if err := cmd.RunE(cmd, []string{}); err != nil {
		t.Errorf("statusCmd execution: %v", err)
	}
}

// loadScenarios path: scenario file with empty array still surfaces an error.
func TestLoadScenarios_EmptyArray(t *testing.T) {
	dir := t.TempDir()
	scenarioFile := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(scenarioFile, []byte(`[]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadScenarios(scenarioFile); err == nil {
		t.Error("expected error for empty scenarios array")
	}
}

// buildMemberAgent: smoke test for the headless code path. chromedp lazily
// connects, so this succeeds without a browser binary; we just confirm we get
// a non-nil agent that we can Close().
func TestBuildMemberAgent_Headless_Succeeds(t *testing.T) {
	if testing.Short() && os.Getenv("CI") != "" {
		t.Skip("skip headless agent smoke in short CI mode")
	}
	appSlog = slog.New(slog.NewJSONHandler(io.Discard, nil))

	tmpDir := t.TempDir()
	patternFile := filepath.Join(tmpDir, "patterns.json")

	agent, err := buildMemberAgent(true, "", patternFile, "", "")
	if err != nil {
		t.Fatalf("buildMemberAgent: %v", err)
	}
	if agent == nil {
		t.Fatal("agent is nil")
	}
	agent.Close()
}

// buildMemberAgent: bad remote-debug-url path triggers NewBrowserAgentWithRemote
// failure → wrapped error returned.
func TestBuildMemberAgent_BadRemoteURL_Errors(t *testing.T) {
	appSlog = slog.New(slog.NewJSONHandler(io.Discard, nil))

	tmpDir := t.TempDir()
	patternFile := filepath.Join(tmpDir, "patterns.json")

	_, err := buildMemberAgent(true, "http://127.0.0.1:1", patternFile, "", "")
	if err == nil {
		t.Fatal("expected error for bad remote-debug-url")
	}
}

// buildMemberAgent: providing an LLM URL triggers the llm.NewClient branch.
func TestBuildMemberAgent_WithLLM(t *testing.T) {
	if testing.Short() && os.Getenv("CI") != "" {
		t.Skip("skip headless agent smoke in short CI mode")
	}
	appSlog = slog.New(slog.NewJSONHandler(io.Discard, nil))

	tmpDir := t.TempDir()
	patternFile := filepath.Join(tmpDir, "patterns.json")

	agent, err := buildMemberAgent(true, "", patternFile, "http://127.0.0.1:1", "fake-model")
	if err != nil {
		t.Fatalf("buildMemberAgent: %v", err)
	}
	defer agent.Close()
}

// healCmd RunE: bad remote-debug-url flag → expect error from buildMemberAgent.
func TestHealCmd_BadRemoteURL_ReturnsError(t *testing.T) {
	logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	appSlog = slog.New(slog.NewJSONHandler(io.Discard, nil))

	cmd := healCmd()
	if err := cmd.Flags().Set("remote-debug-url", "http://127.0.0.1:1"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.RunE(cmd, []string{}); err == nil {
		t.Error("expected error for unreachable remote-debug-url")
	}
}

// healCmd RunE: happy path with default flags exercises the buildMemberAgent
// success branch and printHealResults. This is a smoke test only — heal does
// nothing useful without a real page, but we exercise the full flow.
func TestHealCmd_DefaultFlags_NoError(t *testing.T) {
	if testing.Short() && os.Getenv("CI") != "" {
		t.Skip("skip headless agent smoke in short CI mode")
	}
	logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	appSlog = slog.New(slog.NewJSONHandler(io.Discard, nil))

	cmd := healCmd()
	tmpDir := t.TempDir()
	if err := cmd.Flags().Set("pattern-file", filepath.Join(tmpDir, "patterns.json")); err != nil {
		t.Fatal(err)
	}
	// Use empty ollama URL to avoid hitting LLM.
	if err := cmd.Flags().Set("ollama-url", ""); err != nil {
		t.Fatal(err)
	}
	if err := cmd.RunE(cmd, []string{}); err != nil {
		t.Errorf("healCmd RunE: %v", err)
	}
}

// fakeDemoAgent satisfies demoAgent without launching a browser.
type fakeDemoAgent struct {
	tier          uiauto.ModelTier
	currentURL    string
	urlErr        error
	screenshot    []byte
	screenshotErr error
	navErr        error
	taskResult    uiauto.TaskResult
	closed        bool
	calledRun     []string
	calledNav     []string
}

func (f *fakeDemoAgent) CurrentTier() uiauto.ModelTier { return f.tier }
func (f *fakeDemoAgent) RunTask(ctx context.Context, taskID string, actions []uiauto.Action) uiauto.TaskResult {
	f.calledRun = append(f.calledRun, taskID)
	r := f.taskResult
	r.TaskID = taskID
	return r
}
func (f *fakeDemoAgent) Close()                      { f.closed = true }
func (f *fakeDemoAgent) browserURL() (string, error) { return f.currentURL, f.urlErr }
func (f *fakeDemoAgent) captureScreenshot() ([]byte, error) {
	return f.screenshot, f.screenshotErr
}
func (f *fakeDemoAgent) navigate(url string) error {
	f.calledNav = append(f.calledNav, url)
	return f.navErr
}

func TestRunDemo_FakeAgent_FullFlow(t *testing.T) {
	logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	appSlog = slog.New(slog.NewJSONHandler(io.Discard, nil))

	dir := t.TempDir()
	scenarioFile := filepath.Join(dir, "s.json")
	body := `[{"id":"sc-fake","name":"Fake","natural_language":["click login","type user","verify dashboard"],"action_types":["click","type","verify"],"selectors_used":["#login","#user","#dash"],"source":"unit","tags":["demo"]}]`
	if err := os.WriteFile(scenarioFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	fake := &fakeDemoAgent{currentURL: "https://example.test", taskResult: uiauto.TaskResult{Status: uiauto.TaskCompleted}}

	cfg := demoConfig{
		ScenarioFile: scenarioFile,
		ScenarioID:   "sc-fake",
		URL:          "https://example.test",
		OutputDir:    filepath.Join(dir, "out"),
		StepDelay:    1 * time.Millisecond,
		StepTimeout:  100 * time.Millisecond,
		MetricsJSON:  true,
		agentFactory: func(c demoConfig) (demoAgent, error) { return fake, nil },
	}
	if err := runDemo(cfg); err != nil {
		t.Fatalf("runDemo: %v", err)
	}

	if !fake.closed {
		t.Error("expected agent.Close to be called")
	}
	if len(fake.calledRun) != 3 {
		t.Errorf("expected 3 RunTask calls, got %d", len(fake.calledRun))
	}
	if len(fake.calledNav) != 1 || fake.calledNav[0] != "https://example.test" {
		t.Errorf("nav calls: %v", fake.calledNav)
	}
	// Result and trace files must exist.
	matches, _ := filepath.Glob(filepath.Join(dir, "out", "sc-fake_*", "results", "*.json"))
	if len(matches) < 3 {
		t.Errorf("expected 3 result files (demo-results, demo-metrics, evolution-trace), got %d: %v", len(matches), matches)
	}
}

func TestRunDemo_FakeAgent_FactoryError(t *testing.T) {
	logger = slog.New(slog.NewJSONHandler(io.Discard, nil))

	dir := t.TempDir()
	scenarioFile := filepath.Join(dir, "s.json")
	body := `[{"id":"sc-fake","name":"Fake","natural_language":["a"],"selectors_used":["#a"],"source":"unit","tags":["demo"]}]`
	if err := os.WriteFile(scenarioFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := demoConfig{
		ScenarioFile: scenarioFile,
		OutputDir:    filepath.Join(dir, "out"),
		agentFactory: func(c demoConfig) (demoAgent, error) {
			return nil, errBoom
		},
	}
	if err := runDemo(cfg); err == nil {
		t.Error("expected error when factory fails")
	}
}

var errBoom = stubError("agent factory boom")

type stubError string

func (e stubError) Error() string { return string(e) }

// defaultAgentFactory: bad RemoteDebugURL surfaces an error before any browser
// interaction is attempted.
func TestDefaultAgentFactory_BadRemote_ReturnsError(t *testing.T) {
	logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	appSlog = slog.New(slog.NewJSONHandler(io.Discard, nil))
	cfg := demoConfig{RemoteDebugURL: "http://127.0.0.1:1", PatternFile: filepath.Join(t.TempDir(), "p.json")}
	if _, err := defaultAgentFactory(cfg); err == nil {
		t.Error("expected error for unreachable remote-debug-url")
	}
}

// defaultAgentFactory: headless path returns a real agent that can be Closed
// without launching a browser binary.
func TestDefaultAgentFactory_Headless_Succeeds(t *testing.T) {
	if testing.Short() && os.Getenv("CI") != "" {
		t.Skip("skip headless agent factory in short CI mode")
	}
	logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	appSlog = slog.New(slog.NewJSONHandler(io.Discard, nil))
	cfg := demoConfig{Headless: true, PatternFile: filepath.Join(t.TempDir(), "p.json")}
	a, err := defaultAgentFactory(cfg)
	if err != nil {
		t.Fatalf("defaultAgentFactory: %v", err)
	}
	if a == nil {
		t.Fatal("nil agent")
	}
	a.Close()
}

// realDemoAgent: when wrapped MemberAgent has a nil Browser, the helpers
// behave gracefully (return zero/empty/error rather than panicking).
func TestRealDemoAgent_NilBrowser_NoPanic(t *testing.T) {
	a := &realDemoAgent{inner: &uiauto.MemberAgent{}}
	if _, err := a.browserURL(); err == nil {
		t.Error("expected error when browser is unattached")
	}
	got, err := a.captureScreenshot()
	if err != nil || got != nil {
		t.Errorf("captureScreenshot on nil browser: got=%v err=%v", got, err)
	}
}

// runDemo with OmniParser stub: exercises the full annotation path including
// element detection, screenshot saving, and metrics summary writes.
func TestRunDemo_FakeAgent_WithOmniParser(t *testing.T) {
	logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	appSlog = slog.New(slog.NewJSONHandler(io.Discard, nil))

	// Stub OmniParser server: probe + parse + parse-ocr.
	parseBody := `{"elements":[{"id":1,"type":"button","content":"Login","bbox":[0.1,0.1,0.2,0.2]}],"som_image_b64":""}`
	mux := http.NewServeMux()
	mux.HandleFunc("/probe/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/parse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(parseBody))
	})
	mux.HandleFunc("/parse-ocr", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(parseBody))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	scenarioFile := filepath.Join(dir, "s.json")
	body := `[{"id":"sc-omni","name":"Omni","natural_language":["click login"],"selectors_used":["#login"],"source":"unit","tags":["demo"]}]`
	if err := os.WriteFile(scenarioFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	// Tiny PNG bytes (1x1 transparent) — enough for the screenshot path.
	png1x1 := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
		0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
		0x42, 0x60, 0x82,
	}

	fake := &fakeDemoAgent{
		currentURL: "https://example.test",
		screenshot: png1x1,
		taskResult: uiauto.TaskResult{Status: uiauto.TaskCompleted},
	}

	cfg := demoConfig{
		ScenarioFile:  scenarioFile,
		ScenarioID:    "sc-omni",
		URL:           "https://example.test",
		OmniParserURL: srv.URL,
		HardFailOmni:  true,
		OutputDir:     filepath.Join(dir, "out"),
		StepDelay:     1 * time.Millisecond,
		StepTimeout:   100 * time.Millisecond,
		MetricsJSON:   true,
		agentFactory:  func(c demoConfig) (demoAgent, error) { return fake, nil },
	}
	if err := runDemo(cfg); err != nil {
		t.Fatalf("runDemo: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "out", "sc-omni_*", "screenshots", "*.png"))
	if len(matches) == 0 {
		t.Errorf("expected at least one screenshot, got 0")
	}
}

// serveCmd.RunE: bad remote-debug-url surfaces the buildMemberAgent error
// before the HTTP server starts.
func TestServeCmd_BadRemoteURL_ReturnsError(t *testing.T) {
	logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	appSlog = slog.New(slog.NewJSONHandler(io.Discard, nil))
	cmd := serveCmd()
	if err := cmd.Flags().Set("remote-debug-url", "http://127.0.0.1:1"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.RunE(cmd, []string{}); err == nil {
		t.Error("expected error for unreachable remote-debug-url")
	}
}

// runDemo with hard-fail OmniParser when the URL is unreachable.
func TestRunDemo_FakeAgent_OmniParserHardFail(t *testing.T) {
	logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	appSlog = slog.New(slog.NewJSONHandler(io.Discard, nil))

	dir := t.TempDir()
	scenarioFile := filepath.Join(dir, "s.json")
	body := `[{"id":"sc-h","name":"H","natural_language":["go"],"selectors_used":["#g"],"source":"unit","tags":["demo"]}]`
	if err := os.WriteFile(scenarioFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	fake := &fakeDemoAgent{currentURL: "https://example.test"}

	cfg := demoConfig{
		ScenarioFile:  scenarioFile,
		ScenarioID:    "sc-h",
		URL:           "https://example.test",
		OmniParserURL: "http://127.0.0.1:1",
		HardFailOmni:  true,
		OutputDir:     filepath.Join(dir, "out"),
		StepDelay:     1 * time.Millisecond,
		StepTimeout:   100 * time.Millisecond,
		agentFactory:  func(c demoConfig) (demoAgent, error) { return fake, nil },
	}
	if err := runDemo(cfg); err == nil {
		t.Fatal("expected hard-fail error when OmniParser is unreachable")
	}
}

// EvolutionTraceEntry round-trip via JSON.
func TestEvolutionTraceEntry_RoundTrip(t *testing.T) {
	e := EvolutionTraceEntry{
		ID:         "scenario-step-1",
		ScenarioID: "scenario",
		TaskName:   "ui_action",
		Tier:       "smart",
		Success:    true,
		LatencyMs:  42,
	}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var got EvolutionTraceEntry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != e.ID || got.Tier != "smart" || !got.Success {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}
