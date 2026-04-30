package evolver

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFeedbackLoop_SignalToAction(t *testing.T) {
	fl := NewFeedbackLoop()
	fl.IngestSignal(FeedbackSignal{
		Source: "domheal", Metric: "repair_failure_rate", Value: 0.6, Threshold: 0.3,
	})
	fl.IngestSignal(FeedbackSignal{
		Source: "vlm_judge", Metric: "f1_score", Value: 0.7, Threshold: 0.8,
	})

	actions := fl.Evaluate(context.Background())
	require.Len(t, actions, 1, "only repair_failure_rate exceeds threshold")
	assert.Equal(t, "domheal", actions[0].SignalSource)
	assert.Contains(t, actions[0].Description, "0.60 exceeds threshold 0.30")
}

func TestFeedbackLoop_ApplyAction(t *testing.T) {
	fl := NewFeedbackLoop()
	fl.IngestSignal(FeedbackSignal{
		Source: "router", Metric: "fallback_rate", Value: 0.5, Threshold: 0.3,
	})
	fl.Evaluate(context.Background())

	fl.ApplyAction(0)
	assert.True(t, fl.Actions()[0].Applied)
}

func TestFeedbackLoop_NoActionsWhenBelowThreshold(t *testing.T) {
	fl := NewFeedbackLoop()
	fl.IngestSignal(FeedbackSignal{
		Source: "pipeline", Metric: "success_rate", Value: 0.2, Threshold: 0.8,
	})

	actions := fl.Evaluate(context.Background())
	assert.Len(t, actions, 0)
}
