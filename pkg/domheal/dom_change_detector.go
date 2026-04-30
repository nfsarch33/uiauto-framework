package domheal

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// ChangeEvent records a detected DOM change with metadata.
type ChangeEvent struct {
	PageID     string    `json:"page_id"`
	DetectedAt time.Time `json:"detected_at"`
	OldHash    string    `json:"old_hash"`
	NewHash    string    `json:"new_hash"`
	Similarity float64   `json:"similarity"`
	Severity   string    `json:"severity"` // "minor", "moderate", "major"
	AutoRepair bool      `json:"auto_repair_triggered"`
}

// DOMChangeDetectorMetrics provides observability for change detection.
type DOMChangeDetectorMetrics struct {
	ChangesDetected  *prometheus.CounterVec
	RepairsTriggered prometheus.Counter
	CheckDuration    *prometheus.HistogramVec
}

// NewDOMChangeDetectorMetrics registers metrics with the given registerer.
func NewDOMChangeDetectorMetrics(reg prometheus.Registerer) *DOMChangeDetectorMetrics {
	m := &DOMChangeDetectorMetrics{
		ChangesDetected: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "domheal",
			Subsystem: "change_detector",
			Name:      "changes_total",
			Help:      "DOM changes detected by severity.",
		}, []string{"severity"}),
		RepairsTriggered: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "domheal",
			Subsystem: "change_detector",
			Name:      "repairs_triggered_total",
			Help:      "Auto-repairs triggered by change detection.",
		}),
		CheckDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "domheal",
			Subsystem: "change_detector",
			Name:      "check_duration_seconds",
			Help:      "Duration of DOM change detection checks.",
			Buckets:   []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5},
		}, []string{"page_id"}),
	}
	reg.MustRegister(m.ChangesDetected, m.RepairsTriggered, m.CheckDuration)
	return m
}

// RepairCallback is invoked when a DOM change requires selector repair.
type RepairCallback func(pageID string, event ChangeEvent) error

// DOMChangeDetector composes DriftDetector, FingerprintMatcher, and StructuralMatcher
// to provide multi-layer DOM change detection with automatic repair triggering.
type DOMChangeDetector struct {
	drift       *DriftDetector
	fingerprint *FingerprintMatcher
	structural  *StructuralMatcher
	breaker     *CircuitBreaker

	onRepair RepairCallback
	metrics  *DOMChangeDetectorMetrics
	logger   *slog.Logger

	mu       sync.RWMutex
	events   []ChangeEvent
	stateDir string
}

// DOMChangeDetectorConfig configures the composite change detector.
type DOMChangeDetectorConfig struct {
	StateDir             string
	FingerprintThreshold float64
	StructuralThreshold  float64
	BreakerFailures      int
	BreakerCooldownSec   int
	Logger               *slog.Logger
}

// NewDOMChangeDetector creates a composite detector that uses hash, fingerprint,
// and structural comparison to detect DOM changes at multiple granularity levels.
func NewDOMChangeDetector(cfg DOMChangeDetectorConfig) *DOMChangeDetector {
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if cfg.FingerprintThreshold <= 0 {
		cfg.FingerprintThreshold = 0.75
	}
	if cfg.StructuralThreshold <= 0 {
		cfg.StructuralThreshold = 0.70
	}
	if cfg.BreakerFailures <= 0 {
		cfg.BreakerFailures = 5
	}
	if cfg.BreakerCooldownSec <= 0 {
		cfg.BreakerCooldownSec = 60
	}

	return &DOMChangeDetector{
		drift:       NewDriftDetector(cfg.StateDir),
		fingerprint: NewFingerprintMatcher(cfg.FingerprintThreshold, cfg.Logger),
		structural:  NewStructuralMatcher(cfg.StructuralThreshold, cfg.Logger),
		breaker:     NewCircuitBreaker(cfg.BreakerFailures, cfg.BreakerCooldownSec),
		logger:      cfg.Logger,
		stateDir:    cfg.StateDir,
	}
}

// WithMetrics attaches Prometheus metrics.
func (d *DOMChangeDetector) WithMetrics(m *DOMChangeDetectorMetrics) {
	d.metrics = m
}

// OnRepair sets the callback invoked when auto-repair is needed.
func (d *DOMChangeDetector) OnRepair(cb RepairCallback) {
	d.onRepair = cb
}

// Check performs a multi-layer comparison of the current HTML against stored baselines.
// Returns a ChangeEvent if drift is detected; nil if the page is stable.
func (d *DOMChangeDetector) Check(pageID, html string) *ChangeEvent {
	start := time.Now()

	if !d.breaker.Allow() {
		d.logger.Debug("circuit breaker open, skipping check", "page_id", pageID)
		return nil
	}

	// Layer 1: Content hash (cheapest check)
	hashDrifted, hashErr := d.drift.CheckAndUpdate(pageID, html)
	if hashErr != nil {
		d.logger.Warn("drift detector error", "error", hashErr)
		d.breaker.RecordFailure()
		return nil
	}

	// Always update fingerprint and structural baselines so they have data
	// for comparison on the next call.
	fpResult := d.fingerprint.CheckAndUpdate(pageID, html)
	structResult := d.structural.CheckAndUpdate(pageID, html)

	if !hashDrifted {
		d.breaker.RecordSuccess()
		d.recordCheckDuration(pageID, time.Since(start))
		return nil
	}

	// If this is the first observation for fingerprint/structural, treat as
	// baseline establishment, not a change event.
	if fpResult.IsNew || structResult.IsNew {
		d.breaker.RecordSuccess()
		d.recordCheckDuration(pageID, time.Since(start))
		return nil
	}

	severity := classifySeverity(fpResult.Similarity, structResult.Similarity)

	event := ChangeEvent{
		PageID:     pageID,
		DetectedAt: time.Now(),
		Similarity: fpResult.Similarity,
		Severity:   severity,
	}

	if severity == "minor" && fpResult.Similarity > 0.9 {
		d.breaker.RecordSuccess()
		d.recordCheckDuration(pageID, time.Since(start))
		return nil
	}

	d.mu.Lock()
	d.events = append(d.events, event)
	d.mu.Unlock()

	d.logger.Warn("DOM change detected",
		"page_id", pageID,
		"severity", severity,
		"fingerprint_sim", fpResult.Similarity,
		"structural_sim", structResult.Similarity,
	)

	d.recordChange(severity)

	if severity != "minor" && d.onRepair != nil {
		event.AutoRepair = true
		d.recordRepairTriggered()
		if err := d.onRepair(pageID, event); err != nil {
			d.logger.Error("auto-repair callback failed", "error", err)
			d.breaker.RecordFailure()
		} else {
			d.breaker.RecordSuccess()
		}
	} else {
		d.breaker.RecordSuccess()
	}

	d.recordCheckDuration(pageID, time.Since(start))
	return &event
}

// Events returns all recorded change events.
func (d *DOMChangeDetector) Events() []ChangeEvent {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]ChangeEvent, len(d.events))
	copy(out, d.events)
	return out
}

// Save persists all detector state to disk.
func (d *DOMChangeDetector) Save() error {
	if d.stateDir == "" {
		return nil
	}
	if err := os.MkdirAll(d.stateDir, 0755); err != nil {
		return err
	}
	if err := d.drift.Save(); err != nil {
		return err
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	eventsPath := filepath.Join(d.stateDir, "change_events.json")
	data, err := json.MarshalIndent(d.events, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(eventsPath, data, 0644)
}

// Load restores detector state from disk.
func (d *DOMChangeDetector) Load() error {
	if d.stateDir == "" {
		return nil
	}
	if err := d.drift.Load(); err != nil && !os.IsNotExist(err) {
		return err
	}
	eventsPath := filepath.Join(d.stateDir, "change_events.json")
	data, err := os.ReadFile(eventsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return json.Unmarshal(data, &d.events)
}

// Stats returns diagnostic counts for monitoring.
func (d *DOMChangeDetector) Stats() DOMChangeDetectorStats {
	d.mu.RLock()
	defer d.mu.RUnlock()
	stats := DOMChangeDetectorStats{
		TotalEvents:       len(d.events),
		HashesTracked:     d.drift.HashCount(),
		FingerprintsKnown: d.fingerprint.KnownCount(),
		BreakerState:      d.breaker.State(),
	}
	for _, e := range d.events {
		switch e.Severity {
		case "minor":
			stats.Minor++
		case "moderate":
			stats.Moderate++
		case "major":
			stats.Major++
		}
		if e.AutoRepair {
			stats.RepairsTriggered++
		}
	}
	return stats
}

// DOMChangeDetectorStats holds aggregate statistics.
type DOMChangeDetectorStats struct {
	TotalEvents       int    `json:"total_events"`
	Minor             int    `json:"minor"`
	Moderate          int    `json:"moderate"`
	Major             int    `json:"major"`
	RepairsTriggered  int    `json:"repairs_triggered"`
	HashesTracked     int    `json:"hashes_tracked"`
	FingerprintsKnown int    `json:"fingerprints_known"`
	BreakerState      string `json:"breaker_state"`
}

func classifySeverity(fpSim, structSim float64) string {
	avgSim := (fpSim + structSim) / 2.0
	switch {
	case avgSim >= 0.90:
		return "minor"
	case avgSim >= 0.70:
		return "moderate"
	default:
		return "major"
	}
}

func (d *DOMChangeDetector) recordChange(severity string) {
	if d.metrics == nil {
		return
	}
	d.metrics.ChangesDetected.With(prometheus.Labels{"severity": severity}).Inc()
}

func (d *DOMChangeDetector) recordRepairTriggered() {
	if d.metrics == nil {
		return
	}
	d.metrics.RepairsTriggered.Inc()
}

func (d *DOMChangeDetector) recordCheckDuration(pageID string, dur time.Duration) {
	if d.metrics == nil {
		return
	}
	d.metrics.CheckDuration.With(prometheus.Labels{"page_id": pageID}).Observe(dur.Seconds())
}
