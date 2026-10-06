package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// MiniWoB executor: the first rung of the agent-arena ladder
// (BrowserGym-served tasks with programmatic rewards). A python
// sidecar owns the BrowserGym environment (browser + programmatic
// reward); this executor owns the agent loop: observation in, ONE action
// string out per step, produced by an OpenAI-compatible chat endpoint.
// Verdicts are the gym's own rewards — no judge model anywhere.
//
// All endpoints arrive from outside (flags/env), never baked in:
//   - sidecar: --gym flag (default http://127.0.0.1:8093)
//   - LLM: UA_EVAL_LLM_BASE_URL, UA_EVAL_LLM_MODEL, UA_EVAL_LLM_API_KEY
//     (optional; empty sends no Authorization), UA_EVAL_LLM_AGENT_HEADER
//     (optional caller-attribution header value)
const (
	miniwobExecutorName = "browsergym-miniwob"
	miniwobDefaultCap   = 15
	miniwobHTTPTIMEOUT  = 60 * time.Second
)

// miniwobSystemPrompt teaches the action grammar once per conversation.
// The bid ids in the prompt are the ones the axtree observations carry.
const miniwobSystemPrompt = "You are a web UI agent. Each turn you receive GOAL and AXTREE (an accessibility tree; leaves carry a bid id in parentheses). Reply with EXACTLY ONE action line and nothing else:\n" +
	"click('bid') | fill('bid', 'text') | press('key') | scroll(direction) | stop()\n" +
	"Click buttons/links/checkboxes named by the goal; fill text boxes with requested values. Do not explain."

// webarenaSystemPrompt extends the grammar with the answer protocol
// WebArena's validator needs: it string-matches the LAST assistant
// message, which only lands in its chat log via send_msg_to_user —
// without it, answer-reporting tasks are structurally unpassable.
const webarenaSystemPrompt = "You are a web UI agent working in a website admin panel. Each turn you receive GOAL and AXTREE (an accessibility tree; leaves carry a bid id in parentheses). Reply with EXACTLY ONE action line and nothing else:\n" +
	"click('bid') | fill('bid', 'text') | press('key') | scroll(direction) | send_msg_to_user('answer text') | stop()\n" +
	"Navigate with clicks. When the GOAL asks a question and you can see the answer, send it with send_msg_to_user('...'). Do not explain."

// WebArenaSystemPrompt is the exported rung-2 grammar (send_msg_to_user
// answer protocol) for wiring from cmd.
const WebArenaSystemPrompt = webarenaSystemPrompt

// MiniWobExecutor runs one BrowserGym task per scenario through the
// sidecar, with the LLM choosing actions.
type MiniWobExecutor struct {
	GymURL string
	// LLM is the chat endpoint config; zero value fails every run with a
	// named error (the harness reports, never guesses).
	LLM MiniWobLLMConfig
	// Client is injectable for tests; nil uses http.DefaultClient with a
	// bounded timeout.
	Client *http.Client
	// ExecutorName renames the executor in records and PreFlight (empty =
	// browsergym-miniwob). The loop is arena-agnostic: a second BrowserGym
	// registry (webarena) reuses this executor under its own name.
	ExecutorName string
	// DefaultCap overrides the per-scenario step-budget default (0 =
	// miniwobDefaultCap). Longer-horizon arenas need a longer cap.
	DefaultCap int
	// SystemPrompt replaces the built-in grammar prompt (empty =
	// miniwobSystemPrompt); arenas with extra actions (WebArena's
	// send_msg_to_user answer protocol) teach their own grammar.
	SystemPrompt string
	// StepTimeout bounds one sidecar /step call (0 = miniwobHTTPTIMEOUT).
	// Dense pages can spend longer than a minute in reward evaluation.
	StepTimeout time.Duration
}

// MiniWobLLMConfig names an OpenAI-compatible /v1/chat/completions
// endpoint. APIKeyEnv names an ENV VAR whose value is the bearer — the
// key itself never travels through suite YAML or flags.
type MiniWobLLMConfig struct {
	BaseURL     string
	Model       string
	APIKeyEnv   string
	AgentHeader string
}

func (e *MiniWobExecutor) Name() string {
	if e.ExecutorName != "" {
		return e.ExecutorName
	}
	return miniwobExecutorName
}

// Healthy probes the sidecar so PreFlight can refuse a dead gym before
// any task starts (NOT_RUN, never zeroed gauges).
func (e *MiniWobExecutor) Healthy(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.GymURL+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := e.client().Do(req)
	if err != nil {
		return fmt.Errorf("gym sidecar %s unreachable: %w", e.GymURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gym sidecar health answered %d", resp.StatusCode)
	}
	return nil
}

// gymStartReply is /start's payload: the first observation (goal +
// accessibility tree text, already bounded by the sidecar).
type gymStartReply struct {
	Obs  string `json:"obs"`
	Task string `json:"task"`
}

// gymStepReply is /step's payload: the reward is the task's own
// programmatic verdict for the step; done ends the episode.
type gymStepReply struct {
	Obs    string  `json:"obs"`
	Reward float64 `json:"reward"`
	Done   bool    `json:"done"`
}

// chatReply is the slice of /v1/chat/completions the executor reads.
type chatReply struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (e *MiniWobExecutor) client() *http.Client {
	if e.Client != nil {
		return e.Client
	}
	timeout := miniwobHTTPTIMEOUT
	if e.StepTimeout > 0 {
		timeout = e.StepTimeout
	}
	return &http.Client{Timeout: timeout}
}

// Run plays one scenario: start the task, loop LLM->action->step until
// the gym says done or the step budget is spent. OK is reward==1 at the
// end; tokens and reward ride in Evidence for the report/ledger.
func (e *MiniWobExecutor) Run(ctx context.Context, sc Scenario, attempt int) RunRecord {
	start := time.Now()
	rec := RunRecord{
		ScenarioID: sc.ID,
		Executor:   e.Name(),
		Attempt:    attempt,
		Evidence:   map[string]any{"task": sc.Task},
	}
	fail := func(format string, args ...any) RunRecord {
		rec.OK = false
		rec.DurationSec = time.Since(start).Seconds()
		rec.Errors = append(rec.Errors, fmt.Sprintf(format, args...))
		return rec
	}
	if strings.TrimSpace(sc.Task) == "" {
		return fail("scenario has no task (BrowserGym task id, e.g. miniwob.click-button)")
	}
	if e.LLM.BaseURL == "" || e.LLM.Model == "" {
		return fail("LLM endpoint not configured: set UA_EVAL_LLM_BASE_URL and UA_EVAL_LLM_MODEL")
	}
	cap := sc.MaxSteps
	if cap <= 0 {
		cap = miniwobDefaultCap
		if e.DefaultCap > 0 {
			cap = e.DefaultCap
		}
	}

	var sr gymStartReply
	if err := e.postJSON(ctx, "/start", map[string]string{"task": sc.Task}, &sr); err != nil {
		return fail("gym start: %v", err)
	}
	defer func() {
		// A failed task must free its page: close regardless of outcome.
		ctxClose, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = e.postJSON(ctxClose, "/close", map[string]string{}, nil)
	}()

	history := []chatMessage{
		{Role: "system", Content: e.systemPrompt()},
		{Role: "user", Content: sr.Obs},
	}
	var tokensIn, tokensOut int
	var lastReward float64
	var done bool

	for step := 1; step <= cap && !done; step++ {
		rec.Steps = step
		history = trimHistory(history)
		reply, err := e.chat(ctx, history)
		if err != nil {
			return fail("step %d LLM call: %v", step, err)
		}
		tokensIn += reply.Usage.PromptTokens
		tokensOut += reply.Usage.CompletionTokens
		action := extractAction(reply.Content())
		if action == "" {
			return fail("step %d: model produced no action line", step)
		}
		var stepReply gymStepReply
		if err := e.postJSON(ctx, "/step", map[string]any{"action": action}, &stepReply); err != nil {
			return fail("step %d gym: %v", step, err)
		}
		lastReward = stepReply.Reward
		done = stepReply.Done
		history = append(history,
			chatMessage{Role: "assistant", Content: reply.Content()},
			chatMessage{Role: "user", Content: stepReply.Obs},
		)
	}

	rec.DurationSec = time.Since(start).Seconds()
	rec.Evidence["reward"] = lastReward
	rec.Evidence["tokens_in"] = tokensIn
	rec.Evidence["tokens_out"] = tokensOut
	rec.Evidence["steps_capped"] = !done
	if !done {
		rec.Errors = append(rec.Errors, fmt.Sprintf("step budget %d spent before the task finished", cap))
		return rec
	}
	rec.OK = lastReward == 1
	if !rec.OK {
		rec.Errors = append(rec.Errors, fmt.Sprintf("task finished with reward %.2f (want 1.00)", lastReward))
	}
	return rec
}

// systemPrompt returns the arena's grammar prompt (overridable; the
// default teaches the miniwob action set).
func (e *MiniWobExecutor) systemPrompt() string {
	if e.SystemPrompt != "" {
		return e.SystemPrompt
	}
	return miniwobSystemPrompt
}

// History bound: every step appends a full observation (the whole axtree,
// up to the obs budget), so an unbounded history overflows the model's
// context on long-horizon arenas — the first WebArena slice 400ed at
// ~step 26. Keep the system prompt, the goal-bearing first observation
// and the most recent turns; the omitted middle collapses to one note.
const (
	historyKeepTail = 8 // most recent messages kept verbatim
	historyTrimNote = "[... earlier steps omitted ...]"
)

// trimHistory bounds the conversation when it exceeds system + first
// observation + historyKeepTail messages.
func trimHistory(history []chatMessage) []chatMessage {
	const head = 2 // system + first user (goal + first tree)
	if len(history) <= head+1+historyKeepTail {
		return history
	}
	out := make([]chatMessage, 0, head+1+historyKeepTail)
	out = append(out, history[:head]...)
	out = append(out, chatMessage{Role: "user", Content: historyTrimNote})
	out = append(out, history[len(history)-historyKeepTail:]...)
	return out
}

// chatMessage is one turn of the loop's conversation.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chat calls the configured OpenAI-compatible endpoint once.
func (e *MiniWobExecutor) chat(ctx context.Context, msgs []chatMessage) (*chatReply, error) {
	body, err := json.Marshal(map[string]any{
		"model":      e.LLM.Model,
		"max_tokens": 512,
		"messages":   msgs,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(e.LLM.BaseURL, "/")+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.LLM.APIKeyEnv != "" {
		if key := os.Getenv(e.LLM.APIKeyEnv); key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}
	if e.LLM.AgentHeader != "" {
		req.Header.Set("X-Agent", e.LLM.AgentHeader)
	}
	resp, err := e.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("LLM endpoint answered %d: %.200s", resp.StatusCode, raw)
	}
	var out chatReply
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("LLM reply decode: %w", err)
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("LLM reply has no choices")
	}
	return &out, nil
}

// postJSON posts one JSON object to the sidecar and decodes reply into
// out (when out is non-nil).
func (e *MiniWobExecutor) postJSON(ctx context.Context, path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(e.GymURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %d: %.200s", path, resp.StatusCode, raw)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// Content is the reply's message text (the executor only ever reads
// choice zero).
func (r *chatReply) Content() string {
	if len(r.Choices) == 0 {
		return ""
	}
	return r.Choices[0].Message.Content
}

// extractAction pulls the first BrowserGym bid string (e.g. `click('e12')`,
// `fill('e5', 'Bob')`, `stop()`) out of a model reply. Thinking models
// wrap answers in prose or <think> blocks; the action grammar is line-
// anchored so prose cannot smuggle in a second action.
func extractAction(content string) string {
	text := content
	if i := strings.LastIndex(content, "</think>"); i >= 0 {
		text = content[i+len("</think>"):]
	}
	for _, line := range strings.Split(text, "\n") {
		low := strings.ToLower(line)
		// The verb must be called with a paren, not merely mentioned:
		// "click the button" stays prose, "Sure! fill('e5', 'x')" is an
		// action. When several verbs appear, the LEFTMOST paren call is
		// the action — fill('e5', 'click(x)') is a fill whose text
		// happens to look like a call, never a click.
		best := -1
		for _, verb := range []string{"click", "fill", "scroll", "press", "hover", "drag", "move", "goto", "new_tab", "tab_close", "send_msg_to_user", "stop", "report"} {
			if idx := strings.Index(low, verb+"("); idx >= 0 && (best < 0 || idx < best) {
				best = idx
			}
		}
		if best >= 0 {
			return strings.TrimSpace(line[best:])
		}
	}
	return ""
}
