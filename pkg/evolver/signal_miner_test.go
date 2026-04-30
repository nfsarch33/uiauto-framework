package evolver

import (
	"testing"
	"time"
)

func newTrace(id, task string, success bool, latencyMs, costUSD float64) ExecutionTrace {
	return ExecutionTrace{
		ID:        id,
		TaskName:  task,
		AgentID:   "test-agent",
		StartTime: time.Now().UTC(),
		EndTime:   time.Now().UTC(),
		LatencyMs: latencyMs,
		Success:   success,
		CostUSD:   costUSD,
	}
}

func newFailedTrace(id, task, errMsg string) ExecutionTrace {
	t := newTrace(id, task, false, 100, 0.001)
	t.ErrorMsg = errMsg
	return t
}

func TestSignalMiner_RepeatedFailures(t *testing.T) {
	miner := NewSignalMiner(DefaultSignalMinerConfig())
	traces := []ExecutionTrace{
		newFailedTrace("t1", "scrape", "timeout"),
		newFailedTrace("t2", "scrape", "timeout"),
		newFailedTrace("t3", "scrape", "timeout"),
	}

	signals := miner.Mine(traces)
	found := false
	for _, s := range signals {
		if s.Type == SignalRepeatedFailure {
			found = true
			if len(s.TraceIDs) != 3 {
				t.Errorf("expected 3 trace IDs, got %d", len(s.TraceIDs))
			}
			if s.Severity != SeverityWarning {
				t.Errorf("expected warning severity for 3 failures, got %s", s.Severity)
			}
		}
	}
	if !found {
		t.Fatal("expected repeated_failure signal")
	}
}

func TestSignalMiner_RepeatedFailures_Critical(t *testing.T) {
	miner := NewSignalMiner(DefaultSignalMinerConfig())
	var traces []ExecutionTrace
	for i := 0; i < 6; i++ {
		traces = append(traces, newFailedTrace(
			"t"+string(rune('0'+i)), "scrape", "connection refused",
		))
	}

	signals := miner.Mine(traces)
	for _, s := range signals {
		if s.Type == SignalRepeatedFailure && s.Severity != SeverityCritical {
			t.Errorf("expected critical severity for 6 failures, got %s", s.Severity)
		}
	}
}

func TestSignalMiner_HighLatency(t *testing.T) {
	miner := NewSignalMiner(DefaultSignalMinerConfig())
	traces := []ExecutionTrace{
		newTrace("t1", "fast", true, 100, 0.001),
		newTrace("t2", "slow", true, 10000, 0.002),
		newTrace("t3", "normal", true, 200, 0.001),
	}

	signals := miner.Mine(traces)
	found := false
	for _, s := range signals {
		if s.Type == SignalHighLatency {
			found = true
			if len(s.TraceIDs) != 1 {
				t.Errorf("expected 1 slow trace, got %d", len(s.TraceIDs))
			}
		}
	}
	if !found {
		t.Fatal("expected high_latency signal")
	}
}

func TestSignalMiner_CostSpike(t *testing.T) {
	miner := NewSignalMiner(DefaultSignalMinerConfig())
	traces := []ExecutionTrace{
		newTrace("t1", "cheap", true, 100, 0.001),
		newTrace("t2", "cheap", true, 100, 0.001),
		newTrace("t3", "cheap", true, 100, 0.001),
		newTrace("t4", "cheap", true, 100, 0.001),
		newTrace("t5", "expensive", true, 100, 0.100),
	}

	signals := miner.Mine(traces)
	found := false
	for _, s := range signals {
		if s.Type == SignalCostSpike {
			found = true
		}
	}
	if !found {
		t.Fatal("expected cost_spike signal")
	}
}

func TestSignalMiner_LowSuccessRate(t *testing.T) {
	miner := NewSignalMiner(DefaultSignalMinerConfig())
	traces := []ExecutionTrace{
		newTrace("t1", "flaky-task", false, 100, 0.001),
		newTrace("t2", "flaky-task", false, 100, 0.001),
		newTrace("t3", "flaky-task", false, 100, 0.001),
		newTrace("t4", "flaky-task", true, 100, 0.001),
	}

	signals := miner.Mine(traces)
	found := false
	for _, s := range signals {
		if s.Type == SignalLowSuccessRate {
			found = true
			if s.Severity != SeverityCritical {
				t.Errorf("expected critical severity, got %s", s.Severity)
			}
		}
	}
	if !found {
		t.Fatal("expected low_success_rate signal")
	}
}

func TestSignalMiner_NoSignals(t *testing.T) {
	miner := NewSignalMiner(DefaultSignalMinerConfig())
	traces := []ExecutionTrace{
		newTrace("t1", "good", true, 100, 0.001),
		newTrace("t2", "good", true, 200, 0.001),
	}

	signals := miner.Mine(traces)
	if len(signals) != 0 {
		t.Errorf("expected no signals, got %d", len(signals))
	}
}

func TestSignalMiner_EmptyTraces(t *testing.T) {
	miner := NewSignalMiner(DefaultSignalMinerConfig())
	signals := miner.Mine(nil)
	if len(signals) != 0 {
		t.Errorf("expected no signals from nil, got %d", len(signals))
	}
}
