package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// miniwobStubSidecar is the deterministic stand-in for the BrowserGym
// sidecar: it serves /healthz, /start, /step and /close, and hands out
// scripted step replies.
type miniwobStubSidecar struct {
	server    *httptest.Server
	started   atomic.Int32
	closed    atomic.Int32
	steps     atomic.Int32
	stepReply func(call int) map[string]any
	startCode atomic.Int32
}

func newMiniwobStub(t *testing.T, stepReply func(call int) map[string]any) *miniwobStubSidecar {
	t.Helper()
	s := &miniwobStubSidecar{stepReply: stepReply}
	s.startCode.Store(200)
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		s.started.Add(1)
		if s.startCode.Load() != 200 {
			w.WriteHeader(int(s.startCode.Load()))
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unknown task"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"obs": "GOAL: click the button.\nAXTREE: [button] Ok", "task": "miniwob.click-button"})
	})
	mux.HandleFunc("/step", func(w http.ResponseWriter, _ *http.Request) {
		n := int(s.steps.Add(1))
		_ = json.NewEncoder(w).Encode(s.stepReply(n))
	})
	mux.HandleFunc("/close", func(w http.ResponseWriter, _ *http.Request) {
		s.closed.Add(1)
		w.WriteHeader(http.StatusOK)
	})
	s.server = httptest.NewServer(mux)
	t.Cleanup(s.server.Close)
	return s
}

// miniwobStubLLM answers every chat call with the given content and
// token usage, so token accounting is assertable without a model.
type miniwobStubLLM struct {
	server   *httptest.Server
	calls    atomic.Int32
	content  string
	promTok  int
	compTok  int
	lastAuth atomic.Value // string
	lastSys  atomic.Value // string: system prompt of the latest call
}

func newMiniwobStubLLM(t *testing.T, content string) *miniwobStubLLM {
	t.Helper()
	l := &miniwobStubLLM{content: content, promTok: 900, compTok: 120}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		l.calls.Add(1)
		l.lastAuth.Store(r.Header.Get("Authorization"))
		var body struct {
			Messages []chatMessage `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body.Messages) > 0 {
			l.lastSys.Store(body.Messages[0].Content)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": l.content}}},
			"usage":   map[string]int{"prompt_tokens": l.promTok, "completion_tokens": l.compTok},
		})
	})
	l.server = httptest.NewServer(mux)
	t.Cleanup(l.server.Close)
	return l
}

func TestMiniWob_RewardOneIsOKAndCountsTokens(t *testing.T) {
	gym := newMiniwobStub(t, func(call int) map[string]any {
		if call == 1 {
			return map[string]any{"obs": "clicked", "reward": 0, "done": false}
		}
		return map[string]any{"obs": "done", "reward": 1, "done": true}
	})
	llm := newMiniwobStubLLM(t, "thinking...\n</think>\nclick('e3')")
	ex := &MiniWobExecutor{
		GymURL: gym.server.URL,
		LLM:    MiniWobLLMConfig{BaseURL: llm.server.URL, Model: "stub"},
	}
	rec := ex.Run(context.Background(), Scenario{ID: "s1", Task: "miniwob.click-button", MaxSteps: 5}, 1)
	if !rec.OK {
		t.Fatalf("want OK, errors=%v", rec.Errors)
	}
	if rec.Steps != 2 {
		t.Fatalf("want 2 steps, got %d", rec.Steps)
	}
	if got := rec.Evidence["tokens_in"]; got != 1800 {
		t.Fatalf("want tokens_in 1800 (2 calls x 900), got %v", got)
	}
	if got := rec.Evidence["tokens_out"]; got != 240 {
		t.Fatalf("want tokens_out 240, got %v", got)
	}
	if rec.Evidence["reward"] != float64(1) {
		t.Fatalf("want reward 1, got %v", rec.Evidence["reward"])
	}
	if gym.closed.Load() != 1 {
		t.Fatalf("sidecar close called %d times, want 1", gym.closed.Load())
	}
}

func TestMiniWob_StepBudgetIsAFailureWithNamedError(t *testing.T) {
	gym := newMiniwobStub(t, func(int) map[string]any {
		return map[string]any{"obs": "still going", "reward": 0, "done": false}
	})
	llm := newMiniwobStubLLM(t, "click('e1')")
	ex := &MiniWobExecutor{GymURL: gym.server.URL, LLM: MiniWobLLMConfig{BaseURL: llm.server.URL, Model: "stub"}}
	rec := ex.Run(context.Background(), Scenario{ID: "s2", Task: "miniwob.focus-text", MaxSteps: 3}, 1)
	if rec.OK {
		t.Fatal("a capped run must not be OK")
	}
	if rec.Steps != 3 {
		t.Fatalf("want 3 steps, got %d", rec.Steps)
	}
	if len(rec.Errors) == 0 || rec.Errors[0] == "" {
		t.Fatalf("want a named budget error, got %v", rec.Errors)
	}
	if rec.Evidence["steps_capped"] != true {
		t.Fatal("steps_capped must be true")
	}
}

func TestMiniWob_ModelWithoutActionLineFailsTheRun(t *testing.T) {
	gym := newMiniwobStub(t, func(int) map[string]any {
		return map[string]any{"obs": "x", "reward": 0, "done": false}
	})
	llm := newMiniwobStubLLM(t, "I would click the button, probably the first one.")
	ex := &MiniWobExecutor{GymURL: gym.server.URL, LLM: MiniWobLLMConfig{BaseURL: llm.server.URL, Model: "stub"}}
	rec := ex.Run(context.Background(), Scenario{ID: "s3", Task: "miniwob.click-button"}, 1)
	if rec.OK {
		t.Fatal("no action line must fail")
	}
	if len(rec.Errors) == 0 {
		t.Fatal("want named error")
	}
}

func TestMiniWob_UnknownTaskIsAnErrorNotAPanic(t *testing.T) {
	gym := newMiniwobStub(t, func(int) map[string]any { return nil })
	gym.startCode.Store(400)
	llm := newMiniwobStubLLM(t, "click('e1')")
	ex := &MiniWobExecutor{GymURL: gym.server.URL, LLM: MiniWobLLMConfig{BaseURL: llm.server.URL, Model: "stub"}}
	rec := ex.Run(context.Background(), Scenario{ID: "s4", Task: "miniwob.nope"}, 1)
	if rec.OK {
		t.Fatal("unknown task must fail")
	}
	if len(rec.Errors) == 0 {
		t.Fatal("want named error")
	}
}

func TestMiniWob_MissingLLMConfigRefuses(t *testing.T) {
	gym := newMiniwobStub(t, func(int) map[string]any { return nil })
	ex := &MiniWobExecutor{GymURL: gym.server.URL}
	rec := ex.Run(context.Background(), Scenario{ID: "s5", Task: "miniwob.click-button"}, 1)
	if rec.OK || len(rec.Errors) == 0 {
		t.Fatalf("missing LLM config must fail with a named error, got %+v", rec)
	}
	if gym.started.Load() != 0 {
		t.Fatal("no task may start when the LLM is unconfigured")
	}
}

func TestMiniWob_HealthyProbesTheSidecar(t *testing.T) {
	gym := newMiniwobStub(t, func(int) map[string]any { return nil })
	ex := &MiniWobExecutor{GymURL: gym.server.URL}
	if err := ex.Healthy(context.Background()); err != nil {
		t.Fatalf("healthy sidecar: %v", err)
	}
	ex = &MiniWobExecutor{GymURL: "http://127.0.0.1:1"}
	if err := ex.Healthy(context.Background()); err == nil {
		t.Fatal("dead sidecar must fail the probe")
	}
}

func TestMiniWob_ExecutorRegisteredName(t *testing.T) {
	ex := &MiniWobExecutor{}
	if ex.Name() != "browsergym-miniwob" {
		t.Fatalf("executor name %q must match suite YAML", ex.Name())
	}
}

// The webarena rung reuses this executor under its own name and a longer
// default cap; the record must carry the override, not the miniwob name.
func TestMiniWob_WebArenaOverridesNameAndDefaultCap(t *testing.T) {
	gym := newMiniwobStub(t, func(int) map[string]any {
		return map[string]any{"obs": "still going", "reward": 0, "done": false}
	})
	llm := newMiniwobStubLLM(t, "click('e1')")
	ex := &MiniWobExecutor{
		GymURL:       gym.server.URL,
		ExecutorName: "browsergym-webarena",
		DefaultCap:   2,
		LLM:          MiniWobLLMConfig{BaseURL: llm.server.URL, Model: "stub"},
	}
	if ex.Name() != "browsergym-webarena" {
		t.Fatalf("override name ignored: %q", ex.Name())
	}
	// No MaxSteps on the scenario: DefaultCap (2), not miniwobDefaultCap.
	rec := ex.Run(context.Background(), Scenario{ID: "wa1", Task: "webarenalite.0"}, 1)
	if rec.OK || rec.Steps != 2 {
		t.Fatalf("DefaultCap ignored: ok=%v steps=%d errors=%v", rec.OK, rec.Steps, rec.Errors)
	}
	if rec.Executor != "browsergym-webarena" {
		t.Fatalf("record executor %q must carry the override", rec.Executor)
	}
}

func TestExtractAction_IgnoresProseAndThinkBlocks(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"<think>user wants click</think>\nclick('e12')", "click('e12')"},
		{"Sure! fill('e5', 'Bob')", "fill('e5', 'Bob')"},
		{"no action here", ""},
		{"stop()", "stop()"},
		{"The answer:\nscroll(0, 300)\nthen more prose", "scroll(0, 300)"},
		// Leftmost call wins: a fill whose TEXT looks like a call is a
		// fill, never the inner click.
		{"fill('e5', 'click(x)')", "fill('e5', 'click(x)')"},
		// WebArena answer protocol: the reply IS the send_msg_to_user call.
		{"send_msg_to_user('42 orders')", "send_msg_to_user('42 orders')"},
	}
	for _, c := range cases {
		if got := extractAction(c.in); got != c.want {
			t.Errorf("extractAction(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The history must stay bounded: every step appends a full observation,
// so long-horizon arenas overflow the model context without trimming.
func TestMiniWob_TrimHistoryKeepsGoalAndTail(t *testing.T) {
	small := make([]chatMessage, 2+1+historyKeepTail)
	for i := range small {
		small[i] = chatMessage{Role: "user", Content: fmt.Sprintf("m%d", i)}
	}
	small[0] = chatMessage{Role: "system", Content: "sys"}
	small[1] = chatMessage{Role: "user", Content: "GOAL"}
	if got := trimHistory(small); len(got) != len(small) {
		t.Fatalf("history at the bound must pass through, got %d of %d", len(got), len(small))
	}
	big := make([]chatMessage, 2+1+historyKeepTail+6)
	for i := range big {
		big[i] = chatMessage{Role: "user", Content: fmt.Sprintf("m%d", i)}
	}
	big[0] = chatMessage{Role: "system", Content: "sys"}
	big[1] = chatMessage{Role: "user", Content: "GOAL"}
	got := trimHistory(big)
	if len(got) != 2+1+historyKeepTail {
		t.Fatalf("trimmed length %d, want %d", len(got), 2+1+historyKeepTail)
	}
	if got[0].Content != "sys" || got[1].Content != "GOAL" {
		t.Fatalf("system + goal-bearing first observation must survive, got %q %q", got[0].Content, got[1].Content)
	}
	if got[2].Content != historyTrimNote {
		t.Fatalf("position 2 must be the omission note, got %q", got[2].Content)
	}
	if got[len(got)-1].Content != big[len(big)-1].Content {
		t.Fatal("the most recent message must be the tail verbatim")
	}
}

// The prompt override must reach the wire: a webarena-wired executor
// teaches send_msg_to_user, the default never does.
func TestMiniWob_SystemPromptOverrideReachesTheWire(t *testing.T) {
	gym := newMiniwobStub(t, func(int) map[string]any {
		return map[string]any{"obs": "x", "reward": 0, "done": false}
	})
	llm := newMiniwobStubLLM(t, "stop()")
	ex := &MiniWobExecutor{GymURL: gym.server.URL, SystemPrompt: WebArenaSystemPrompt,
		LLM: MiniWobLLMConfig{BaseURL: llm.server.URL, Model: "stub"}}
	_ = ex.Run(context.Background(), Scenario{ID: "wa", Task: "webarenalite.4", MaxSteps: 1}, 1)
	sys, _ := llm.lastSys.Load().(string)
	if !strings.Contains(sys, "send_msg_to_user") {
		t.Fatalf("system prompt on the wire must teach the answer protocol, got %q", sys)
	}
}
