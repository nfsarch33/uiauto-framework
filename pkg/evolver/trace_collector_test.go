package evolver

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTraceCollector_RecordAndFlush(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "traces.jsonl")

	tc := NewTraceCollector(path, 100)

	trace := ExecutionTrace{
		ID:        "t-001",
		TaskName:  "scrape-login",
		AgentID:   "agent-1",
		StartTime: time.Now().UTC(),
		EndTime:   time.Now().UTC().Add(500 * time.Millisecond),
		Success:   true,
		CostUSD:   0.002,
	}

	if err := tc.Record(trace); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if tc.Len() != 1 {
		t.Fatalf("expected 1 trace, got %d", tc.Len())
	}

	if err := tc.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if tc.Len() != 0 {
		t.Fatalf("expected 0 traces after flush, got %d", tc.Len())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty trace file")
	}
}

func TestTraceCollector_RecordEmptyID(t *testing.T) {
	tc := NewTraceCollector("/dev/null", 100)
	err := tc.Record(ExecutionTrace{TaskName: "test"})
	if err == nil {
		t.Fatal("expected error for empty ID")
	}
}

func TestTraceCollector_FlushEmpty(t *testing.T) {
	tc := NewTraceCollector("/dev/null", 100)
	if err := tc.Flush(); err != nil {
		t.Fatalf("Flush empty: %v", err)
	}
}

func TestTraceCollector_Snapshot(t *testing.T) {
	tc := NewTraceCollector("/dev/null", 100)
	_ = tc.Record(ExecutionTrace{ID: "t1", TaskName: "a"})
	_ = tc.Record(ExecutionTrace{ID: "t2", TaskName: "b"})

	snap := tc.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 in snapshot, got %d", len(snap))
	}
	if tc.Len() != 2 {
		t.Fatal("snapshot should not drain the buffer")
	}
}

func TestTraceCollector_LatencyAutoCalc(t *testing.T) {
	tc := NewTraceCollector("/dev/null", 100)
	start := time.Now().UTC()
	end := start.Add(250 * time.Millisecond)

	_ = tc.Record(ExecutionTrace{
		ID:        "t-auto",
		TaskName:  "test",
		StartTime: start,
		EndTime:   end,
	})

	snap := tc.Snapshot()
	if snap[0].LatencyMs != 250 {
		t.Errorf("expected auto-calculated latency 250ms, got %.0f", snap[0].LatencyMs)
	}
}

func TestLoadTraces_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "traces.jsonl")

	tc := NewTraceCollector(path, 100)
	_ = tc.Record(ExecutionTrace{ID: "t1", TaskName: "a", Success: true, CostUSD: 0.01})
	_ = tc.Record(ExecutionTrace{ID: "t2", TaskName: "b", Success: false, ErrorMsg: "fail"})
	_ = tc.Flush()

	loaded, err := LoadTraces(path)
	if err != nil {
		t.Fatalf("LoadTraces: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 loaded traces, got %d", len(loaded))
	}
	if loaded[0].ID != "t1" {
		t.Errorf("first trace ID mismatch")
	}
	if loaded[1].ErrorMsg != "fail" {
		t.Errorf("second trace ErrorMsg mismatch")
	}
}
