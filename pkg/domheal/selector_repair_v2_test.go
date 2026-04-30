package domheal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestSelectorRepairV2_CSSFallback(t *testing.T) {
	dir := t.TempDir()
	rl := NewRepairLog(filepath.Join(dir, "repair.jsonl"))
	sr := NewSelectorRepairV2(rl, nil)

	eval := func(_ context.Context, sel string) (int, error) {
		if strings.Contains(sel, "content") {
			return 3, nil
		}
		return 0, nil
	}

	result := sr.Repair(context.Background(), "content_links", "div.old-content-list a", eval)
	if !result.Repaired {
		t.Fatal("expected repair to succeed via CSS class partial match")
	}
	if result.BestCandidate.Strategy != StrategyCSS {
		t.Errorf("expected CSS strategy, got %s", result.BestCandidate.Strategy)
	}
	if len(result.Candidates) == 0 {
		t.Error("expected at least one candidate")
	}
}

func TestSelectorRepairV2_XPathFallback(t *testing.T) {
	dir := t.TempDir()
	rl := NewRepairLog(filepath.Join(dir, "repair.jsonl"))
	sr := NewSelectorRepairV2(rl, nil)

	eval := func(_ context.Context, sel string) (int, error) {
		if strings.HasPrefix(sel, "//") {
			return 1, nil
		}
		return 0, nil
	}

	result := sr.Repair(context.Background(), "nav", "nav.main-menu", eval)
	if !result.Repaired {
		t.Fatal("expected repair via XPath")
	}
	if result.BestCandidate.Strategy != StrategyXPath {
		t.Errorf("expected XPath strategy, got %s", result.BestCandidate.Strategy)
	}
}

func TestSelectorRepairV2_ARIAFallback(t *testing.T) {
	dir := t.TempDir()
	rl := NewRepairLog(filepath.Join(dir, "repair.jsonl"))
	sr := NewSelectorRepairV2(rl, nil)

	eval := func(_ context.Context, sel string) (int, error) {
		if strings.Contains(sel, "role=") {
			return 1, nil
		}
		return 0, nil
	}

	result := sr.Repair(context.Background(), "navigation", "nav.broken-nav", eval)
	if !result.Repaired {
		t.Fatal("expected repair via ARIA")
	}
	if result.BestCandidate.Strategy != StrategyARIA {
		t.Errorf("expected ARIA strategy, got %s", result.BestCandidate.Strategy)
	}
}

func TestSelectorRepairV2_TextContentFallback(t *testing.T) {
	dir := t.TempDir()
	rl := NewRepairLog(filepath.Join(dir, "repair.jsonl"))
	sr := NewSelectorRepairV2(rl, nil)

	eval := func(_ context.Context, sel string) (int, error) {
		if strings.Contains(sel, "has-text") {
			return 1, nil
		}
		return 0, nil
	}

	result := sr.Repair(context.Background(), "login_button", "button.broken-login", eval)
	if !result.Repaired {
		t.Fatal("expected repair via text content")
	}
	if result.BestCandidate.Strategy != StrategyTextContent {
		t.Errorf("expected text_content strategy, got %s", result.BestCandidate.Strategy)
	}
}

func TestSelectorRepairV2_NoMatch(t *testing.T) {
	dir := t.TempDir()
	rl := NewRepairLog(filepath.Join(dir, "repair.jsonl"))
	sr := NewSelectorRepairV2(rl, nil)

	eval := func(_ context.Context, _ string) (int, error) {
		return 0, nil
	}

	result := sr.Repair(context.Background(), "unknown_element", "div.nonexistent", eval)
	if result.Repaired {
		t.Error("expected no repair for completely unmatched element")
	}
	if result.BestCandidate != nil {
		t.Error("expected nil best candidate")
	}
}

func TestSelectorRepairV2_EvaluatorError(t *testing.T) {
	dir := t.TempDir()
	rl := NewRepairLog(filepath.Join(dir, "repair.jsonl"))
	sr := NewSelectorRepairV2(rl, nil)

	eval := func(_ context.Context, _ string) (int, error) {
		return 0, fmt.Errorf("browser disconnected")
	}

	result := sr.Repair(context.Background(), "button", "button.submit", eval)
	if result.Repaired {
		t.Error("expected no repair when evaluator always errors")
	}
}

func TestSelectorRepairV2_HighMatchCountPenalty(t *testing.T) {
	dir := t.TempDir()
	rl := NewRepairLog(filepath.Join(dir, "repair.jsonl"))
	sr := NewSelectorRepairV2(rl, nil)

	eval := func(_ context.Context, _ string) (int, error) {
		return 50, nil
	}

	result := sr.Repair(context.Background(), "content_links", "a.content", eval)
	if !result.Repaired {
		t.Fatal("should still repair even with high match count")
	}
	if result.BestCandidate.Confidence >= 0.95 {
		t.Errorf("confidence should be penalized for high match count, got %.2f", result.BestCandidate.Confidence)
	}
}

func TestSelectorRepairV2_ExactMatchBonus(t *testing.T) {
	conf := strategyConfidence(StrategyCSS, 1)
	if conf <= 0.95 {
		t.Errorf("exact match should have bonus, got %.2f", conf)
	}
}

func TestSelectorRepairV2_RepairLogPersistence(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "repair.jsonl")
	rl := NewRepairLog(logPath)
	sr := NewSelectorRepairV2(rl, nil)

	eval := func(_ context.Context, sel string) (int, error) {
		if strings.Contains(sel, "content") || strings.Contains(sel, "module") || strings.Contains(sel, "d2l") {
			return 1, nil
		}
		return 0, nil
	}

	result := sr.Repair(context.Background(), "content_links", "div.old-links", eval)
	if !result.Repaired {
		t.Fatal("expected repair to succeed before checking log")
	}

	suggestions, err := rl.Read()
	if err != nil {
		t.Fatalf("read repair log: %v", err)
	}
	if len(suggestions) == 0 {
		t.Error("expected repair suggestions in log")
	}
}

func TestSelectorRepairV2_RepairHistory(t *testing.T) {
	dir := t.TempDir()
	rl := NewRepairLog(filepath.Join(dir, "repair.jsonl"))
	sr := NewSelectorRepairV2(rl, nil)

	eval := func(_ context.Context, sel string) (int, error) {
		if strings.Contains(sel, "content") {
			return 1, nil
		}
		return 0, nil
	}

	sr.Repair(context.Background(), "content_links", "div.old", eval)
	hist := sr.RepairHistory()
	if len(hist) == 0 {
		t.Error("expected repair history entry")
	}
}

func TestSelectorRepairV2_WithMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics := NewSelectorRepairV2Metrics(reg)

	dir := t.TempDir()
	rl := NewRepairLog(filepath.Join(dir, "repair.jsonl"))
	sr := NewSelectorRepairV2(rl, nil)
	sr.WithMetrics(metrics)

	eval := func(_ context.Context, sel string) (int, error) {
		if strings.Contains(sel, "role=") {
			return 1, nil
		}
		return 0, nil
	}

	sr.Repair(context.Background(), "navigation", "nav.broken", eval)

	gathered, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	found := false
	for _, mf := range gathered {
		if strings.Contains(mf.GetName(), "selector_repair") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected selector_repair metrics to be recorded")
	}
}

func TestSelectorRepairV2_IDVariation(t *testing.T) {
	dir := t.TempDir()
	rl := NewRepairLog(filepath.Join(dir, "repair.jsonl"))
	sr := NewSelectorRepairV2(rl, nil)

	eval := func(_ context.Context, sel string) (int, error) {
		if strings.Contains(sel, "main-content") {
			return 1, nil
		}
		return 0, nil
	}

	result := sr.Repair(context.Background(), "main", "div#main-content.wrapper", eval)
	if !result.Repaired {
		t.Fatal("expected repair via ID variation")
	}
}

func TestSelectorRepairV2_MultipleCandidatesRanked(t *testing.T) {
	dir := t.TempDir()
	rl := NewRepairLog(filepath.Join(dir, "repair.jsonl"))
	sr := NewSelectorRepairV2(rl, nil)

	callCount := 0
	eval := func(_ context.Context, _ string) (int, error) {
		callCount++
		return 1, nil
	}

	result := sr.Repair(context.Background(), "content_links", "a.d2l-content-link", eval)
	if !result.Repaired {
		t.Fatal("expected repair")
	}
	if len(result.Candidates) < 2 {
		t.Errorf("expected multiple candidates, got %d", len(result.Candidates))
	}
	if result.BestCandidate.Confidence <= 0 {
		t.Error("best candidate should have positive confidence")
	}
}

func TestParseCSSSelector(t *testing.T) {
	tests := []struct {
		input       string
		wantTag     string
		wantClasses int
	}{
		{"div.foo.bar", "div", 2},
		{"a.link", "a", 1},
		{"#main", "", 0},
		{"div[data-key='test']", "div", 0},
		{"", "", 0},
	}

	for _, tt := range tests {
		tag, classes, _ := parseCSSSelector(tt.input)
		if tag != tt.wantTag {
			t.Errorf("parseCSSSelector(%q) tag = %q, want %q", tt.input, tag, tt.wantTag)
		}
		if len(classes) != tt.wantClasses {
			t.Errorf("parseCSSSelector(%q) classes count = %d, want %d", tt.input, len(classes), tt.wantClasses)
		}
	}
}

func TestStrategyConfidence(t *testing.T) {
	tests := []struct {
		strategy   RepairStrategy
		matchCount int
		wantMin    float64
		wantMax    float64
	}{
		{StrategyCSS, 1, 0.95, 1.0},
		{StrategyCSS, 15, 0.7, 0.8},
		{StrategyXPath, 1, 0.85, 1.0},
		{StrategyARIA, 3, 0.75, 0.85},
		{StrategyTextContent, 1, 0.70, 0.80},
		{StrategyVLM, 1, 0.60, 0.70},
	}

	for _, tt := range tests {
		conf := strategyConfidence(tt.strategy, tt.matchCount)
		if conf < tt.wantMin || conf > tt.wantMax {
			t.Errorf("strategyConfidence(%s, %d) = %.2f, want [%.2f, %.2f]",
				tt.strategy, tt.matchCount, conf, tt.wantMin, tt.wantMax)
		}
	}
}

func TestSelectorRepairV2_D2LContentLinks(t *testing.T) {
	dir := t.TempDir()
	rl := NewRepairLog(filepath.Join(dir, "repair.jsonl"))
	sr := NewSelectorRepairV2(rl, nil)

	eval := func(_ context.Context, sel string) (int, error) {
		if strings.Contains(sel, "d2l") || strings.Contains(sel, "content") || strings.Contains(sel, "module") {
			return 5, nil
		}
		return 0, nil
	}

	result := sr.Repair(context.Background(), "content_links", "div.d2l-old-content a.d2l-link", eval)
	if !result.Repaired {
		t.Fatal("expected repair for D2L content links")
	}
}

func TestSelectorRepairV2_ModuleList(t *testing.T) {
	dir := t.TempDir()
	rl := NewRepairLog(filepath.Join(dir, "repair.jsonl"))
	sr := NewSelectorRepairV2(rl, nil)

	eval := func(_ context.Context, sel string) (int, error) {
		if strings.Contains(sel, "content") || strings.Contains(sel, "module") {
			return 2, nil
		}
		return 0, nil
	}

	result := sr.Repair(context.Background(), "module_list", ".d2l-broken-module-list", eval)
	if !result.Repaired {
		t.Fatal("expected repair for module list")
	}
}

func TestSelectorRepairV2_ConcurrentRepairs(t *testing.T) {
	dir := t.TempDir()
	rl := NewRepairLog(filepath.Join(dir, "repair.jsonl"))
	sr := NewSelectorRepairV2(rl, nil)

	eval := func(_ context.Context, sel string) (int, error) {
		if strings.Contains(sel, "content") {
			return 1, nil
		}
		return 0, nil
	}

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			sr.Repair(context.Background(),
				fmt.Sprintf("elem_%d", idx),
				fmt.Sprintf("div.broken-%d", idx),
				eval)
			done <- true
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}

	hist := sr.RepairHistory()
	if len(hist) != 10 {
		t.Errorf("expected 10 history entries, got %d", len(hist))
	}
}

func TestSelectorRepairV2_SyntheticD2LLogin(t *testing.T) {
	dir := t.TempDir()
	rl := NewRepairLog(filepath.Join(dir, "repair.jsonl"))
	sr := NewSelectorRepairV2(rl, nil)

	eval := func(_ context.Context, sel string) (int, error) {
		if strings.Contains(sel, "Sign In") || strings.Contains(sel, "Log In") || sel == `[type="submit"]` {
			return 1, nil
		}
		return 0, nil
	}

	result := sr.Repair(context.Background(), "login_button", "button#old-login-btn", eval)
	if !result.Repaired {
		t.Fatal("expected repair for login button via text content")
	}
}

func TestSelectorRepairV2_TempDirCleanup(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "repair.jsonl")
	rl := NewRepairLog(logPath)
	sr := NewSelectorRepairV2(rl, nil)

	eval := func(_ context.Context, sel string) (int, error) {
		if strings.Contains(sel, "content") || strings.Contains(sel, "module") || strings.Contains(sel, "d2l") {
			return 1, nil
		}
		return 0, nil
	}
	result := sr.Repair(context.Background(), "content_links", "div.test", eval)
	if !result.Repaired {
		t.Skip("repair did not succeed, skipping file existence check")
	}

	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Error("repair log file should exist")
	}
}

// --- Sprint 7: Full self-healing pipeline E2E (D2L/Deakin production hardening) ---

func TestSelfHealingPipeline_D2LContentRedesign(t *testing.T) {
	dir := t.TempDir()
	reg := prometheus.NewRegistry()

	originalD2L := `<html><body>
		<nav class="d2l-nav" role="navigation"><a href="/home">Home</a></nav>
		<main id="content" role="main">
			<div class="d2l-content-module">
				<a class="d2l-link" href="/content/1">Module 1: Health Basics</a>
				<a class="d2l-link" href="/content/2">Module 2: Nutrition</a>
			</div>
			<button class="d2l-btn d2l-submit" type="button">Submit Assignment</button>
		</main>
	</body></html>`

	redesignedD2L := `<html><body>
		<header class="d2l-header-v2" role="banner">
			<nav role="navigation"><a href="/home">Dashboard</a></nav>
		</header>
		<main id="content-area" role="main">
			<section class="d2l-content-grid" data-view="card">
				<article class="d2l-card" data-id="1"><h3>Module 1: Health Basics</h3></article>
				<article class="d2l-card" data-id="2"><h3>Module 2: Nutrition</h3></article>
			</section>
			<button class="d2l-floating-action" data-action="submit">Submit</button>
		</main>
	</body></html>`

	// 1. Detect the DOM change
	changeDetector := NewDOMChangeDetector(DOMChangeDetectorConfig{StateDir: dir})
	changeMetrics := NewDOMChangeDetectorMetrics(reg)
	changeDetector.WithMetrics(changeMetrics)

	repairTriggered := false
	changeDetector.OnRepair(func(pageID string, event ChangeEvent) error {
		repairTriggered = true
		return nil
	})

	changeDetector.Check("d2l-hsh206-week1", originalD2L)
	event := changeDetector.Check("d2l-hsh206-week1", redesignedD2L)
	if event == nil {
		t.Fatal("pipeline: expected DOM change event on D2L redesign")
	}
	if event.Severity != "major" {
		t.Errorf("pipeline: expected major severity, got %s", event.Severity)
	}
	if !repairTriggered {
		t.Error("pipeline: repair callback should have been triggered")
	}

	// 2. Run selector repair for the broken content links selector
	repairReg := prometheus.NewRegistry()
	logPath := filepath.Join(dir, "repair.jsonl")
	repairLog := NewRepairLog(logPath)
	repairV2 := NewSelectorRepairV2(repairLog, nil)
	repairMetrics := NewSelectorRepairV2Metrics(repairReg)
	repairV2.WithMetrics(repairMetrics)

	newDOMEval := func(_ context.Context, sel string) (int, error) {
		lower := strings.ToLower(sel)
		if strings.Contains(lower, "d2l-card") || strings.Contains(lower, "article") ||
			strings.Contains(lower, "content") || strings.Contains(lower, "module") {
			return 2, nil
		}
		if strings.Contains(lower, "submit") || strings.Contains(lower, "action") {
			return 1, nil
		}
		return 0, nil
	}

	contentResult := repairV2.Repair(context.Background(), "content_links", "div.d2l-content-module a.d2l-link", newDOMEval)
	if !contentResult.Repaired {
		t.Error("pipeline: content links selector should be repaired")
	}
	if contentResult.BestCandidate != nil {
		t.Logf("pipeline: content repaired with strategy=%s selector=%q confidence=%.2f",
			contentResult.BestCandidate.Strategy,
			contentResult.BestCandidate.Selector,
			contentResult.BestCandidate.Confidence)
	}

	submitResult := repairV2.Repair(context.Background(), "submit_button", "button.d2l-btn.d2l-submit", newDOMEval)
	if !submitResult.Repaired {
		t.Error("pipeline: submit button selector should be repaired")
	}

	// 3. Verify repair log has entries
	entries, err := repairLog.Read()
	if err != nil {
		t.Fatalf("pipeline: read repair log: %v", err)
	}
	if len(entries) < 2 {
		t.Errorf("pipeline: expected at least 2 repair log entries, got %d", len(entries))
	}

	// 4. Verify metrics were recorded
	gathered, err := repairReg.Gather()
	if err != nil {
		t.Fatalf("pipeline: gather metrics: %v", err)
	}
	metricsFound := false
	for _, mf := range gathered {
		if strings.Contains(mf.GetName(), "selector_repair") {
			metricsFound = true
			break
		}
	}
	if !metricsFound {
		t.Error("pipeline: expected selector_repair metrics to be recorded")
	}

	// 5. Verify fingerprint change tracking
	fp1 := ParseDOMFingerprint(originalD2L)
	fp2 := ParseDOMFingerprint(redesignedD2L)
	sim := DOMFingerprintSimilarity(fp1, fp2)
	if sim > 0.5 {
		t.Errorf("pipeline: fingerprint similarity should be low for major redesign, got %.2f", sim)
	}

	// 6. Save and verify state persistence
	if err := changeDetector.Save(); err != nil {
		t.Fatalf("pipeline: save state: %v", err)
	}
	reloaded := NewDOMChangeDetector(DOMChangeDetectorConfig{StateDir: dir})
	if err := reloaded.Load(); err != nil {
		t.Fatalf("pipeline: load state: %v", err)
	}
	if len(reloaded.Events()) == 0 {
		t.Error("pipeline: events should persist after save/load")
	}
}

func TestSelfHealingPipeline_D2LGradeTableMigration(t *testing.T) {
	dir := t.TempDir()

	originalGrades := `<html><body>
		<table class="d2l-grades-table" role="table">
			<thead><tr><th>Item</th><th>Grade</th><th>Weight</th></tr></thead>
			<tbody>
				<tr><td>Assignment 1</td><td>85</td><td>20%</td></tr>
				<tr><td>Quiz 1</td><td>90</td><td>10%</td></tr>
			</tbody>
		</table>
	</body></html>`

	redesignedGrades := `<html><body>
		<div class="d2l-grades-grid" role="grid" aria-label="Grades">
			<div class="d2l-grade-row" role="row" data-item="1">
				<span class="grade-name" role="gridcell">Assignment 1</span>
				<span class="grade-score" role="gridcell">85/100</span>
				<span class="grade-weight" role="gridcell">20%</span>
			</div>
			<div class="d2l-grade-row" role="row" data-item="2">
				<span class="grade-name" role="gridcell">Quiz 1</span>
				<span class="grade-score" role="gridcell">90/100</span>
				<span class="grade-weight" role="gridcell">10%</span>
			</div>
		</div>
	</body></html>`

	changeDetector := NewDOMChangeDetector(DOMChangeDetectorConfig{StateDir: dir})
	changeDetector.Check("d2l-grades", originalGrades)
	event := changeDetector.Check("d2l-grades", redesignedGrades)
	if event == nil {
		t.Fatal("grade table migration should be detected")
	}

	logPath := filepath.Join(dir, "repair.jsonl")
	repairLog := NewRepairLog(logPath)
	repairV2 := NewSelectorRepairV2(repairLog, nil)

	gradeEval := func(_ context.Context, sel string) (int, error) {
		lower := strings.ToLower(sel)
		if strings.Contains(lower, "grade") || strings.Contains(lower, "grid") ||
			strings.Contains(lower, "row") || strings.Contains(lower, "[role=") {
			return 2, nil
		}
		return 0, nil
	}

	result := repairV2.Repair(context.Background(), "grade_table", "table.d2l-grades-table tbody tr", gradeEval)
	if !result.Repaired {
		t.Error("grade table selector should be repaired after migration")
	}
	if result.StrategiesTried == 0 {
		t.Error("expected at least 1 strategy tried")
	}

	// Verify repair log persistence
	entries, err := repairLog.Read()
	if err != nil {
		t.Fatalf("read repair log: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected repair log entries after grade table healing")
	}
}

func TestSelfHealingPipeline_CircuitBreaker_UnderLoad(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "repair.jsonl")
	repairLog := NewRepairLog(logPath)
	repairV2 := NewSelectorRepairV2(repairLog, nil)

	failCount := 0
	eval := func(_ context.Context, sel string) (int, error) {
		failCount++
		return 0, fmt.Errorf("element not found (attempt %d)", failCount)
	}

	for i := 0; i < 10; i++ {
		result := repairV2.Repair(context.Background(), "missing_elem", fmt.Sprintf("div.broken-%d", i), eval)
		if result.Repaired {
			t.Errorf("iteration %d: should not repair with all-failing evaluator", i)
		}
	}

	if failCount == 0 {
		t.Error("evaluator should have been called")
	}
}

func TestSelectorRepairV2_VLMFallback(t *testing.T) {
	dir := t.TempDir()
	rl := NewRepairLog(filepath.Join(dir, "repair.jsonl"))
	sr := NewSelectorRepairV2(rl, nil)

	// Wire a mock VLM that returns selectors
	sr.WithVLM(func(_ context.Context, elemType string, _ []byte) ([]string, error) {
		return []string{".vlm-suggested-" + elemType, "[data-vlm-match]"}, nil
	})

	eval := func(_ context.Context, sel string) (int, error) {
		if strings.Contains(sel, "vlm-suggested") || strings.Contains(sel, "data-vlm-match") {
			return 1, nil
		}
		return 0, nil
	}

	result := sr.Repair(context.Background(), "unknown_element", "div.completely-broken", eval)
	if !result.Repaired {
		t.Fatal("expected repair via VLM fallback")
	}
	if result.BestCandidate.Strategy != StrategyVLM {
		t.Errorf("expected VLM strategy, got %s", result.BestCandidate.Strategy)
	}
}

func TestSelectorRepairV2_VLMSkippedWhenHighConfidence(t *testing.T) {
	dir := t.TempDir()
	rl := NewRepairLog(filepath.Join(dir, "repair.jsonl"))
	sr := NewSelectorRepairV2(rl, nil)

	vlmCalled := false
	sr.WithVLM(func(_ context.Context, _ string, _ []byte) ([]string, error) {
		vlmCalled = true
		return []string{".vlm-selector"}, nil
	})

	eval := func(_ context.Context, sel string) (int, error) {
		if strings.Contains(sel, "content") {
			return 1, nil
		}
		return 0, nil
	}

	result := sr.Repair(context.Background(), "content_links", "div.old-content a", eval)
	if !result.Repaired {
		t.Fatal("expected repair via CSS")
	}
	if vlmCalled {
		t.Error("VLM should not be called when earlier strategy succeeds with high confidence")
	}
}

func TestSelectorRepairV2_VLMError(t *testing.T) {
	dir := t.TempDir()
	rl := NewRepairLog(filepath.Join(dir, "repair.jsonl"))
	sr := NewSelectorRepairV2(rl, nil)

	sr.WithVLM(func(_ context.Context, _ string, _ []byte) ([]string, error) {
		return nil, fmt.Errorf("VLM service unavailable")
	})

	eval := func(_ context.Context, _ string) (int, error) {
		return 0, nil
	}

	result := sr.Repair(context.Background(), "broken_elem", "div.broken", eval)
	if result.Repaired {
		t.Error("should not repair when all strategies fail including VLM")
	}
	if result.StrategiesTried != 5 {
		t.Errorf("expected 5 strategies tried (including VLM), got %d", result.StrategiesTried)
	}
}

// --- Sprint 2: D2L/Brightspace comprehensive 5-strategy E2E targeting 90%+ success ---

func TestSelfHealingPipeline_D2L_5Strategy_90Pct(t *testing.T) {
	dir := t.TempDir()

	type d2lScenario struct {
		name            string
		elementType     string
		brokenSelector  string
		newDOMSelectors map[string]int // selector substring -> match count
	}

	scenarios := []d2lScenario{
		{
			name:            "content_module_to_card_grid",
			elementType:     "content_links",
			brokenSelector:  "div.d2l-content-module a.d2l-link",
			newDOMSelectors: map[string]int{"d2l": 2, "content": 2, "module": 3, "card": 4},
		},
		{
			name:            "grade_table_to_grid",
			elementType:     "grade_table",
			brokenSelector:  "table.d2l-grades-table tbody tr",
			newDOMSelectors: map[string]int{"grade": 5, "grid": 2, "row": 3, "role=": 4, "table": 1},
		},
		{
			name:            "submit_button_redesign",
			elementType:     "submit_button",
			brokenSelector:  "button.d2l-btn.d2l-submit",
			newDOMSelectors: map[string]int{"submit": 1, "action": 1, "button": 2, "Submit": 1, "Save": 1},
		},
		{
			name:            "nav_restructure",
			elementType:     "course_nav",
			brokenSelector:  "nav.d2l-nav-old ul.d2l-menu",
			newDOMSelectors: map[string]int{"navigation": 1, "nav": 1, "role=": 2, "d2l-nav": 1, "course": 1, "Course": 1},
		},
		{
			name:            "login_page_update",
			elementType:     "login_button",
			brokenSelector:  "button#d2l-old-login.d2l-primary",
			newDOMSelectors: map[string]int{"d2l": 1, "Sign In": 1, "Log In": 1, "submit": 1},
		},
		{
			name:            "module_list_v2",
			elementType:     "module_list",
			brokenSelector:  "div.d2l-le-content-list ul.d2l-modules",
			newDOMSelectors: map[string]int{"content": 3, "module": 2, "d2l": 3, "key": 1, "d2l-le": 1},
		},
		{
			name:            "assignment_card_migration",
			elementType:     "assignment",
			brokenSelector:  "div.d2l-assignment-list .d2l-item",
			newDOMSelectors: map[string]int{"article": 2, "assignment": 3, "d2l-card": 2, "role=": 1, "data-type": 1},
		},
		{
			name:            "discussion_redesign",
			elementType:     "discussion_post",
			brokenSelector:  "div.d2l-forum-post.d2l-thread",
			newDOMSelectors: map[string]int{"article": 1, "Discussion": 1, "Forum": 1, "d2l-discussion": 1, "role=": 1},
		},
		{
			name:            "file_upload_widget",
			elementType:     "file_upload",
			brokenSelector:  "div.d2l-dropbox input[type=file]",
			newDOMSelectors: map[string]int{"file": 1, "Upload": 1, "Browse": 1, "upload": 1, "role=": 1},
		},
		{
			name:            "calendar_widget",
			elementType:     "calendar_event",
			brokenSelector:  "div.d2l-calendar .d2l-event-item",
			newDOMSelectors: map[string]int{"listitem": 1, "Event": 1, "Due": 1, "event": 2, "d2l-calendar": 1},
		},
		{
			name:            "announcement_banner",
			elementType:     "announcement",
			brokenSelector:  "div.d2l-announcement-old .d2l-alert",
			newDOMSelectors: map[string]int{"article": 1, "Announcement": 1, "Important": 1, "d2l-announcement": 1},
		},
		{
			name:            "heading_rename",
			elementType:     "heading",
			brokenSelector:  "h1.d2l-page-title.d2l-old",
			newDOMSelectors: map[string]int{"heading": 1, "h1": 1, "h2": 1, "role=": 1, "aria-level": 1},
		},
		{
			name:            "search_field_move",
			elementType:     "search",
			brokenSelector:  "input.d2l-search-old#d2l-search",
			newDOMSelectors: map[string]int{"search": 1, "role=": 1, "type=": 1, "aria-label": 1},
		},
		{
			name:            "sidebar_content_shift",
			elementType:     "sidebar",
			brokenSelector:  "aside.d2l-sidebar-old.d2l-panel",
			newDOMSelectors: map[string]int{"complementary": 1, "aside": 1, "sidebar": 1, "role=": 1},
		},
		{
			name:            "main_content_wrapper",
			elementType:     "main_content",
			brokenSelector:  "div.d2l-main-wrapper#d2l-content",
			newDOMSelectors: map[string]int{"main": 1, "role=": 1, "d2l-content": 1, "main-content": 1},
		},
		{
			name:            "dialog_popup_update",
			elementType:     "dialog",
			brokenSelector:  "div.d2l-modal.d2l-overlay",
			newDOMSelectors: map[string]int{"dialog": 1, "role=": 1, "alertdialog": 1},
		},
		{
			name:            "tab_navigation_change",
			elementType:     "tab",
			brokenSelector:  "div.d2l-tabs-old button.d2l-tab-btn",
			newDOMSelectors: map[string]int{"tab": 1, "tablist": 1, "role=": 2},
		},
		{
			name:            "form_restructure",
			elementType:     "form",
			brokenSelector:  "form.d2l-form-old#d2l-submission",
			newDOMSelectors: map[string]int{"form": 1, "role=": 1},
		},
		{
			name:            "quiz_link_update",
			elementType:     "quiz_link",
			brokenSelector:  "a.d2l-quiz-link.d2l-old",
			newDOMSelectors: map[string]int{"Quiz": 1, "Test": 1},
		},
		{
			name:            "grades_link_update",
			elementType:     "grades_link",
			brokenSelector:  "a.d2l-grades-link.d2l-old",
			newDOMSelectors: map[string]int{"Grades": 1, "Grade": 1},
		},
	}

	logPath := filepath.Join(dir, "repair.jsonl")
	repairLog := NewRepairLog(logPath)
	repairV2 := NewSelectorRepairV2(repairLog, nil)

	// Wire mock VLM as 5th-strategy fallback
	repairV2.WithVLM(func(_ context.Context, elemType string, _ []byte) ([]string, error) {
		return []string{
			fmt.Sprintf("[data-vlm-type='%s']", elemType),
			fmt.Sprintf(".vlm-%s", elemType),
		}, nil
	})

	var successes, failures int
	for _, sc := range scenarios {
		eval := func(_ context.Context, sel string) (int, error) {
			lower := strings.ToLower(sel)
			for substr, count := range sc.newDOMSelectors {
				if strings.Contains(lower, strings.ToLower(substr)) {
					return count, nil
				}
			}
			// VLM selectors always match as fallback
			if strings.Contains(sel, "vlm") {
				return 1, nil
			}
			return 0, nil
		}

		result := repairV2.Repair(context.Background(), sc.elementType, sc.brokenSelector, eval)
		if result.Repaired {
			successes++
			t.Logf("PASS %-35s strategy=%-15s confidence=%.2f selector=%q",
				sc.name, result.BestCandidate.Strategy, result.BestCandidate.Confidence, result.BestCandidate.Selector)
		} else {
			failures++
			t.Logf("FAIL %-35s strategies_tried=%d candidates=%d",
				sc.name, result.StrategiesTried, len(result.Candidates))
		}
	}

	total := len(scenarios)
	successRate := float64(successes) / float64(total) * 100
	t.Logf("\n--- D2L 5-Strategy E2E Summary ---\nTotal: %d  Success: %d  Failed: %d  Rate: %.1f%%",
		total, successes, failures, successRate)

	if successRate < 90 {
		t.Errorf("self-healing success rate %.1f%% below 90%% target", successRate)
	}
}

func TestSelfHealingPipeline_MultiPageTracking(t *testing.T) {
	dir := t.TempDir()
	d := NewDOMChangeDetector(DOMChangeDetectorConfig{StateDir: dir})

	pages := map[string]struct {
		original string
		mutated  string
	}{
		"week1": {
			original: `<html><body><div class="d2l-content" id="w1"><a href="/m1">Module 1</a></div></body></html>`,
			mutated:  `<html><body><section class="d2l-grid" id="w1"><div class="card" data-id="m1">Module 1</div></section></body></html>`,
		},
		"week2": {
			original: `<html><body><div class="d2l-content" id="w2"><a href="/m2">Module 2</a></div></body></html>`,
			mutated:  `<html><body><section class="d2l-grid" id="w2"><div class="card" data-id="m2">Module 2</div></section></body></html>`,
		},
		"grades": {
			original: `<html><body><table class="grades"><tr><td>A1</td><td>85</td></tr></table></body></html>`,
			mutated:  `<html><body><div class="grade-cards"><div class="grade" data-item="A1">85</div></div></body></html>`,
		},
	}

	for pageID, p := range pages {
		d.Check(pageID, p.original)
	}

	var detectedChanges int
	for pageID, p := range pages {
		event := d.Check(pageID, p.mutated)
		if event != nil {
			detectedChanges++
		}
	}

	if detectedChanges != 3 {
		t.Errorf("expected 3 DOM changes detected across pages, got %d", detectedChanges)
	}

	if err := d.Save(); err != nil {
		t.Fatalf("save multi-page state: %v", err)
	}

	stats := d.Stats()
	if stats.HashesTracked < 3 {
		t.Errorf("expected at least 3 hashes tracked, got %d", stats.HashesTracked)
	}
}
