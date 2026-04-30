package uiauto

import (
	"testing"
	"time"
)

func TestWaitResult_Type(t *testing.T) {
	r := WaitResult{
		Strategy: WaitNetworkIdle,
		Label:    "network_idle",
		Duration: 100 * time.Millisecond,
	}
	if r.Strategy != WaitNetworkIdle {
		t.Errorf("Strategy = %d, want %d", r.Strategy, WaitNetworkIdle)
	}
	if r.Label != "network_idle" {
		t.Errorf("Label = %q, want network_idle", r.Label)
	}
	if r.Err != nil {
		t.Errorf("Err = %v, want nil", r.Err)
	}
}

func TestPageWaiter_WithTargetSelector(t *testing.T) {
	pw := NewPageWaiter(10*time.Second, WaitNetworkIdle|WaitElementVisible).
		WithTargetSelector(".login-button")

	if pw.targetSelector != ".login-button" {
		t.Errorf("targetSelector = %q, want .login-button", pw.targetSelector)
	}
	if pw.continueOnErr {
		t.Error("continueOnErr should be false by default")
	}
}

func TestPageWaiter_FromConfigWithTargetSelector(t *testing.T) {
	cfg := WaitConfig{
		Timeout:        5 * time.Second,
		Strategy:       WaitNetworkIdle | WaitDOMStable | WaitElementVisible,
		StableFor:      300 * time.Millisecond,
		PollInterval:   50 * time.Millisecond,
		ContinueOnErr:  true,
		TargetSelector: "#main-content",
	}
	pw := NewPageWaiterFromConfig(cfg)

	if pw.timeout != 5*time.Second {
		t.Errorf("timeout = %v, want 5s", pw.timeout)
	}
	if pw.strategy != WaitNetworkIdle|WaitDOMStable|WaitElementVisible {
		t.Errorf("strategy = %d, unexpected", pw.strategy)
	}
	if pw.stableFor != 300*time.Millisecond {
		t.Errorf("stableFor = %v, want 300ms", pw.stableFor)
	}
	if pw.pollInterval != 50*time.Millisecond {
		t.Errorf("pollInterval = %v, want 50ms", pw.pollInterval)
	}
	if !pw.continueOnErr {
		t.Error("continueOnErr should be true")
	}
	if pw.targetSelector != "#main-content" {
		t.Errorf("targetSelector = %q, want #main-content", pw.targetSelector)
	}
}

func TestDefaultWaitConfig_V2Fields(t *testing.T) {
	cfg := DefaultWaitConfig()
	if cfg.Timeout != 15*time.Second {
		t.Errorf("Timeout = %v, want 15s", cfg.Timeout)
	}
	if cfg.Strategy != WaitNetworkIdle|WaitDOMStable {
		t.Errorf("Strategy = %d, want %d", cfg.Strategy, WaitNetworkIdle|WaitDOMStable)
	}
	if cfg.ContinueOnErr {
		t.Error("ContinueOnErr should be false by default")
	}
	if cfg.TargetSelector != "" {
		t.Errorf("TargetSelector = %q, want empty", cfg.TargetSelector)
	}
}

func TestStrategyLabel_Coverage(t *testing.T) {
	tests := []struct {
		strategy WaitStrategy
		want     string
	}{
		{WaitNetworkIdle, "network_idle"},
		{WaitDOMStable, "dom_stable"},
		{WaitNetworkIdle | WaitDOMStable, "network_and_dom"},
		{WaitElementVisible, "custom"},
		{WaitNetworkIdle | WaitElementVisible, "custom"},
		{0, "custom"},
	}

	for _, tt := range tests {
		pw := NewPageWaiter(5*time.Second, tt.strategy)
		got := pw.strategyLabel()
		if got != tt.want {
			t.Errorf("strategyLabel(%d) = %q, want %q", tt.strategy, got, tt.want)
		}
	}
}
