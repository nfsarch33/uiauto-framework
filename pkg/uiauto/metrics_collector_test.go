package uiauto

import (
	"path/filepath"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func setupMetricsTest(t *testing.T) (*MetricsCollector, *Metrics, *MemberAgent) {
	t.Helper()
	skipWithoutBrowser(t)

	dir := t.TempDir()
	patternFile := filepath.Join(dir, "patterns.json")

	agent, err := NewMemberAgent(MemberAgentConfig{
		Headless:    true,
		PatternFile: patternFile,
	})
	if err != nil {
		t.Fatalf("NewMemberAgent: %v", err)
	}

	reg := prometheus.NewRegistry()
	prom := NewMetrics(reg)
	collector := NewMetricsCollector(prom, agent)

	return collector, prom, agent
}

func TestMetricsCollector_InitialCollect(t *testing.T) {
	skipWithoutBrowser(t)
	collector, prom, agent := setupMetricsTest(t)
	defer agent.Close()

	collector.Collect()

	m := &dto.Metric{}
	if err := prom.HealerAttemptsTotal.Write(m); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := m.GetCounter().GetValue(); got != 0 {
		t.Errorf("expected 0 healer attempts, got %v", got)
	}
}

func TestNewMetrics_RegistersTwicePanics(t *testing.T) {
	reg := prometheus.NewRegistry()
	_ = NewMetrics(reg)

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate registration")
		}
	}()
	_ = NewMetrics(reg)
}

func TestMetricsCollector_DeltaAccumulation(t *testing.T) {
	reg := prometheus.NewRegistry()
	prom := NewMetrics(reg)

	snap1 := ExecutorMetricsSnapshot{SuccessActions: 5, FailedActions: 2, CacheHits: 3, CacheMisses: 1}
	snap2 := ExecutorMetricsSnapshot{SuccessActions: 8, FailedActions: 3, CacheHits: 6, CacheMisses: 2}

	c := &MetricsCollector{prom: prom}
	c.collectExecutor(snap1)
	c.collectExecutor(snap2)

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}

	totals := map[string]float64{}
	for _, mf := range mfs {
		if mf.GetName() == "uiauto_executor_actions_total" {
			for _, m := range mf.GetMetric() {
				for _, lp := range m.GetLabel() {
					totals[lp.GetValue()] += m.GetCounter().GetValue()
				}
			}
		}
	}

	if totals["success"] != 8 {
		t.Errorf("expected 8 success actions, got %v", totals["success"])
	}
	if totals["failure"] != 3 {
		t.Errorf("expected 3 failed actions, got %v", totals["failure"])
	}
}

func TestMetricsCollector_HealerDeltas(t *testing.T) {
	reg := prometheus.NewRegistry()
	prom := NewMetrics(reg)

	c := &MetricsCollector{prom: prom}

	c.collectHealer(HealerMetrics{TotalAttempts: 2, SuccessfulHeals: 1, FailedHeals: 1, FingerprintHeals: 1})
	c.collectHealer(HealerMetrics{TotalAttempts: 5, SuccessfulHeals: 3, FailedHeals: 2, FingerprintHeals: 1, StructuralHeals: 2})

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}

	for _, mf := range mfs {
		switch mf.GetName() {
		case "uiauto_healer_attempts_total":
			if got := mf.GetMetric()[0].GetCounter().GetValue(); got != 5 {
				t.Errorf("healer attempts: expected 5, got %v", got)
			}
		case "uiauto_healer_method_total":
			for _, m := range mf.GetMetric() {
				for _, lp := range m.GetLabel() {
					if lp.GetValue() == "structural" && m.GetCounter().GetValue() != 2 {
						t.Errorf("structural heals: expected 2, got %v", m.GetCounter().GetValue())
					}
				}
			}
		}
	}
}

func TestMetricsCollector_RouterDeltas(t *testing.T) {
	reg := prometheus.NewRegistry()
	prom := NewMetrics(reg)

	c := &MetricsCollector{prom: prom}

	c.collectRouter(RouterMetrics{LightAttempts: 10, SmartAttempts: 2, Promotions: 1})
	c.collectRouter(RouterMetrics{LightAttempts: 15, SmartAttempts: 5, Promotions: 3})

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}

	for _, mf := range mfs {
		if mf.GetName() == "uiauto_router_promotions_total" {
			if got := mf.GetMetric()[0].GetCounter().GetValue(); got != 3 {
				t.Errorf("promotions: expected 3, got %v", got)
			}
		}
	}
}

func TestMetricsCollector_VLMDeltas(t *testing.T) {
	reg := prometheus.NewRegistry()
	prom := NewMetrics(reg)

	c := &MetricsCollector{prom: prom}

	c.collectVLM(VLMMetrics{SuccessCalls: 4, FailedCalls: 1})
	c.collectVLM(VLMMetrics{SuccessCalls: 7, FailedCalls: 2})

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}

	totals := map[string]float64{}
	for _, mf := range mfs {
		if mf.GetName() == "uiauto_vlm_calls_total" {
			for _, m := range mf.GetMetric() {
				for _, lp := range m.GetLabel() {
					totals[lp.GetValue()] += m.GetCounter().GetValue()
				}
			}
		}
	}

	if totals["success"] != 7 {
		t.Errorf("vlm success: expected 7, got %v", totals["success"])
	}
	if totals["failure"] != 2 {
		t.Errorf("vlm failure: expected 2, got %v", totals["failure"])
	}
}
