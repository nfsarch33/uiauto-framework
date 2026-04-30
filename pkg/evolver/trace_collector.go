package evolver

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// ExecutionTrace captures a single agent task execution with all relevant
// context for post-hoc signal mining.
type ExecutionTrace struct {
	ID           string            `json:"id"`
	TaskName     string            `json:"task_name"`
	AgentID      string            `json:"agent_id"`
	StartTime    time.Time         `json:"start_time"`
	EndTime      time.Time         `json:"end_time"`
	LatencyMs    float64           `json:"latency_ms"`
	Success      bool              `json:"success"`
	ErrorMsg     string            `json:"error_msg,omitempty"`
	ToolsCalled  []ToolCall        `json:"tools_called,omitempty"`
	PromptTokens int               `json:"prompt_tokens"`
	CompTokens   int               `json:"completion_tokens"`
	CostUSD      float64           `json:"cost_usd"`
	ModelTier    string            `json:"model_tier,omitempty"`
	Tags         []string          `json:"tags,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// ToolCall records a single tool invocation within a trace.
type ToolCall struct {
	Name      string  `json:"name"`
	LatencyMs float64 `json:"latency_ms"`
	Success   bool    `json:"success"`
	ErrorMsg  string  `json:"error_msg,omitempty"`
}

// TraceCollector accumulates execution traces and can flush them to disk
// as JSONL for signal mining.
type TraceCollector struct {
	mu       sync.Mutex
	traces   []ExecutionTrace
	maxBuf   int
	filePath string
}

// NewTraceCollector creates a collector that buffers up to maxBuf traces
// before requiring a flush. filePath is the JSONL file for persistence.
func NewTraceCollector(filePath string, maxBuf int) *TraceCollector {
	if maxBuf <= 0 {
		maxBuf = 1000
	}
	return &TraceCollector{
		filePath: filePath,
		maxBuf:   maxBuf,
	}
}

// Record adds a trace to the in-memory buffer.
func (tc *TraceCollector) Record(trace ExecutionTrace) error {
	if trace.ID == "" {
		return fmt.Errorf("trace: id is required")
	}
	tc.mu.Lock()
	defer tc.mu.Unlock()

	if trace.LatencyMs == 0 && !trace.EndTime.IsZero() && !trace.StartTime.IsZero() {
		trace.LatencyMs = float64(trace.EndTime.Sub(trace.StartTime).Milliseconds())
	}

	tc.traces = append(tc.traces, trace)
	return nil
}

// Len returns the number of buffered traces.
func (tc *TraceCollector) Len() int {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return len(tc.traces)
}

// Flush writes all buffered traces to the JSONL file and clears the buffer.
func (tc *TraceCollector) Flush() error {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	if len(tc.traces) == 0 {
		return nil
	}

	f, err := os.OpenFile(tc.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open trace file: %w", err)
	}
	defer func() { _ = f.Close() }()

	enc := json.NewEncoder(f)
	for _, t := range tc.traces {
		if err := enc.Encode(t); err != nil {
			return fmt.Errorf("encode trace %s: %w", t.ID, err)
		}
	}

	tc.traces = tc.traces[:0]
	return nil
}

// Snapshot returns a copy of the current buffered traces without flushing.
func (tc *TraceCollector) Snapshot() []ExecutionTrace {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	out := make([]ExecutionTrace, len(tc.traces))
	copy(out, tc.traces)
	return out
}

// LoadTraces reads a JSONL trace file and returns all valid ExecutionTrace
// entries. Lines that do not decode (e.g. event metadata) are silently skipped
// so mixed-format trace files remain usable.
func LoadTraces(filePath string) ([]ExecutionTrace, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read traces: %w", err)
	}

	var traces []ExecutionTrace
	for _, line := range splitJSONL(data) {
		if len(line) == 0 {
			continue
		}
		var t ExecutionTrace
		if err := json.Unmarshal(line, &t); err != nil {
			continue // skip non-ExecutionTrace lines
		}
		if t.ID == "" {
			continue // not a valid trace (missing required field)
		}
		traces = append(traces, t)
	}
	return traces, nil
}

func splitJSONL(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i := range data {
		if data[i] == '\n' {
			line := data[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}
