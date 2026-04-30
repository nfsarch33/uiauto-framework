package evolver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ConfigMutator Tests ---

func TestConfigMutatorApply(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	before := json.RawMessage(`{"old": true}`)
	after := json.RawMessage(mustJSON(t, map[string]any{
		"path":  path,
		"value": json.RawMessage(`{"new": true}`),
	}))

	mut := Mutation{
		ID:          "mut-cfg-1",
		SignalID:    "sig-1",
		Reasoning:   "test config mutation",
		Strategy:    ModeRepairOnly,
		BeforeState: before,
		AfterState:  after,
	}

	cm := NewConfigMutator(nil)
	require.True(t, cm.Supports(mut), "ConfigMutator should support repair-only")

	result, err := cm.Apply(context.Background(), mut)
	require.NoError(t, err)
	assert.True(t, result.Applied)
	assert.False(t, result.AppliedAt.IsZero())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, true, got["new"])

	require.NoError(t, result.RollbackFn(context.Background()))
	data, _ = os.ReadFile(path)
	assert.Equal(t, `{"old": true}`, string(data))
}

func TestConfigMutator_NilAfterState(t *testing.T) {
	cm := NewConfigMutator(nil)
	mut := Mutation{ID: "nil-after", SignalID: "s", Reasoning: "test", Strategy: ModeRepairOnly}
	_, err := cm.Apply(context.Background(), mut)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "after_state is required")
}

func TestConfigMutator_InvalidJSON(t *testing.T) {
	cm := NewConfigMutator(nil)
	mut := Mutation{
		ID: "bad-json", SignalID: "s", Reasoning: "test",
		Strategy:   ModeRepairOnly,
		AfterState: json.RawMessage(`{invalid`),
	}
	_, err := cm.Apply(context.Background(), mut)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse after_state")
}

func TestConfigMutator_MissingPath(t *testing.T) {
	cm := NewConfigMutator(nil)
	after := json.RawMessage(mustJSON(t, map[string]any{
		"value": json.RawMessage(`{"x":1}`),
	}))
	mut := Mutation{
		ID: "no-path", SignalID: "s", Reasoning: "test",
		Strategy: ModeRepairOnly, AfterState: after,
	}
	_, err := cm.Apply(context.Background(), mut)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "path is required")
}

func TestConfigMutator_WriteError(t *testing.T) {
	cm := NewConfigMutator(nil)
	after := json.RawMessage(mustJSON(t, map[string]any{
		"path":  "/nonexistent/dir/config.json",
		"value": json.RawMessage(`{"x":1}`),
	}))
	mut := Mutation{
		ID: "write-err", SignalID: "s", Reasoning: "test",
		Strategy: ModeRepairOnly, AfterState: after,
	}
	_, err := cm.Apply(context.Background(), mut)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "write file")
}

func TestConfigMutator_RollbackRemovesWhenNoBeforeState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new-config.json")

	after := json.RawMessage(mustJSON(t, map[string]any{
		"path":  path,
		"value": json.RawMessage(`{"created":true}`),
	}))
	mut := Mutation{
		ID: "rollback-remove", SignalID: "s", Reasoning: "test",
		Strategy: ModeRepairOnly, AfterState: after,
	}

	cm := NewConfigMutator(nil)
	result, err := cm.Apply(context.Background(), mut)
	require.NoError(t, err)
	assert.True(t, result.Applied)

	_, err = os.Stat(path)
	require.NoError(t, err, "file should exist after apply")

	require.NoError(t, result.RollbackFn(context.Background()))
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err), "file should be removed on rollback with nil before_state")
}

func TestConfigMutator_SupportsHarden(t *testing.T) {
	cm := NewConfigMutator(nil)
	assert.True(t, cm.Supports(Mutation{Strategy: ModeHarden}))
}

func TestConfigMutator_DoesNotSupportBalanced(t *testing.T) {
	cm := NewConfigMutator(nil)
	assert.False(t, cm.Supports(Mutation{Strategy: ModeBalanced}))
	assert.False(t, cm.Supports(Mutation{Strategy: ModeInnovate}))
}

// --- ThresholdMutator Tests ---

func TestThresholdMutatorApply(t *testing.T) {
	initial := map[string]float64{"confidence_threshold": 0.6}
	tm := NewThresholdMutator(initial, nil)

	after := json.RawMessage(`{"key":"confidence_threshold","value":0.8}`)
	mut := Mutation{
		ID: "mut-thresh-1", SignalID: "sig-2", Reasoning: "boost confidence",
		Strategy: ModeBalanced, AfterState: after,
	}

	require.True(t, tm.Supports(mut))

	result, err := tm.Apply(context.Background(), mut)
	require.NoError(t, err)
	assert.True(t, result.Applied)
	assert.Equal(t, 0.8, tm.Get("confidence_threshold"))
	assert.Contains(t, result.Message, "0.6000")
	assert.Contains(t, result.Message, "0.8000")

	require.NoError(t, result.RollbackFn(context.Background()))
	assert.Equal(t, 0.6, tm.Get("confidence_threshold"))
}

func TestThresholdMutator_InvalidJSON(t *testing.T) {
	tm := NewThresholdMutator(nil, nil)
	mut := Mutation{
		ID: "bad-json", SignalID: "s", Reasoning: "test",
		Strategy: ModeBalanced, AfterState: json.RawMessage(`not-json`),
	}
	_, err := tm.Apply(context.Background(), mut)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "threshold mutator: parse")
}

func TestThresholdMutator_NewKey(t *testing.T) {
	tm := NewThresholdMutator(nil, nil)
	after := json.RawMessage(`{"key":"new_threshold","value":0.95}`)
	mut := Mutation{
		ID: "new-key", SignalID: "s", Reasoning: "test",
		Strategy: ModeBalanced, AfterState: after,
	}
	result, err := tm.Apply(context.Background(), mut)
	require.NoError(t, err)
	assert.True(t, result.Applied)
	assert.Equal(t, 0.95, tm.Get("new_threshold"))

	require.NoError(t, result.RollbackFn(context.Background()))
	assert.Equal(t, 0.0, tm.Get("new_threshold"), "rollback restores zero for new key")
}

func TestThresholdMutator_SupportsInnovate(t *testing.T) {
	tm := NewThresholdMutator(nil, nil)
	assert.True(t, tm.Supports(Mutation{Strategy: ModeInnovate}))
}

func TestThresholdMutator_DoesNotSupportRepairOnly(t *testing.T) {
	tm := NewThresholdMutator(nil, nil)
	assert.False(t, tm.Supports(Mutation{Strategy: ModeRepairOnly}))
	assert.False(t, tm.Supports(Mutation{Strategy: ModeHarden}))
}

func TestThresholdMutator_DoesNotMutateOriginalMap(t *testing.T) {
	original := map[string]float64{"x": 1.0}
	tm := NewThresholdMutator(original, nil)
	after := json.RawMessage(`{"key":"x","value":2.0}`)
	mut := Mutation{
		ID: "isolate", SignalID: "s", Reasoning: "test",
		Strategy: ModeBalanced, AfterState: after,
	}
	_, err := tm.Apply(context.Background(), mut)
	require.NoError(t, err)
	assert.Equal(t, 1.0, original["x"], "original map must not be modified")
	assert.Equal(t, 2.0, tm.Get("x"))
}

// --- PromptMutator Tests ---

func TestPromptMutatorApply(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prompt.md")

	after := json.RawMessage(mustJSON(t, map[string]any{
		"path":    path,
		"content": "You are a UI test automation agent.",
	}))

	mut := Mutation{
		ID: "mut-prompt-1", SignalID: "sig-3", Reasoning: "improve prompt",
		Strategy: ModeInnovate, AfterState: after,
	}

	pm := NewPromptMutator(nil)
	require.True(t, pm.Supports(mut))

	result, err := pm.Apply(context.Background(), mut)
	require.NoError(t, err)
	assert.True(t, result.Applied)

	data, _ := os.ReadFile(path)
	assert.Equal(t, "You are a UI test automation agent.", string(data))
}

func TestPromptMutator_InvalidJSON(t *testing.T) {
	pm := NewPromptMutator(nil)
	mut := Mutation{
		ID: "bad-json", SignalID: "s", Reasoning: "test",
		Strategy: ModeInnovate, AfterState: json.RawMessage(`{bad`),
	}
	_, err := pm.Apply(context.Background(), mut)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "prompt mutator: parse")
}

func TestPromptMutator_WriteError(t *testing.T) {
	pm := NewPromptMutator(nil)
	after := json.RawMessage(mustJSON(t, map[string]any{
		"path":    "/nonexistent/dir/prompt.md",
		"content": "test",
	}))
	mut := Mutation{
		ID: "write-err", SignalID: "s", Reasoning: "test",
		Strategy: ModeInnovate, AfterState: after,
	}
	_, err := pm.Apply(context.Background(), mut)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "prompt mutator: write")
}

func TestPromptMutator_RollbackRemovesWhenNoBeforeState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new-prompt.md")
	after := json.RawMessage(mustJSON(t, map[string]any{
		"path": path, "content": "new prompt",
	}))
	mut := Mutation{
		ID: "rollback-rm", SignalID: "s", Reasoning: "test",
		Strategy: ModeInnovate, AfterState: after,
	}
	pm := NewPromptMutator(nil)
	result, err := pm.Apply(context.Background(), mut)
	require.NoError(t, err)

	require.NoError(t, result.RollbackFn(context.Background()))
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err))
}

func TestPromptMutator_RollbackRestoresOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.md")
	require.NoError(t, os.WriteFile(path, []byte("original content"), 0644))

	before := json.RawMessage("original content")
	after := json.RawMessage(mustJSON(t, map[string]any{
		"path": path, "content": "updated content",
	}))
	mut := Mutation{
		ID: "rollback-restore", SignalID: "s", Reasoning: "test",
		Strategy: ModeInnovate, BeforeState: before, AfterState: after,
	}
	pm := NewPromptMutator(nil)
	result, err := pm.Apply(context.Background(), mut)
	require.NoError(t, err)

	data, _ := os.ReadFile(path)
	assert.Equal(t, "updated content", string(data))

	require.NoError(t, result.RollbackFn(context.Background()))
	data, _ = os.ReadFile(path)
	assert.Equal(t, "original content", string(data))
}

func TestPromptMutator_DoesNotSupportRepairOnly(t *testing.T) {
	pm := NewPromptMutator(nil)
	assert.False(t, pm.Supports(Mutation{Strategy: ModeRepairOnly}))
	assert.False(t, pm.Supports(Mutation{Strategy: ModeHarden}))
	assert.False(t, pm.Supports(Mutation{Strategy: ModeBalanced}))
}

// --- Applicator Registry Pattern ---

func TestApplicatorRegistry_RoutesToCorrectMutator(t *testing.T) {
	dir := t.TempDir()
	applicators := []MutationApplicator{
		NewConfigMutator(nil),
		NewThresholdMutator(map[string]float64{"x": 1.0}, nil),
		NewPromptMutator(nil),
	}

	path := filepath.Join(dir, "routed.json")
	after := json.RawMessage(mustJSON(t, map[string]any{
		"path":  path,
		"value": json.RawMessage(`{"routed":true}`),
	}))
	mut := Mutation{
		ID: "registry-test", SignalID: "s", Reasoning: "test routing",
		Strategy: ModeHarden, AfterState: after,
	}

	var applied bool
	for _, a := range applicators {
		if a.Supports(mut) {
			result, err := a.Apply(context.Background(), mut)
			require.NoError(t, err)
			assert.True(t, result.Applied)
			applied = true
			break
		}
	}
	assert.True(t, applied, "at least one applicator should handle harden strategy")

	data, _ := os.ReadFile(path)
	assert.Contains(t, string(data), "routed")
}

func TestApplicatorRegistry_NoMatch(t *testing.T) {
	applicators := []MutationApplicator{
		NewPromptMutator(nil),
	}
	mut := Mutation{Strategy: ModeRepairOnly}

	var handled bool
	for _, a := range applicators {
		if a.Supports(mut) {
			handled = true
		}
	}
	assert.False(t, handled, "PromptMutator should not handle repair-only")
}

// --- ApplyResult Tests ---

func TestApplyResult_AppliedAtIsSet(t *testing.T) {
	cm := NewConfigMutator(nil)
	dir := t.TempDir()
	path := filepath.Join(dir, "ts.json")
	after := json.RawMessage(mustJSON(t, map[string]any{
		"path":  path,
		"value": json.RawMessage(`{}`),
	}))
	mut := Mutation{
		ID: "ts", SignalID: "s", Reasoning: "test",
		Strategy: ModeRepairOnly, AfterState: after,
	}
	result, err := cm.Apply(context.Background(), mut)
	require.NoError(t, err)
	assert.False(t, result.AppliedAt.IsZero(), "AppliedAt must be set")
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
