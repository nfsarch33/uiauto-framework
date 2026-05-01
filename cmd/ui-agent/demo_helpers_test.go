package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTallyResults_Empty(t *testing.T) {
	passed, total := tallyResults(nil)
	if passed != 0 || total != 0 {
		t.Errorf("got passed=%d total=%v", passed, total)
	}
}

func TestTallyResults_MixedPassFail(t *testing.T) {
	results := []DemoStepResult{
		{Status: "PASS", Latency: 100 * time.Millisecond},
		{Status: "FAIL", Latency: 250 * time.Millisecond},
		{Status: "PASS", Latency: 50 * time.Millisecond},
		{Status: "SKIP", Latency: 10 * time.Millisecond},
	}
	passed, total := tallyResults(results)
	if passed != 2 {
		t.Errorf("passed=%d", passed)
	}
	if total != 410*time.Millisecond {
		t.Errorf("total=%v", total)
	}
}

func TestBuildDemoSummary_HappyPath(t *testing.T) {
	scenario := NLScenario{ID: "sc-1", Name: "Smoke", Source: "frontend"}
	results := []DemoStepResult{
		{StepIndex: 1, Status: "PASS", Tier: "light", Latency: 100 * time.Millisecond},
		{StepIndex: 2, Status: "FAIL", Tier: "smart", Latency: 200 * time.Millisecond},
	}
	tierCounts := map[string]int{"light": 1, "smart": 1}
	healPath := map[string]int{"fingerprint": 1}

	summary := buildDemoSummary(scenario, results, tierCounts, healPath, "2026-01-01T00:00:00Z", "2026-01-01T00:00:01Z", 1, 300*time.Millisecond)

	if summary.ScenarioID != "sc-1" {
		t.Errorf("id=%q", summary.ScenarioID)
	}
	if summary.TotalSteps != 2 || summary.PassedSteps != 1 || summary.FailedSteps != 1 {
		t.Errorf("counts: total=%d passed=%d failed=%d", summary.TotalSteps, summary.PassedSteps, summary.FailedSteps)
	}
	if summary.AvgLatencyMs != 150 {
		t.Errorf("avg=%d", summary.AvgLatencyMs)
	}
	if summary.TotalLatencyMs != 300 {
		t.Errorf("total=%d", summary.TotalLatencyMs)
	}
	if summary.Source != "frontend" {
		t.Errorf("source=%q", summary.Source)
	}
	if len(summary.TierBreakdown) != 2 || summary.TierBreakdown["smart"] != 1 {
		t.Errorf("tier=%v", summary.TierBreakdown)
	}
}

// Empty results → AvgLatencyMs is 0 (safe-divide branch).
func TestBuildDemoSummary_EmptyResultsAvoidsDivByZero(t *testing.T) {
	summary := buildDemoSummary(NLScenario{ID: "x"}, nil, nil, nil, "", "", 0, 0)
	if summary.AvgLatencyMs != 0 {
		t.Errorf("avg should be 0, got %d", summary.AvgLatencyMs)
	}
	if summary.TotalSteps != 0 {
		t.Errorf("total=%d", summary.TotalSteps)
	}
}

func TestWriteJSON_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	payload := map[string]string{"a": "1"}
	if err := writeJSON(path, payload); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]string
	if err := json.Unmarshal(got, &back); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if back["a"] != "1" {
		t.Errorf("back=%v", back)
	}
}

// writeJSON returns the underlying os.WriteFile error for unwritable paths.
func TestWriteJSON_BadPath(t *testing.T) {
	err := writeJSON("/dev/null/cannot-write", map[string]string{"x": "y"})
	if err == nil {
		t.Error("expected error for unwritable path")
	}
}

// writeJSON propagates marshal errors when the value cannot be encoded.
type unmarshalable struct{ C chan int }

func TestWriteJSON_MarshalError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	err := writeJSON(path, unmarshalable{C: make(chan int)})
	if err == nil {
		t.Error("expected marshal error for chan field")
	}
	if err != nil && !errors.Is(err, err) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestResolveLLMTarget_GatewayWins(t *testing.T) {
	url, model := resolveLLMTarget("http://ollama", "smart", "http://gateway", "gw-model", "")
	if url != "http://gateway" || model != "gw-model" {
		t.Errorf("got url=%q model=%q", url, model)
	}
}

func TestResolveLLMTarget_EnvFallback(t *testing.T) {
	url, model := resolveLLMTarget("http://ollama", "smart", "", "gw-model", "http://env-url")
	if url != "http://env-url" || model != "gw-model" {
		t.Errorf("got url=%q model=%q", url, model)
	}
}

func TestResolveLLMTarget_DefaultsToOllama(t *testing.T) {
	url, model := resolveLLMTarget("http://ollama", "smart", "", "gw-model", "")
	if url != "http://ollama" || model != "smart" {
		t.Errorf("got url=%q model=%q", url, model)
	}
}

// actionValueForStep when ActionValues is shorter than NaturalLanguage returns "".
func TestActionValueForStep_OutOfRange(t *testing.T) {
	s := NLScenario{
		NaturalLanguage: []string{"a", "b", "c"},
		ActionValues:    []string{"x"},
	}
	if got := actionValueForStep(s, 0); got != "x" {
		t.Errorf("idx 0: %q", got)
	}
	if got := actionValueForStep(s, 1); got != "" {
		t.Errorf("idx 1: %q", got)
	}
	if got := actionValueForStep(s, -1); got != "" {
		t.Errorf("idx -1: %q", got)
	}
}
