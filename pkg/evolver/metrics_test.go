package evolver

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestNewMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	if m == nil {
		t.Fatal("expected non-nil metrics")
	}

	fams, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, f := range fams {
		names[f.GetName()] = true
	}
	for _, want := range []string{
		"evolver_mutations_total",
		"evolver_genes_applied_total",
		"evolver_capsules_created_total",
		"evolver_signal_severity",
		"evolver_engine_runs_total",
		"evolver_engine_run_duration_seconds",
	} {
		if !names[want] {
			t.Errorf("missing metric %s", want)
		}
	}
}

func TestEvolverMetrics_RecordSignals(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	signals := []Signal{
		{Type: SignalRepeatedFailure, Severity: SeverityCritical},
		{Type: SignalRepeatedFailure, Severity: SeverityWarning},
		{Type: SignalHighLatency, Severity: SeverityInfo},
	}
	m.RecordSignals(signals)

	fams, _ := reg.Gather()
	found := false
	for _, f := range fams {
		if f.GetName() == "evolver_signal_severity" {
			found = true
			if len(f.GetMetric()) == 0 {
				t.Error("expected observations")
			}
		}
	}
	if !found {
		t.Error("signal_severity metric not found")
	}
}

func TestEvolverMetrics_RecordMutations(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	mutations := []Mutation{
		{ID: "m1", Status: MutationStatusPending, CreatedAt: time.Now()},
		{ID: "m2", Status: MutationStatusApplied, CreatedAt: time.Now()},
		{ID: "m3", Status: MutationStatusRejected, CreatedAt: time.Now()},
	}
	m.RecordMutations(mutations)

	fams, _ := reg.Gather()
	for _, f := range fams {
		if f.GetName() != "evolver_mutations_total" {
			continue
		}
		statusCounts := map[string]float64{}
		for _, met := range f.GetMetric() {
			for _, lp := range met.GetLabel() {
				if lp.GetName() == "status" {
					statusCounts[lp.GetValue()] += met.GetCounter().GetValue()
				}
			}
		}
		if statusCounts["pending"] != 1 {
			t.Errorf("pending=%v want 1", statusCounts["pending"])
		}
		if statusCounts["applied"] != 1 {
			t.Errorf("applied=%v want 1", statusCounts["applied"])
		}
		if statusCounts["rejected"] != 1 {
			t.Errorf("rejected=%v want 1", statusCounts["rejected"])
		}
	}
}

func TestEvolverMetrics_DuplicateRegistrationPanics(t *testing.T) {
	reg := prometheus.NewRegistry()
	NewMetrics(reg)
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate registration")
		}
	}()
	NewMetrics(reg)
}

func TestSeverityToFloat(t *testing.T) {
	cases := []struct {
		sev  SignalSeverity
		want float64
	}{
		{SeverityInfo, 0},
		{SeverityWarning, 1},
		{SeverityCritical, 2},
		{SignalSeverity("unknown"), 0},
	}
	for _, tc := range cases {
		got := severityToFloat(tc.sev)
		if got != tc.want {
			t.Errorf("severityToFloat(%s)=%v want %v", tc.sev, got, tc.want)
		}
	}
}

func counterValue(c prometheus.Counter) float64 {
	var m dto.Metric
	_ = c.(prometheus.Metric).Write(&m)
	return m.GetCounter().GetValue()
}

func TestEvolverMetrics_EngineCounters(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	m.EngineRunsTotal.Inc()
	m.EngineRunsTotal.Inc()
	m.GenesAppliedTotal.Inc()
	m.CapsulesCreated.Inc()

	if v := counterValue(m.EngineRunsTotal); v != 2 {
		t.Errorf("EngineRunsTotal=%v want 2", v)
	}
	if v := counterValue(m.GenesAppliedTotal); v != 1 {
		t.Errorf("GenesAppliedTotal=%v want 1", v)
	}
	if v := counterValue(m.CapsulesCreated); v != 1 {
		t.Errorf("CapsulesCreated=%v want 1", v)
	}
}
