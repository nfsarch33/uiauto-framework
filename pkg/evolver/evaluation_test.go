package evolver

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockJudgeLLM struct {
	response string
	err      error
}

func (m *mockJudgeLLM) Complete(_ context.Context, _ string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.response, nil
}

func testSignal() Signal {
	return Signal{
		ID:          "sig-001",
		Type:        SignalRepeatedFailure,
		Severity:    SeverityCritical,
		Description: "selector #submit-btn failed 5 times in login flow",
	}
}

func testMutation() Mutation {
	return Mutation{
		ID:           "mut-001",
		SignalID:     "sig-001",
		GeneID:       "gene-selector-fix",
		RiskEstimate: RiskLow,
		Reasoning:    "matched gene for selector repair using structural matching with cached patterns",
		Strategy:     ModeBalanced,
		Status:       MutationStatusPending,
	}
}

func TestEvaluationHarness_RuleBased_Pass(t *testing.T) {
	harness := NewEvaluationHarness(DefaultEvaluationConfig(), nil)
	ctx := context.Background()

	result, err := harness.Evaluate(ctx, testMutation(), testSignal())
	require.NoError(t, err)
	assert.True(t, result.Pass, "mutation with gene and good reasoning should pass")
	assert.Greater(t, result.Score, 0.0)
	assert.Equal(t, "rule_based", result.Method)
	assert.NotEmpty(t, result.Reasoning)
	assert.Len(t, result.RubricResults, 4)
}

func TestEvaluationHarness_RuleBased_HighRiskLowScore(t *testing.T) {
	harness := NewEvaluationHarness(DefaultEvaluationConfig(), nil)
	ctx := context.Background()

	mut := testMutation()
	mut.RiskEstimate = RiskHigh
	mut.GeneID = ""
	mut.Reasoning = "fix"

	result, err := harness.Evaluate(ctx, mut, testSignal())
	require.NoError(t, err)
	assert.Less(t, result.Score, 6.0, "high-risk mutation with short reasoning should score low")
}

func TestEvaluationHarness_RubricScoresCorrectly(t *testing.T) {
	harness := NewEvaluationHarness(DefaultEvaluationConfig(), nil)
	ctx := context.Background()

	result, err := harness.Evaluate(ctx, testMutation(), testSignal())
	require.NoError(t, err)

	for _, r := range result.RubricResults {
		assert.Greater(t, r.Score, 0.0, "criterion %s should have positive score", r.Criterion)
		assert.LessOrEqual(t, r.Score, r.MaxScore, "criterion %s score should not exceed max", r.Criterion)
		assert.Greater(t, r.Weight, 0.0, "criterion %s should have weight", r.Criterion)
	}
}

func TestEvaluationHarness_LLMAsJudge(t *testing.T) {
	llm := &mockJudgeLLM{
		response: `{"score": 8.5, "reasoning": "Good mutation with clear root cause analysis"}`,
	}
	cfg := DefaultEvaluationConfig()
	cfg.LLMAsJudge = true

	harness := NewEvaluationHarness(cfg, llm)
	ctx := context.Background()

	result, err := harness.Evaluate(ctx, testMutation(), testSignal())
	require.NoError(t, err)
	assert.Equal(t, "llm_as_judge", result.Method)
	assert.Contains(t, result.Reasoning, "root cause")
}

func TestEvaluationHarness_LLMJudgeError_FallsBackToRules(t *testing.T) {
	llm := &mockJudgeLLM{err: fmt.Errorf("model unavailable")}
	cfg := DefaultEvaluationConfig()
	cfg.LLMAsJudge = true

	harness := NewEvaluationHarness(cfg, llm)
	ctx := context.Background()

	result, err := harness.Evaluate(ctx, testMutation(), testSignal())
	require.NoError(t, err)
	assert.Equal(t, "rule_based", result.Method, "should fall back to rules on LLM error")
}

func TestEvaluationHarness_LLMJudgeInvalidJSON(t *testing.T) {
	llm := &mockJudgeLLM{response: "This is not JSON but a reasonable assessment"}
	cfg := DefaultEvaluationConfig()
	cfg.LLMAsJudge = true

	harness := NewEvaluationHarness(cfg, llm)
	ctx := context.Background()

	result, err := harness.Evaluate(ctx, testMutation(), testSignal())
	require.NoError(t, err)
	assert.Equal(t, "llm_as_judge", result.Method)
}

func TestEvaluationHarness_PassRate(t *testing.T) {
	harness := NewEvaluationHarness(DefaultEvaluationConfig(), nil)
	ctx := context.Background()

	harness.Evaluate(ctx, testMutation(), testSignal())

	failMut := testMutation()
	failMut.ID = "mut-002"
	failMut.RiskEstimate = RiskHigh
	failMut.GeneID = ""
	failMut.Reasoning = "x"
	harness.Evaluate(ctx, failMut, testSignal())

	rate := harness.PassRate()
	assert.Greater(t, rate, 0.0)
	assert.LessOrEqual(t, rate, 1.0)

	results := harness.Results()
	assert.Len(t, results, 2)
}

func TestEvaluationHarness_EmptyPassRate(t *testing.T) {
	harness := NewEvaluationHarness(DefaultEvaluationConfig(), nil)
	assert.Equal(t, 0.0, harness.PassRate())
}

func TestEvaluationHarness_CustomRubric(t *testing.T) {
	cfg := EvaluationConfig{
		PassThreshold: 0.5,
		MaxScore:      100,
		Rubric: []RubricCriterion{
			{Name: "correctness", Description: "fix quality", Weight: 1.0, MaxScore: 100},
		},
	}
	harness := NewEvaluationHarness(cfg, nil)
	ctx := context.Background()

	result, err := harness.Evaluate(ctx, testMutation(), testSignal())
	require.NoError(t, err)
	assert.Equal(t, 100.0, result.MaxScore)
	assert.Len(t, result.RubricResults, 1)
}

func TestEvaluationHarness_EfficiencyBoostForLatencySignal(t *testing.T) {
	harness := NewEvaluationHarness(DefaultEvaluationConfig(), nil)
	ctx := context.Background()

	sig := testSignal()
	sig.Type = SignalHighLatency

	mut := testMutation()
	mut.Reasoning = "switch to lighter model tier with caching enabled"

	result, err := harness.Evaluate(ctx, mut, sig)
	require.NoError(t, err)

	var effScore float64
	for _, r := range result.RubricResults {
		if r.Criterion == "efficiency" {
			effScore = r.Score
			break
		}
	}
	assert.Greater(t, effScore, 5.0, "efficiency should score higher when reasoning mentions cache/lighter")
}
