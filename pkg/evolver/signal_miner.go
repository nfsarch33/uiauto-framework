package evolver

import (
	"fmt"
	"time"
)

// Signal represents a detected pattern in execution traces that may warrant
// an evolutionary response.
type Signal struct {
	ID                string         `json:"id"`
	Type              SignalType     `json:"type"`
	Severity          SignalSeverity `json:"severity"`
	Description       string         `json:"description"`
	TraceIDs          []string       `json:"trace_ids"`
	DetectedAt        time.Time      `json:"detected_at"`
	SuggestedMutation string         `json:"suggested_mutation,omitempty"`
}

// SignalType classifies the kind of anomaly detected.
type SignalType string

// SignalType values.
const (
	SignalRepeatedFailure SignalType = "repeated_failure"
	SignalHighLatency     SignalType = "high_latency"
	SignalCostSpike       SignalType = "cost_spike"
	SignalBehavioralDrift SignalType = "behavioral_drift"
	SignalLowSuccessRate  SignalType = "low_success_rate"
)

// SignalSeverity indicates how urgently a signal should be addressed.
type SignalSeverity string

// SignalSeverity values.
const (
	SeverityInfo     SignalSeverity = "info"
	SeverityWarning  SignalSeverity = "warning"
	SeverityCritical SignalSeverity = "critical"
)

// SignalMinerConfig holds thresholds for signal detection.
type SignalMinerConfig struct {
	RepeatedFailureThreshold int
	HighLatencyThresholdMs   float64
	CostSpikeMultiplier      float64
	LowSuccessRateThreshold  float64
	WindowSize               int
}

// DefaultSignalMinerConfig returns sensible defaults.
func DefaultSignalMinerConfig() SignalMinerConfig {
	return SignalMinerConfig{
		RepeatedFailureThreshold: 3,
		HighLatencyThresholdMs:   5000,
		CostSpikeMultiplier:      3.0,
		LowSuccessRateThreshold:  0.5,
		WindowSize:               100,
	}
}

// SignalMiner scans execution traces for anomalous patterns that should
// trigger evolutionary mutations.
type SignalMiner struct {
	cfg     SignalMinerConfig
	counter int
}

// NewSignalMiner creates a miner with the given configuration.
func NewSignalMiner(cfg SignalMinerConfig) *SignalMiner {
	return &SignalMiner{cfg: cfg}
}

// Mine processes a batch of traces and returns detected signals.
func (sm *SignalMiner) Mine(traces []ExecutionTrace) []Signal {
	var signals []Signal

	signals = append(signals, sm.detectRepeatedFailures(traces)...)
	signals = append(signals, sm.detectHighLatency(traces)...)
	signals = append(signals, sm.detectCostSpikes(traces)...)
	signals = append(signals, sm.detectLowSuccessRate(traces)...)

	return signals
}

func (sm *SignalMiner) nextID() string {
	sm.counter++
	return fmt.Sprintf("sig-%06d", sm.counter)
}

func (sm *SignalMiner) detectRepeatedFailures(traces []ExecutionTrace) []Signal {
	errCounts := make(map[string][]string)
	for _, t := range traces {
		if !t.Success && t.ErrorMsg != "" {
			errCounts[t.ErrorMsg] = append(errCounts[t.ErrorMsg], t.ID)
		}
	}

	var signals []Signal
	for errMsg, ids := range errCounts {
		if len(ids) >= sm.cfg.RepeatedFailureThreshold {
			severity := SeverityWarning
			if len(ids) >= sm.cfg.RepeatedFailureThreshold*2 {
				severity = SeverityCritical
			}
			signals = append(signals, Signal{
				ID:                sm.nextID(),
				Type:              SignalRepeatedFailure,
				Severity:          severity,
				Description:       fmt.Sprintf("error repeated %d times: %s", len(ids), truncate(errMsg, 100)),
				TraceIDs:          ids,
				DetectedAt:        time.Now().UTC(),
				SuggestedMutation: "add retry/fallback or fix root cause",
			})
		}
	}
	return signals
}

func (sm *SignalMiner) detectHighLatency(traces []ExecutionTrace) []Signal {
	var slow []string
	for _, t := range traces {
		if t.LatencyMs > sm.cfg.HighLatencyThresholdMs {
			slow = append(slow, t.ID)
		}
	}
	if len(slow) == 0 {
		return nil
	}

	severity := SeverityInfo
	ratio := float64(len(slow)) / float64(len(traces))
	if ratio > 0.3 {
		severity = SeverityWarning
	}
	if ratio > 0.5 {
		severity = SeverityCritical
	}

	return []Signal{{
		ID:                sm.nextID(),
		Type:              SignalHighLatency,
		Severity:          severity,
		Description:       fmt.Sprintf("%d/%d traces exceed %.0fms latency threshold", len(slow), len(traces), sm.cfg.HighLatencyThresholdMs),
		TraceIDs:          slow,
		DetectedAt:        time.Now().UTC(),
		SuggestedMutation: "optimise prompt, cache, or switch to lighter model tier",
	}}
}

func (sm *SignalMiner) detectCostSpikes(traces []ExecutionTrace) []Signal {
	if len(traces) < 2 {
		return nil
	}

	var totalCost float64
	for _, t := range traces {
		totalCost += t.CostUSD
	}
	avgCost := totalCost / float64(len(traces))

	var spiked []string
	for _, t := range traces {
		if avgCost > 0 && t.CostUSD > avgCost*sm.cfg.CostSpikeMultiplier {
			spiked = append(spiked, t.ID)
		}
	}
	if len(spiked) == 0 {
		return nil
	}

	return []Signal{{
		ID:                sm.nextID(),
		Type:              SignalCostSpike,
		Severity:          SeverityWarning,
		Description:       fmt.Sprintf("%d traces exceed %.1fx average cost ($%.4f avg)", len(spiked), sm.cfg.CostSpikeMultiplier, avgCost),
		TraceIDs:          spiked,
		DetectedAt:        time.Now().UTC(),
		SuggestedMutation: "route to cheaper model tier or optimise token usage",
	}}
}

func (sm *SignalMiner) detectLowSuccessRate(traces []ExecutionTrace) []Signal {
	if len(traces) == 0 {
		return nil
	}

	taskSuccess := make(map[string]struct{ ok, total int })
	for _, t := range traces {
		s := taskSuccess[t.TaskName]
		s.total++
		if t.Success {
			s.ok++
		}
		taskSuccess[t.TaskName] = s
	}

	var signals []Signal
	for task, s := range taskSuccess {
		if s.total < 3 {
			continue
		}
		rate := float64(s.ok) / float64(s.total)
		if rate < sm.cfg.LowSuccessRateThreshold {
			signals = append(signals, Signal{
				ID:                sm.nextID(),
				Type:              SignalLowSuccessRate,
				Severity:          SeverityCritical,
				Description:       fmt.Sprintf("task %q has %.0f%% success rate (%d/%d)", task, rate*100, s.ok, s.total),
				DetectedAt:        time.Now().UTC(),
				SuggestedMutation: "review task implementation or add self-healing",
			})
		}
	}
	return signals
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
