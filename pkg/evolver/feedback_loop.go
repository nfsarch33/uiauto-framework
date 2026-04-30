package evolver

import (
	"context"
	"fmt"
	"time"
)

// FeedbackSignal represents a metrics-derived signal for self-improvement.
type FeedbackSignal struct {
	Source    string    `json:"source"`
	Metric    string    `json:"metric"`
	Value     float64   `json:"value"`
	Threshold float64   `json:"threshold"`
	Severity  string    `json:"severity"`
	Timestamp time.Time `json:"timestamp"`
}

// FeedbackAction represents a corrective action from a feedback signal.
type FeedbackAction struct {
	SignalSource string `json:"signal_source"`
	ActionType   string `json:"action_type"`
	Description  string `json:"description"`
	Applied      bool   `json:"applied"`
}

// FeedbackLoop implements the metrics->signal->mutate->validate->deploy pipeline.
type FeedbackLoop struct {
	signals []FeedbackSignal
	actions []FeedbackAction
}

// NewFeedbackLoop creates a new feedback loop.
func NewFeedbackLoop() *FeedbackLoop {
	return &FeedbackLoop{}
}

// IngestSignal processes a new metrics signal.
func (fl *FeedbackLoop) IngestSignal(signal FeedbackSignal) {
	if signal.Timestamp.IsZero() {
		signal.Timestamp = time.Now()
	}
	fl.signals = append(fl.signals, signal)
}

// Evaluate checks signals against thresholds and generates actions.
func (fl *FeedbackLoop) Evaluate(_ context.Context) []FeedbackAction {
	var actions []FeedbackAction
	for _, s := range fl.signals {
		if s.Value > s.Threshold {
			action := FeedbackAction{
				SignalSource: s.Source,
				ActionType:   "mutate",
				Description:  fmt.Sprintf("%s: %.2f exceeds threshold %.2f", s.Metric, s.Value, s.Threshold),
				Applied:      false,
			}
			actions = append(actions, action)
		}
	}
	fl.actions = append(fl.actions, actions...)
	return actions
}

// ApplyAction marks an action as applied.
func (fl *FeedbackLoop) ApplyAction(idx int) {
	if idx >= 0 && idx < len(fl.actions) {
		fl.actions[idx].Applied = true
	}
}

// Signals returns all ingested signals.
func (fl *FeedbackLoop) Signals() []FeedbackSignal {
	return fl.signals
}

// Actions returns all generated actions.
func (fl *FeedbackLoop) Actions() []FeedbackAction {
	return fl.actions
}
