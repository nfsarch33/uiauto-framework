package uiauto

import (
	"io"
	"log/slog"
	"testing"
)

// NewMemberAgent + WithMemberLogger + Close: chromedp lazily connects, so
// constructing a headless MemberAgent doesn't actually launch Chrome until
// the first call. This exercises the construction path without a browser.
func TestNewMemberAgent_Headless_Construct(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := MemberAgentConfig{
		Headless:    true,
		PatternFile: t.TempDir() + "/p.json",
	}
	agent, err := NewMemberAgent(cfg, WithMemberLogger(logger))
	if err != nil {
		t.Fatalf("NewMemberAgent: %v", err)
	}
	if agent == nil {
		t.Fatal("agent is nil")
	}
	defer agent.Close()
	if agent.logger != logger {
		t.Error("WithMemberLogger did not apply")
	}

	// targetCircuitBreaker is exercised whenever RunTask processes an action.
	cb := agent.targetCircuitBreaker("test-target")
	if cb == nil {
		t.Error("targetCircuitBreaker should never return nil")
	}
	// Calling again returns the cached breaker.
	cb2 := agent.targetCircuitBreaker("test-target")
	if cb != cb2 {
		t.Error("targetCircuitBreaker should reuse the cached breaker")
	}

	if agent.IsDegraded() {
		t.Error("freshly constructed agent should not be degraded")
	}

	stats := agent.TargetCBStats()
	if _, ok := stats["test-target"]; !ok {
		t.Errorf("expected stats for test-target, got %v", stats)
	}
}

// NewMemberAgent: bad RemoteDebugURL fails fast (no Chrome required).
func TestNewMemberAgent_BadRemote_Errors(t *testing.T) {
	cfg := MemberAgentConfig{
		Headless:       true,
		RemoteDebugURL: "http://127.0.0.1:1",
		PatternFile:    t.TempDir() + "/p.json",
	}
	if _, err := NewMemberAgent(cfg); err == nil {
		t.Error("expected error for unreachable remote-debug-url")
	}
}

// NewMemberAgent with empty PatternFile uses default path (no error).
func TestNewMemberAgent_DefaultPatternFile(t *testing.T) {
	cfg := MemberAgentConfig{Headless: true}
	agent, err := NewMemberAgent(cfg)
	if err != nil {
		t.Fatalf("NewMemberAgent: %v", err)
	}
	defer agent.Close()
}
