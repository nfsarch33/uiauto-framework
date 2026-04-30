package uiauto

import (
	"context"
	"fmt"
	"testing"

	"github.com/nfsarch33/uiauto-framework/pkg/llm"
	"github.com/prometheus/client_golang/prometheus"
)

func TestVLMJudgeEvaluator_PerfectAccuracy(t *testing.T) {
	mock := &MockVLMProvider{
		Response: `{"present": true, "confidence": 0.95, "explanation": "element found", "suggested_selector": "#btn"}`,
	}
	bridge := NewVLMBridge(mock, []string{"qwen3-vl"})
	eval := NewVLMJudgeEvaluator(bridge, 0.6, nil)

	cases := []JudgeTestCase{
		{ID: "c1", Description: "login button", Screenshot: []byte("fake"), Expected: true},
		{ID: "c2", Description: "submit form", Screenshot: []byte("fake"), Expected: true},
		{ID: "c3", Description: "navigation menu", Screenshot: []byte("fake"), Expected: true},
	}

	report, err := eval.RunBatch(context.Background(), cases)
	if err != nil {
		t.Fatalf("batch eval: %v", err)
	}

	if report.Accuracy != 1.0 {
		t.Errorf("expected 100%% accuracy, got %.2f", report.Accuracy)
	}
	if report.TruePositive != 3 {
		t.Errorf("expected 3 TP, got %d", report.TruePositive)
	}
	if report.F1Score != 1.0 {
		t.Errorf("expected F1=1.0, got %.2f", report.F1Score)
	}
	if !eval.PassesTarget(0.8) {
		t.Error("should pass 80% target")
	}
}

func TestVLMJudgeEvaluator_MixedResults(t *testing.T) {
	callCount := 0
	responses := []string{
		`{"present": true, "confidence": 0.92, "explanation": "found", "suggested_selector": "#a"}`,
		`{"present": false, "confidence": 0.85, "explanation": "not found", "suggested_selector": ""}`,
		`{"present": true, "confidence": 0.88, "explanation": "found", "suggested_selector": "#c"}`,
		`{"present": false, "confidence": 0.90, "explanation": "not found", "suggested_selector": ""}`,
		`{"present": true, "confidence": 0.75, "explanation": "found", "suggested_selector": "#e"}`,
	}

	mock := &SequentialMockProvider{responses: responses, callCount: &callCount}
	bridge := NewVLMBridge(mock, []string{"qwen3-vl"})
	eval := NewVLMJudgeEvaluator(bridge, 0.6, nil)

	cases := []JudgeTestCase{
		{ID: "c1", Description: "login button", Screenshot: []byte("f"), Expected: true},
		{ID: "c2", Description: "modal dialog", Screenshot: []byte("f"), Expected: false},
		{ID: "c3", Description: "sidebar", Screenshot: []byte("f"), Expected: true},
		{ID: "c4", Description: "missing element", Screenshot: []byte("f"), Expected: false},
		{ID: "c5", Description: "submit btn", Screenshot: []byte("f"), Expected: true},
	}

	report, err := eval.RunBatch(context.Background(), cases)
	if err != nil {
		t.Fatalf("batch eval: %v", err)
	}

	if report.TotalCases != 5 {
		t.Errorf("expected 5 total, got %d", report.TotalCases)
	}
	if report.Correct != 5 {
		t.Errorf("expected 5 correct, got %d", report.Correct)
	}
	if report.Accuracy < 0.8 {
		t.Errorf("expected accuracy >= 80%%, got %.2f", report.Accuracy)
	}
	if report.Precision == 0 {
		t.Error("precision should not be zero with correct positives")
	}
	if report.Recall == 0 {
		t.Error("recall should not be zero with correct positives")
	}
}

func TestVLMJudgeEvaluator_FalsePositiveAndNegative(t *testing.T) {
	callCount := 0
	responses := []string{
		`{"present": true, "confidence": 0.90, "explanation": "found", "suggested_selector": "#x"}`,
		`{"present": false, "confidence": 0.85, "explanation": "not found", "suggested_selector": ""}`,
	}

	mock := &SequentialMockProvider{responses: responses, callCount: &callCount}
	bridge := NewVLMBridge(mock, []string{"qwen3-vl"})
	eval := NewVLMJudgeEvaluator(bridge, 0.6, nil)

	cases := []JudgeTestCase{
		{ID: "fp", Description: "false positive", Screenshot: []byte("f"), Expected: false},
		{ID: "fn", Description: "false negative", Screenshot: []byte("f"), Expected: true},
	}

	report, err := eval.RunBatch(context.Background(), cases)
	if err != nil {
		t.Fatalf("batch eval: %v", err)
	}

	if report.Correct != 0 {
		t.Errorf("expected 0 correct, got %d", report.Correct)
	}
	if report.FalsePositive != 1 {
		t.Errorf("expected 1 FP, got %d", report.FalsePositive)
	}
	if report.FalseNegative != 1 {
		t.Errorf("expected 1 FN, got %d", report.FalseNegative)
	}
	if report.Accuracy != 0 {
		t.Errorf("expected 0%% accuracy, got %.2f", report.Accuracy)
	}
	if eval.PassesTarget(0.8) {
		t.Error("should NOT pass 80% target with 0% accuracy")
	}
}

func TestVLMJudgeEvaluator_AllModelsFail(t *testing.T) {
	mock := &MockVLMProvider{Err: fmt.Errorf("model offline")}
	bridge := NewVLMBridge(mock, []string{"qwen3-vl"})
	eval := NewVLMJudgeEvaluator(bridge, 0.6, nil)

	cases := []JudgeTestCase{
		{ID: "c1", Description: "button", Screenshot: []byte("f"), Expected: true},
	}

	report, err := eval.RunBatch(context.Background(), cases)
	if err != nil {
		t.Fatalf("batch eval should succeed even with failed cases: %v", err)
	}
	if report.Correct != 0 {
		t.Errorf("expected 0 correct when model fails, got %d", report.Correct)
	}
}

func TestVLMJudgeEvaluator_LowConfidenceRejection(t *testing.T) {
	mock := &MockVLMProvider{
		Response: `{"present": true, "confidence": 0.3, "explanation": "uncertain", "suggested_selector": ""}`,
	}
	bridge := NewVLMBridge(mock, []string{"qwen3-vl"})
	eval := NewVLMJudgeEvaluator(bridge, 0.6, nil)

	cases := []JudgeTestCase{
		{ID: "c1", Description: "button", Screenshot: []byte("f"), Expected: false},
	}

	report, err := eval.RunBatch(context.Background(), cases)
	if err != nil {
		t.Fatalf("batch eval: %v", err)
	}

	if report.Results[0].Predicted {
		t.Error("low confidence (0.3) should NOT predict present when threshold is 0.6")
	}
	if !report.Results[0].Correct {
		t.Error("predicting absent for absent element should be correct")
	}
}

func TestVLMJudgeEvaluator_EmptyCases(t *testing.T) {
	mock := &MockVLMProvider{Response: "ok"}
	bridge := NewVLMBridge(mock, []string{"qwen3-vl"})
	eval := NewVLMJudgeEvaluator(bridge, 0.6, nil)

	report, err := eval.RunBatch(context.Background(), nil)
	if err != nil {
		t.Fatalf("batch eval: %v", err)
	}
	if report.TotalCases != 0 {
		t.Errorf("expected 0 cases, got %d", report.TotalCases)
	}
	if report.Accuracy != 0 {
		t.Errorf("expected 0 accuracy for no cases, got %.2f", report.Accuracy)
	}
}

func TestVLMJudgeEvaluator_ScaleAccuracy(t *testing.T) {
	mock := &MockVLMProvider{
		Response: `{"present": true, "confidence": 0.92, "explanation": "found", "suggested_selector": "#x"}`,
	}
	bridge := NewVLMBridge(mock, []string{"qwen3-vl"})
	eval := NewVLMJudgeEvaluator(bridge, 0.6, nil)

	cases := make([]JudgeTestCase, 20)
	for i := range cases {
		cases[i] = JudgeTestCase{
			ID: fmt.Sprintf("case-%d", i), Description: "element",
			Screenshot: []byte("f"), Expected: true,
		}
	}

	report, err := eval.RunBatch(context.Background(), cases)
	if err != nil {
		t.Fatalf("batch eval: %v", err)
	}
	if report.Accuracy < 0.8 {
		t.Errorf("expected >= 80%% accuracy at scale, got %.2f", report.Accuracy)
	}
	if report.AvgConfidence < 0.5 {
		t.Errorf("expected avg confidence > 0.5, got %.2f", report.AvgConfidence)
	}
}

// SequentialMockProvider returns different responses for each call.
type SequentialMockProvider struct {
	responses []string
	callCount *int
}

func (m *SequentialMockProvider) Complete(_ context.Context, _ llm.CompletionRequest) (*llm.CompletionResponse, error) {
	idx := *m.callCount
	*m.callCount++
	if idx >= len(m.responses) {
		return nil, fmt.Errorf("no more mock responses")
	}
	return &llm.CompletionResponse{
		Choices: []llm.Choice{
			{Message: llm.Message{Role: "assistant", Content: m.responses[idx]}},
		},
	}, nil
}

// --- Sprint 3: VLM-as-Judge comprehensive LMS E2E, F1 80%+ target ---

func TestVLMJudgeEvaluator_LMS_F1_80Pct(t *testing.T) {
	type lmsCase struct {
		id          string
		description string
		present     bool // ground truth
		vlmPresent  bool // simulated VLM response
		vlmConf     float64
	}

	scenarios := []lmsCase{
		// True Positives -- element present and VLM correctly identifies
		{id: "tp-login-btn", description: "D2L login button", present: true, vlmPresent: true, vlmConf: 0.95},
		{id: "tp-content-nav", description: "course content navigation", present: true, vlmPresent: true, vlmConf: 0.92},
		{id: "tp-grade-table", description: "student grade table", present: true, vlmPresent: true, vlmConf: 0.88},
		{id: "tp-submit-btn", description: "assignment submit button", present: true, vlmPresent: true, vlmConf: 0.91},
		{id: "tp-file-upload", description: "file upload widget", present: true, vlmPresent: true, vlmConf: 0.85},
		{id: "tp-discussion", description: "discussion forum post", present: true, vlmPresent: true, vlmConf: 0.90},
		{id: "tp-calendar", description: "calendar event widget", present: true, vlmPresent: true, vlmConf: 0.87},
		{id: "tp-announcement", description: "course announcement banner", present: true, vlmPresent: true, vlmConf: 0.93},
		{id: "tp-quiz-start", description: "quiz start button", present: true, vlmPresent: true, vlmConf: 0.89},
		{id: "tp-search-bar", description: "course search bar", present: true, vlmPresent: true, vlmConf: 0.94},
		{id: "tp-sidebar-nav", description: "sidebar navigation menu", present: true, vlmPresent: true, vlmConf: 0.91},
		{id: "tp-breadcrumb", description: "breadcrumb navigation", present: true, vlmPresent: true, vlmConf: 0.86},
		{id: "tp-rubric-view", description: "assignment rubric viewer", present: true, vlmPresent: true, vlmConf: 0.88},
		{id: "tp-peer-review", description: "peer review submission link", present: true, vlmPresent: true, vlmConf: 0.84},
		{id: "tp-video-embed", description: "embedded lecture video player", present: true, vlmPresent: true, vlmConf: 0.82},

		// True Negatives -- element absent and VLM correctly reports absent
		{id: "tn-admin-panel", description: "admin control panel", present: false, vlmPresent: false, vlmConf: 0.88},
		{id: "tn-deleted-post", description: "deleted discussion post", present: false, vlmPresent: false, vlmConf: 0.91},
		{id: "tn-old-submit", description: "deprecated submit button v1", present: false, vlmPresent: false, vlmConf: 0.85},
		{id: "tn-removed-widget", description: "removed calendar widget", present: false, vlmPresent: false, vlmConf: 0.90},
		{id: "tn-hidden-form", description: "hidden feedback form", present: false, vlmPresent: false, vlmConf: 0.87},
		{id: "tn-stale-link", description: "stale course link", present: false, vlmPresent: false, vlmConf: 0.93},
		{id: "tn-no-rubric", description: "rubric not assigned", present: false, vlmPresent: false, vlmConf: 0.86},

		// False Positives -- VLM incorrectly says present
		{id: "fp-phantom-btn", description: "phantom action button", present: false, vlmPresent: true, vlmConf: 0.72},
		{id: "fp-wrong-modal", description: "misidentified modal dialog", present: false, vlmPresent: true, vlmConf: 0.68},

		// False Negatives -- VLM misses present element
		{id: "fn-tiny-link", description: "small footer link", present: true, vlmPresent: false, vlmConf: 0.40},
		{id: "fn-low-contrast", description: "low contrast disabled button", present: true, vlmPresent: true, vlmConf: 0.45},

		// Edge cases
		{id: "edge-borderline-conf", description: "borderline confidence element", present: true, vlmPresent: true, vlmConf: 0.60},
		{id: "edge-just-below", description: "just below threshold", present: true, vlmPresent: true, vlmConf: 0.59},
		{id: "edge-loading-spinner", description: "loading spinner (transient)", present: true, vlmPresent: true, vlmConf: 0.78},
		{id: "edge-tooltip", description: "hover tooltip text", present: true, vlmPresent: true, vlmConf: 0.70},
	}

	callCount := 0
	var responses []string
	for _, sc := range scenarios {
		resp := fmt.Sprintf(`{"present": %v, "confidence": %.2f, "explanation": "test", "suggested_selector": ""}`,
			sc.vlmPresent, sc.vlmConf)
		responses = append(responses, resp)
	}

	mock := &SequentialMockProvider{responses: responses, callCount: &callCount}
	bridge := NewVLMBridge(mock, []string{"qwen3-vl"})
	eval := NewVLMJudgeEvaluator(bridge, 0.6, nil)

	var cases []JudgeTestCase
	for _, sc := range scenarios {
		cases = append(cases, JudgeTestCase{
			ID:          sc.id,
			Description: sc.description,
			Screenshot:  []byte("mock-screenshot"),
			Expected:    sc.present,
		})
	}

	report, err := eval.RunBatch(context.Background(), cases)
	if err != nil {
		t.Fatalf("batch eval: %v", err)
	}

	t.Logf("\n--- VLM-as-Judge LMS E2E Summary ---")
	t.Logf("Total: %d  Correct: %d  Accuracy: %.2f%%", report.TotalCases, report.Correct, report.Accuracy*100)
	t.Logf("TP: %d  FP: %d  TN: %d  FN: %d", report.TruePositive, report.FalsePositive, report.TrueNegative, report.FalseNegative)
	t.Logf("Precision: %.3f  Recall: %.3f  F1: %.3f", report.Precision, report.Recall, report.F1Score)

	for _, r := range report.Results {
		status := "PASS"
		if !r.Correct {
			status = "FAIL"
		}
		t.Logf("  %s %-30s expected=%v predicted=%v conf=%.2f", status, r.CaseID, r.Expected, r.Predicted, r.Confidence)
	}

	if report.F1Score < 0.80 {
		t.Errorf("F1 score %.3f below 80%% target", report.F1Score)
	}

	if !eval.PassesF1Target(report, 0.80) {
		t.Errorf("PassesF1Target(0.80) returned false with F1=%.3f", report.F1Score)
	}
}

func TestVLMJudgeEvaluator_PrometheusMetricsIntegration(t *testing.T) {
	reg := prometheus.NewRegistry()
	prom := NewMetrics(reg)

	mock := &MockVLMProvider{
		Response: `{"present": true, "confidence": 0.92, "explanation": "found", "suggested_selector": "#x"}`,
	}
	bridge := NewVLMBridge(mock, []string{"qwen3-vl"})
	eval := NewVLMJudgeEvaluator(bridge, 0.6, nil)

	cases := []JudgeTestCase{
		{ID: "c1", Description: "button", Screenshot: []byte("f"), Expected: true},
		{ID: "c2", Description: "link", Screenshot: []byte("f"), Expected: true},
	}

	report, err := eval.RunBatch(context.Background(), cases)
	if err != nil {
		t.Fatalf("batch eval: %v", err)
	}

	// Simulate MetricsCollector.CollectJudgeReport without needing a MemberAgent
	prom.VLMJudgeF1Score.Set(report.F1Score)
	prom.VLMJudgePrecision.Set(report.Precision)
	prom.VLMJudgeRecall.Set(report.Recall)
	prom.VLMJudgeAccuracy.Set(report.Accuracy)

	for _, r := range report.Results {
		if r.Correct {
			prom.VLMJudgeCasesTotal.WithLabelValues("correct").Inc()
		} else {
			prom.VLMJudgeCasesTotal.WithLabelValues("incorrect").Inc()
		}
	}

	// Verify metrics were published
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	found := make(map[string]bool)
	for _, mf := range mfs {
		found[mf.GetName()] = true
	}

	required := []string{
		"uiauto_vlm_judge_f1_score",
		"uiauto_vlm_judge_precision",
		"uiauto_vlm_judge_recall",
		"uiauto_vlm_judge_accuracy",
		"uiauto_vlm_judge_cases_total",
	}
	for _, name := range required {
		if !found[name] {
			t.Errorf("expected Prometheus metric %q to be published", name)
		}
	}
}

func TestVLMJudgeEvaluator_RouterIntegration_Qwen3VL(t *testing.T) {
	callCount := 0
	responses := []string{
		`{"present": true, "confidence": 0.90, "explanation": "login visible", "suggested_selector": "#login"}`,
		`{"present": false, "confidence": 0.85, "explanation": "not found", "suggested_selector": ""}`,
		`{"present": true, "confidence": 0.88, "explanation": "nav present", "suggested_selector": "nav"}`,
	}

	mock := &SequentialMockProvider{responses: responses, callCount: &callCount}
	bridge := NewVLMBridge(mock, []string{"qwen3-vl-7b", "qwen3-vl-72b"})
	eval := NewVLMJudgeEvaluator(bridge, 0.6, nil)

	cases := []JudgeTestCase{
		{ID: "router-1", Description: "login button", Screenshot: []byte("f"), Expected: true},
		{ID: "router-2", Description: "missing element", Screenshot: []byte("f"), Expected: false},
		{ID: "router-3", Description: "navigation", Screenshot: []byte("f"), Expected: true},
	}

	report, err := eval.RunBatch(context.Background(), cases)
	if err != nil {
		t.Fatalf("batch eval: %v", err)
	}

	if report.Accuracy < 0.8 {
		t.Errorf("expected >= 80%% accuracy with Qwen3-VL mock, got %.2f", report.Accuracy)
	}

	// Verify VLM metrics were tracked
	snap := bridge.Metrics.Snapshot()
	if snap.TotalCalls != 3 {
		t.Errorf("expected 3 VLM calls, got %d", snap.TotalCalls)
	}
	if snap.SuccessCalls != 3 {
		t.Errorf("expected 3 successful VLM calls, got %d", snap.SuccessCalls)
	}
}
