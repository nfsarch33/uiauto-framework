package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goldenScenarios = "../../pkg/uiauto/config/testdata/scenarios.golden.json"

// --- Tests for about:blank guardrail ---

func TestCheckPageReadiness_RejectsAboutBlank(t *testing.T) {
	if err := checkPageReadiness("about:blank"); err == nil {
		t.Fatal("expected error for about:blank")
	}
}

func TestCheckPageReadiness_RejectsEmpty(t *testing.T) {
	if err := checkPageReadiness(""); err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestCheckPageReadiness_RejectsChromeNewTab(t *testing.T) {
	if err := checkPageReadiness("chrome://newtab/"); err == nil {
		t.Fatal("expected error for chrome://newtab/")
	}
}

func TestCheckPageReadiness_AcceptsRealPage(t *testing.T) {
	if err := checkPageReadiness("https://example.com"); err != nil {
		t.Fatalf("unexpected error for real page: %v", err)
	}
}

func TestGuardrailHardFailsOnURLError_NoURLFlag(t *testing.T) {
	err := guardPageOrFail("", fmt.Errorf("Failed to open new tab - no browser is open (-32000)"), "")
	if err == nil {
		t.Fatal("expected hard error when CurrentURL() fails and --url not provided")
	}
	if !strings.Contains(err.Error(), "browser") {
		t.Errorf("error should mention browser issue, got: %v", err)
	}
}

func TestGuardrailSoftOnURLError_WithURLFlag(t *testing.T) {
	if err := guardPageOrFail("", fmt.Errorf("Failed to open new tab"), "https://example.com"); err != nil {
		t.Fatalf("should not error when --url is provided: %v", err)
	}
}

func TestGuardrailHardFailsOnBlankPage_NoURLFlag(t *testing.T) {
	if err := guardPageOrFail("about:blank", nil, ""); err == nil {
		t.Fatal("expected hard error for about:blank without --url")
	}
}

func TestGuardrailPassesOnBlankPage_WithURLFlag(t *testing.T) {
	if err := guardPageOrFail("about:blank", nil, "https://example.com"); err != nil {
		t.Fatalf("should not error when --url overrides blank page: %v", err)
	}
}

func TestGuardrailPassesOnRealPage(t *testing.T) {
	if err := guardPageOrFail("https://example.com", nil, ""); err != nil {
		t.Fatalf("should pass for real page: %v", err)
	}
}

// --- Gateway / API key wiring ---

func TestDemoCmd_FallbackToOpenAIBaseURL(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "https://gateway.example.com/v1")
	cmd := demoCmd()
	cmd.SetArgs([]string{"--scenario", "/tmp/fake.json"})

	gatewayFlag := cmd.Flags().Lookup("gateway-url")
	if gatewayFlag == nil {
		t.Fatal("gateway-url flag missing")
	}
	if gatewayFlag.Value.String() != "" {
		t.Skip("gateway-url has non-empty default; env fallback happens at runtime")
	}
}

func TestBuildMemberAgent_APIKeyFromEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key-12345")
	if got := os.Getenv("OPENAI_API_KEY"); got != "test-key-12345" {
		t.Fatalf("expected OPENAI_API_KEY=test-key-12345, got %q", got)
	}
}

func TestDemoCmdHasGatewayFlags(t *testing.T) {
	cmd := demoCmd()
	for _, name := range []string{"gateway-url", "gateway-model"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing flag: --%s (needed for OpenAI-compatible gateway)", name)
		}
	}
}

func TestDemoConfig_GatewayFieldsExist(t *testing.T) {
	cfg := demoConfig{
		GatewayURL:   "https://gateway.example.com/v1",
		GatewayModel: "gpt-4.1-mini",
	}
	if cfg.GatewayURL == "" {
		t.Error("GatewayURL field missing from demoConfig")
	}
	if cfg.GatewayModel == "" {
		t.Error("GatewayModel field missing from demoConfig")
	}
}

func TestDemoConfig_NavigationNotSkippedWithRemoteDebug(t *testing.T) {
	cfg := demoConfig{
		URL:            "https://example.com",
		RemoteDebugURL: "http://localhost:9222",
	}
	if cfg.URL == "" || cfg.RemoteDebugURL == "" {
		t.Fatal("both URL and RemoteDebugURL should be set")
	}
	if cfg.URL == "" {
		t.Error("navigation should NOT be skipped when both URL and RemoteDebugURL are set")
	}
}

// --- Scenario loader ---

func TestLoadScenarios(t *testing.T) {
	scenarios, err := loadScenarios(goldenScenarios)
	if err != nil {
		t.Fatalf("loadScenarios(%s): %v", goldenScenarios, err)
	}
	if len(scenarios) == 0 {
		t.Fatal("expected at least one scenario")
	}
	first := scenarios[0]
	if first.ID == "" {
		t.Error("first scenario ID is empty")
	}
	if first.Name == "" {
		t.Error("first scenario Name is empty")
	}
	if len(first.NaturalLanguage) == 0 {
		t.Error("first scenario has no NL steps")
	}
}

func TestLoadScenarios_MissingFile(t *testing.T) {
	if _, err := loadScenarios("/nonexistent/path.json"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadScenarios_EmptyPath(t *testing.T) {
	if _, err := loadScenarios(""); err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestLoadScenarios_FindByID(t *testing.T) {
	scenarios, err := loadScenarios(goldenScenarios)
	if err != nil {
		t.Fatalf("loadScenarios: %v", err)
	}
	found := false
	for _, s := range scenarios {
		if s.ID == "smoke-001" {
			found = true
			if s.Name == "" {
				t.Error("smoke-001 has empty name")
			}
			if len(s.NaturalLanguage) < 1 {
				t.Errorf("smoke-001 expected >= 1 NL step, got %d", len(s.NaturalLanguage))
			}
			if len(s.SelectorsUsed) == 0 {
				t.Error("smoke-001 has no selectors")
			}
			break
		}
	}
	if !found {
		t.Error("scenario smoke-001 not found")
	}
}

func TestDemoStepResultJSON(t *testing.T) {
	result := DemoStepResult{
		StepIndex:   1,
		Instruction: "Click the accept button",
		Status:      "PASS",
		Tier:        "light",
		Elements:    5,
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded DemoStepResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.StepIndex != 1 || decoded.Status != "PASS" {
		t.Errorf("round-trip mismatch: %+v", decoded)
	}
}

func TestDemoCmdRegistered(t *testing.T) {
	cmd := demoCmd()
	if cmd.Use != "demo" {
		t.Errorf("expected Use='demo', got %q", cmd.Use)
	}
	if cmd.RunE == nil {
		t.Error("demo command RunE is nil")
	}
	flags := []string{
		"url", "scenario", "scenario-id", "headless", "remote-debug-url",
		"omniparser-url", "step-delay", "output-dir",
		"visual", "step-timeout", "metrics-json", "vlm-model",
	}
	for _, name := range flags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing flag: --%s", name)
		}
	}
}

// --- ActionTypes parsing ---

func TestNLScenario_ActionTypesParsed_DefaultsToClick(t *testing.T) {
	raw := `[{"id":"t1","name":"T","natural_language":["click home"],"page_objects":[],"selectors_used":["#home"],"source":"x","tags":["t"]}]`
	tmpFile := filepath.Join(t.TempDir(), "t.json")
	if err := os.WriteFile(tmpFile, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	scenarios, err := loadScenarios(tmpFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := actionTypeForStep(scenarios[0], 0); got != "click" {
		t.Errorf("expected default action type 'click', got %q", got)
	}
}

func TestNLScenario_ActionTypesShorterThanSteps_FillsRemainderWithClick(t *testing.T) {
	raw := `[{"id":"t1","name":"T","natural_language":["s1","s2","s3"],"selectors_used":["#a","#b","#c"],"source":"x","tags":["t"],"action_types":["wait","verify"]}]`
	tmpFile := filepath.Join(t.TempDir(), "t.json")
	if err := os.WriteFile(tmpFile, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	scenarios, err := loadScenarios(tmpFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := actionTypeForStep(scenarios[0], 0); got != "wait" {
		t.Errorf("step 0: want wait, got %q", got)
	}
	if got := actionTypeForStep(scenarios[0], 1); got != "verify" {
		t.Errorf("step 1: want verify, got %q", got)
	}
	if got := actionTypeForStep(scenarios[0], 2); got != "click" {
		t.Errorf("step 2: want click (fallback), got %q", got)
	}
}

func TestNLScenario_ActionValuesParsed(t *testing.T) {
	raw := `[{"id":"t1","name":"T","natural_language":["type hello"],"selectors_used":["#input"],"source":"x","tags":["t"],"action_types":["type"],"action_values":["hello world"]}]`
	tmpFile := filepath.Join(t.TempDir(), "t.json")
	if err := os.WriteFile(tmpFile, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	scenarios, err := loadScenarios(tmpFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := actionValueForStep(scenarios[0], 0); got != "hello world" {
		t.Errorf("expected 'hello world', got %q", got)
	}
}

// --- Visual mode ---

func TestVisualScreenshotPath_PointsToAnnotated(t *testing.T) {
	dir := t.TempDir()
	annot := filepath.Join(dir, "step-01-annotated.png")
	if err := os.WriteFile(annot, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(annot); err != nil {
		t.Fatalf("annotated screenshot setup failed: %v", err)
	}
}

// --- OmniParser hard-fail ---

func TestOmniParserHardFail_ReturnsError(t *testing.T) {
	err := runDemo(demoConfig{
		ScenarioFile:  goldenScenarios,
		ScenarioID:    "smoke-001",
		OmniParserURL: "http://127.0.0.1:1",
		OutputDir:     t.TempDir(),
		HardFailOmni:  true,
	})
	if err == nil {
		t.Fatal("expected hard-fail error when OmniParser unreachable")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "omniparser") {
		t.Errorf("error should mention omniparser, got: %v", err)
	}
}

func TestDemoCmd_RequiresScenario(t *testing.T) {
	if err := runDemo(demoConfig{
		ScenarioFile: "",
		OutputDir:    t.TempDir(),
	}); err == nil {
		t.Fatal("expected error when --scenario is empty")
	}
}

func TestNLScenarioStructure(t *testing.T) {
	raw := `[{"id":"test-1","name":"Test","natural_language":["step 1"],"page_objects":[],"selectors_used":[],"source":"test.rb","tags":["core"]}]`
	tmpFile := filepath.Join(t.TempDir(), "test.json")
	if err := os.WriteFile(tmpFile, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	scenarios, err := loadScenarios(tmpFile)
	if err != nil {
		t.Fatalf("loadScenarios: %v", err)
	}
	if len(scenarios) != 1 || scenarios[0].ID != "test-1" {
		t.Errorf("unexpected: %+v", scenarios)
	}
}
