package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/nfsarch33/uiauto-framework/pkg/uiauto"
)

// fakeServerAgent is a hand-rolled stub that satisfies serverAgent. It records
// every method call so tests can assert handler behaviour without touching a
// real browser.
type fakeServerAgent struct {
	mu          sync.Mutex
	degraded    bool
	converged   bool
	tier        uiauto.ModelTier
	taskCount   int
	navErr      error
	healResults []uiauto.HealResult
	metrics     uiauto.AggregatedMetrics
	taskResult  uiauto.TaskResult
	calledNav   []string
	calledHeal  int
	calledRun   []string
	calledTier  int
	calledDegr  int
	calledConv  int
	calledTC    int
	calledMetr  int
}

func (f *fakeServerAgent) IsDegraded() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calledDegr++
	return f.degraded
}

func (f *fakeServerAgent) IsConverged() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calledConv++
	return f.converged
}

func (f *fakeServerAgent) CurrentTier() uiauto.ModelTier {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calledTier++
	return f.tier
}

func (f *fakeServerAgent) Navigate(url string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calledNav = append(f.calledNav, url)
	return f.navErr
}

func (f *fakeServerAgent) DetectDriftAndHeal(ctx context.Context) []uiauto.HealResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calledHeal++
	return f.healResults
}

func (f *fakeServerAgent) Metrics() uiauto.AggregatedMetrics {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calledMetr++
	return f.metrics
}

func (f *fakeServerAgent) TaskCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calledTC++
	return f.taskCount
}

func (f *fakeServerAgent) RunTask(ctx context.Context, taskID string, actions []uiauto.Action) uiauto.TaskResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calledRun = append(f.calledRun, taskID)
	r := f.taskResult
	r.TaskID = taskID
	return r
}

func newTestMux(t *testing.T, agent *fakeServerAgent) *http.ServeMux {
	t.Helper()
	card := defaultA2ACard("http://test.example/")
	return buildServeMux(card, agent, true, "/tmp/test-patterns.json")
}

func TestBuildServeMux_A2ACard(t *testing.T) {
	agent := &fakeServerAgent{}
	mux := newTestMux(t, agent)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/a2a-card", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("content-type=%q", got)
	}
	var card A2ACard
	if err := json.Unmarshal(w.Body.Bytes(), &card); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if card.Name != "ui-agent" {
		t.Errorf("name=%q", card.Name)
	}
	if len(card.Capabilities) == 0 {
		t.Error("capabilities should be populated")
	}
}

func TestBuildServeMux_Health(t *testing.T) {
	agent := &fakeServerAgent{degraded: true, converged: false, tier: 1}
	mux := newTestMux(t, agent)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status=%v", body["status"])
	}
	if body["degraded"] != true {
		t.Errorf("degraded=%v", body["degraded"])
	}
	if body["converged"] != false {
		t.Errorf("converged=%v", body["converged"])
	}
	if agent.calledDegr == 0 || agent.calledConv == 0 || agent.calledTier == 0 {
		t.Errorf("expected agent state methods to be called")
	}
}

func TestBuildServeMux_Heal_Post_HappyPath(t *testing.T) {
	agent := &fakeServerAgent{
		healResults: []uiauto.HealResult{
			{TargetID: "btn-login", Success: true, Method: "fingerprint"},
			{TargetID: "btn-submit", Success: false, Method: "smart_llm"},
		},
	}
	mux := newTestMux(t, agent)

	body := strings.NewReader(`{"page_url":"https://example.com","selector":"#login","element_type":"button","description":"login"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/heal", body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var got map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["status"] != "completed" {
		t.Errorf("status=%v", got["status"])
	}
	if got["count"].(float64) != 2 {
		t.Errorf("count=%v", got["count"])
	}
	if len(agent.calledNav) != 1 || agent.calledNav[0] != "https://example.com" {
		t.Errorf("nav=%v", agent.calledNav)
	}
	if agent.calledHeal != 1 {
		t.Errorf("heal=%d", agent.calledHeal)
	}
}

func TestBuildServeMux_Heal_NavError_StillRuns(t *testing.T) {
	agent := &fakeServerAgent{navErr: errors.New("nav failed")}
	mux := newTestMux(t, agent)

	body := strings.NewReader(`{"page_url":"https://broken.example"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/heal", body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	if agent.calledHeal != 1 {
		t.Errorf("heal still expected to run despite nav error")
	}
}

func TestBuildServeMux_Heal_Get_NotAllowed(t *testing.T) {
	agent := &fakeServerAgent{}
	mux := newTestMux(t, agent)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/heal", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status=%d", w.Code)
	}
}

func TestBuildServeMux_Heal_BadJSON(t *testing.T) {
	agent := &fakeServerAgent{}
	mux := newTestMux(t, agent)

	body := strings.NewReader(`{not-json`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/heal", body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d", w.Code)
	}
}

func TestBuildServeMux_Status(t *testing.T) {
	agent := &fakeServerAgent{taskCount: 7, tier: 2}
	mux := newTestMux(t, agent)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["agent"] != "ui-agent" {
		t.Errorf("agent=%v", body["agent"])
	}
	if body["task_count"].(float64) != 7 {
		t.Errorf("task_count=%v", body["task_count"])
	}
	if body["pattern_file"] != "/tmp/test-patterns.json" {
		t.Errorf("pattern_file=%v", body["pattern_file"])
	}
	if agent.calledMetr != 1 {
		t.Errorf("metrics call count=%d", agent.calledMetr)
	}
}

func TestBuildServeMux_RunTask_HappyPath(t *testing.T) {
	agent := &fakeServerAgent{taskResult: uiauto.TaskResult{Status: uiauto.TaskCompleted}}
	mux := newTestMux(t, agent)

	body := strings.NewReader(`{"task_id":"t-1","actions":[{"Type":"click","TargetID":"btn"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/run-task", body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if len(agent.calledRun) != 1 || agent.calledRun[0] != "t-1" {
		t.Errorf("run=%v", agent.calledRun)
	}
}

func TestBuildServeMux_RunTask_GET_NotAllowed(t *testing.T) {
	agent := &fakeServerAgent{}
	mux := newTestMux(t, agent)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/run-task", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status=%d", w.Code)
	}
}

func TestBuildServeMux_RunTask_BadJSON(t *testing.T) {
	agent := &fakeServerAgent{}
	mux := newTestMux(t, agent)

	body := strings.NewReader(`{this-is-not-json`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/run-task", body)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status=%d", w.Code)
	}
}

func TestPrintHealResults_FormatsBothOutcomes(t *testing.T) {
	var buf bytes.Buffer
	results := []uiauto.HealResult{
		{TargetID: "btn-1", Success: true, Method: "fingerprint"},
		{TargetID: "btn-2", Success: false, Method: "smart_llm"},
	}
	printHealResults(&buf, results)
	got := buf.String()
	for _, want := range []string{"Self-healing results: 2 repairs attempted", "OK target=btn-1", "FAILED target=btn-2", "method=fingerprint", "method=smart_llm"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q: %s", want, got)
		}
	}
}

func TestPrintHealResults_EmptySlice(t *testing.T) {
	var buf bytes.Buffer
	printHealResults(&buf, nil)
	if !strings.Contains(buf.String(), "0 repairs") {
		t.Errorf("expected 0 repairs message, got %q", buf.String())
	}
}
