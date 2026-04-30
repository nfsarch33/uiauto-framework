package evolver

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sync"
	"time"

	"github.com/nfsarch33/uiauto-framework/internal/doctor"
)

// TraceSource identifies the origin package of a trace.
type TraceSource string

// Trace source identifiers for cross-package trace collection.
const (
	TraceSourceUIAuto      TraceSource = "uiauto"
	TraceSourceResearch    TraceSource = "research"
	TraceSourceEvolver     TraceSource = "evolver"
	TraceSourceCursor      TraceSource = "cursor"
	TraceSourceClaudeCode  TraceSource = "claude_code"
	TraceSourceSubAgent    TraceSource = "sub_agent"
	TraceSourceDoctor      TraceSource = "doctor"
	TraceSourceWooCommerce TraceSource = "woocommerce"
)

// TraceBridgeConfig configures the trace bridge.
type TraceBridgeConfig struct {
	FlushInterval time.Duration
	AutoFlush     bool
	Logger        *slog.Logger
}

// DefaultTraceBridgeConfig returns production defaults.
func DefaultTraceBridgeConfig() TraceBridgeConfig {
	return TraceBridgeConfig{
		FlushInterval: 30 * time.Second,
		AutoFlush:     true,
		Logger:        slog.Default(),
	}
}

// TraceBridge connects multiple trace sources to a single TraceCollector
// and feeds traces to the SignalMiner for evolution.
type TraceBridge struct {
	collector *TraceCollector
	miner     *SignalMiner
	cfg       TraceBridgeConfig
	mu        sync.Mutex
	counts    map[TraceSource]int
	stopCh    chan struct{}
	running   bool
}

// NewTraceBridge creates a bridge between execution traces and the evolution pipeline.
func NewTraceBridge(collector *TraceCollector, miner *SignalMiner, cfg TraceBridgeConfig) *TraceBridge {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &TraceBridge{
		collector: collector,
		miner:     miner,
		cfg:       cfg,
		counts:    make(map[TraceSource]int),
		stopCh:    make(chan struct{}),
	}
}

// RecordUIAutoTrace records a trace from the uiauto package.
func (b *TraceBridge) RecordUIAutoTrace(taskName string, success bool, latencyMs float64, toolCalls []ToolCall, meta map[string]string) error {
	trace := ExecutionTrace{
		ID:          fmt.Sprintf("uiauto-%d", time.Now().UnixNano()),
		TaskName:    taskName,
		AgentID:     "ui-agent",
		StartTime:   time.Now().Add(-time.Duration(latencyMs) * time.Millisecond),
		EndTime:     time.Now(),
		LatencyMs:   latencyMs,
		Success:     success,
		ToolsCalled: toolCalls,
		ModelTier:   meta["model_tier"],
		Tags:        []string{string(TraceSourceUIAuto)},
		Metadata:    meta,
	}
	return b.record(TraceSourceUIAuto, trace)
}

// RecordResearchTrace records a trace from the research package.
func (b *TraceBridge) RecordResearchTrace(taskName string, success bool, latencyMs float64, errMsg string, meta map[string]string) error {
	trace := ExecutionTrace{
		ID:        fmt.Sprintf("research-%d", time.Now().UnixNano()),
		TaskName:  taskName,
		AgentID:   "research-agent",
		StartTime: time.Now().Add(-time.Duration(latencyMs) * time.Millisecond),
		EndTime:   time.Now(),
		LatencyMs: latencyMs,
		Success:   success,
		ErrorMsg:  errMsg,
		Tags:      []string{string(TraceSourceResearch)},
		Metadata:  meta,
	}
	return b.record(TraceSourceResearch, trace)
}

// RecordDoctorReport converts a unified doctor.Report into execution traces
// and feeds them into the evolution pipeline. Each check becomes a trace,
// allowing the SignalMiner to detect degradation patterns and propose mutations.
func (b *TraceBridge) RecordDoctorReport(report *doctor.Report) error {
	if report == nil {
		return nil
	}
	for _, suite := range report.Suites {
		for _, check := range suite.Checks {
			success := check.Status == doctor.StatusPass
			errMsg := ""
			if !success {
				errMsg = check.Message
			}
			trace := ExecutionTrace{
				ID:        fmt.Sprintf("doctor-%s-%s-%d", suite.Name, check.Name, time.Now().UnixNano()),
				TaskName:  fmt.Sprintf("doctor/%s/%s", suite.Name, check.Name),
				AgentID:   "agent-doctor",
				StartTime: report.Timestamp,
				EndTime:   report.Timestamp.Add(check.Duration),
				LatencyMs: float64(check.Duration.Milliseconds()),
				Success:   success,
				ErrorMsg:  errMsg,
				Tags:      []string{string(TraceSourceDoctor), suite.Name},
				Metadata: map[string]string{
					"suite":    suite.Name,
					"check":    check.Name,
					"status":   check.Status.String(),
					"overall":  report.Overall,
					"platform": report.Platform,
				},
			}
			if err := b.record(TraceSourceDoctor, trace); err != nil {
				return fmt.Errorf("record doctor trace %s/%s: %w", suite.Name, check.Name, err)
			}
		}
	}
	return nil
}

// WCMetricsSnapshot captures key WooCommerce operational metrics for trend analysis.
type WCMetricsSnapshot struct {
	Imports      int     `json:"imports"`
	Orders       int     `json:"orders"`
	Anomalies    int     `json:"anomalies"`
	Descriptions int     `json:"descriptions"`
	Deploys      int     `json:"deploys"`
	Errors       int     `json:"errors"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`

	// Secondary WC stack packages (see internal/*/metrics.go naming: wc_<subsystem>_*_total).
	EventsTracked      int `json:"events_tracked"`
	ReportsGenerated   int `json:"reports_generated"`
	StoresSynced       int `json:"stores_synced"`
	OrdersRouted       int `json:"orders_routed"`
	LeadsScored        int `json:"leads_scored"`
	LeadsQualified     int `json:"leads_qualified"`
	PricesCalculated   int `json:"prices_calculated"`
	DynamicAdjustments int `json:"dynamic_adjustments"`
	ChecksTotal        int `json:"checks_total"`
	AlertsTotal        int `json:"alerts_total"`
	SequencesRun       int `json:"sequences_run"`
	MessagesSent       int `json:"messages_sent"`
	CampaignsRun       int `json:"campaigns_run"`
	InteractionsTotal  int `json:"interactions_total"`
}

// WCMetricsData is an alias for WCMetricsSnapshot (evolver trace + JSON payloads).
type WCMetricsData = WCMetricsSnapshot

// RecordWCMetrics converts a WooCommerce metrics snapshot into a trace
// for the evolution pipeline to detect operational trends.
func (b *TraceBridge) RecordWCMetrics(snap WCMetricsData) error {
	success := snap.Errors == 0
	errMsg := ""
	if !success {
		errMsg = fmt.Sprintf("%d errors detected", snap.Errors)
	}
	trace := ExecutionTrace{
		ID:        fmt.Sprintf("wc-metrics-%d", time.Now().UnixNano()),
		TaskName:  "woocommerce/metrics_snapshot",
		AgentID:   "wc-tools",
		StartTime: time.Now(),
		EndTime:   time.Now(),
		LatencyMs: snap.AvgLatencyMs,
		Success:   success,
		ErrorMsg:  errMsg,
		Tags:      []string{string(TraceSourceWooCommerce), "metrics"},
		Metadata: map[string]string{
			"imports":             fmt.Sprintf("%d", snap.Imports),
			"orders":              fmt.Sprintf("%d", snap.Orders),
			"anomalies":           fmt.Sprintf("%d", snap.Anomalies),
			"descriptions":        fmt.Sprintf("%d", snap.Descriptions),
			"deploys":             fmt.Sprintf("%d", snap.Deploys),
			"errors":              fmt.Sprintf("%d", snap.Errors),
			"events_tracked":      fmt.Sprintf("%d", snap.EventsTracked),
			"reports_generated":   fmt.Sprintf("%d", snap.ReportsGenerated),
			"stores_synced":       fmt.Sprintf("%d", snap.StoresSynced),
			"orders_routed":       fmt.Sprintf("%d", snap.OrdersRouted),
			"leads_scored":        fmt.Sprintf("%d", snap.LeadsScored),
			"leads_qualified":     fmt.Sprintf("%d", snap.LeadsQualified),
			"prices_calculated":   fmt.Sprintf("%d", snap.PricesCalculated),
			"dynamic_adjustments": fmt.Sprintf("%d", snap.DynamicAdjustments),
			"checks_total":        fmt.Sprintf("%d", snap.ChecksTotal),
			"alerts_total":        fmt.Sprintf("%d", snap.AlertsTotal),
			"sequences_run":       fmt.Sprintf("%d", snap.SequencesRun),
			"messages_sent":       fmt.Sprintf("%d", snap.MessagesSent),
			"campaigns_run":       fmt.Sprintf("%d", snap.CampaignsRun),
			"interactions_total":  fmt.Sprintf("%d", snap.InteractionsTotal),
		},
	}
	return b.record(TraceSourceWooCommerce, trace)
}

// Record stores an ExecutionTrace from any source into the collector.
// This is the generic entrypoint for all trace sources (Cursor, Claude Code CLI,
// sub-agents, uiauto, research). The trace.Tags[0] is used as the source label
// for Prometheus metrics when a Metrics instance is attached.
func (b *TraceBridge) Record(source TraceSource, trace ExecutionTrace) error {
	return b.record(source, trace)
}

func (b *TraceBridge) record(source TraceSource, trace ExecutionTrace) error {
	if err := b.collector.Record(trace); err != nil {
		return err
	}
	b.mu.Lock()
	b.counts[source]++
	b.mu.Unlock()
	return nil
}

// MineSignals flushes buffered traces and mines for evolution signals.
func (b *TraceBridge) MineSignals() ([]Signal, error) {
	if err := b.collector.Flush(); err != nil {
		return nil, fmt.Errorf("flush traces: %w", err)
	}

	traces, err := LoadTraces(b.collector.filePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("load traces: %w", err)
	}

	if len(traces) == 0 {
		return nil, nil
	}

	return b.miner.Mine(traces), nil
}

// Start begins background flush goroutine if AutoFlush is enabled.
func (b *TraceBridge) Start(_ context.Context) {
	if !b.cfg.AutoFlush || b.cfg.FlushInterval <= 0 {
		return
	}

	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return
	}
	b.running = true
	b.mu.Unlock()

	go func() {
		ticker := time.NewTicker(b.cfg.FlushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := b.collector.Flush(); err != nil {
					b.cfg.Logger.Warn("trace bridge auto-flush failed", "err", err)
				}
			case <-b.stopCh:
				return
			}
		}
	}()
}

// Stop halts the background flush goroutine and performs a final flush.
func (b *TraceBridge) Stop() error {
	b.mu.Lock()
	if b.running {
		close(b.stopCh)
		b.running = false
	}
	b.mu.Unlock()
	return b.collector.Flush()
}

// Stats returns per-source trace counts.
func (b *TraceBridge) Stats() map[TraceSource]int {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make(map[TraceSource]int, len(b.counts))
	for k, v := range b.counts {
		out[k] = v
	}
	return out
}
