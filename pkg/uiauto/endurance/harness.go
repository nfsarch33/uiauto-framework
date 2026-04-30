package endurance

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/signal"
)

// CycleFunc is called once per endurance cycle. Return error on failure.
type CycleFunc func(ctx context.Context, cycle int) error

// Config controls the endurance run parameters.
type Config struct {
	Duration       time.Duration // total run time (0 = cycle-count only)
	MaxCycles      int           // 0 = unlimited (bounded by Duration)
	CycleInterval  time.Duration // pause between cycles
	ReportInterval time.Duration // how often to emit progress signals
}

// DefaultConfig returns a config suitable for a 1-hour dry run.
func DefaultConfig() Config {
	return Config{
		Duration:       1 * time.Hour,
		MaxCycles:      0,
		CycleInterval:  5 * time.Second,
		ReportInterval: 1 * time.Minute,
	}
}

// Result captures the full endurance run outcome.
type Result struct {
	TotalCycles   int           `json:"total_cycles"`
	Passed        int           `json:"passed"`
	Failed        int           `json:"failed"`
	TotalDuration time.Duration `json:"total_duration"`
	AvgCycleTime  time.Duration `json:"avg_cycle_time"`
	MaxCycleTime  time.Duration `json:"max_cycle_time"`
	MinCycleTime  time.Duration `json:"min_cycle_time"`
	Errors        []CycleError  `json:"errors,omitempty"`
}

// CycleError captures a specific cycle failure.
type CycleError struct {
	Cycle int    `json:"cycle"`
	Error string `json:"error"`
}

// Harness drives continuous mutation+heal+report cycles.
type Harness struct {
	config  Config
	cycleFn CycleFunc
	emitter *signal.Emitter
	logger  *slog.Logger
	mu      sync.Mutex
	result  Result
}

// NewHarness creates an endurance test harness.
func NewHarness(fn CycleFunc, cfg Config, emitter *signal.Emitter) *Harness {
	return &Harness{
		config:  cfg,
		cycleFn: fn,
		emitter: emitter,
		logger:  slog.Default(),
	}
}

// Run executes the endurance harness until duration expires or max cycles reached.
func (h *Harness) Run(ctx context.Context) Result {
	start := time.Now()
	var runCtx context.Context
	var cancel context.CancelFunc

	if h.config.Duration > 0 {
		runCtx, cancel = context.WithTimeout(ctx, h.config.Duration)
	} else {
		runCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	lastReport := start
	var minCycle, maxCycle time.Duration
	var totalCycleTime time.Duration

	for cycle := 0; ; cycle++ {
		if h.config.MaxCycles > 0 && cycle >= h.config.MaxCycles {
			break
		}

		select {
		case <-runCtx.Done():
			goto done
		default:
		}

		cycleStart := time.Now()
		err := h.cycleFn(runCtx, cycle)
		cycleDur := time.Since(cycleStart)

		h.mu.Lock()
		h.result.TotalCycles++
		totalCycleTime += cycleDur

		if cycleDur > maxCycle || cycle == 0 {
			maxCycle = cycleDur
		}
		if cycleDur < minCycle || cycle == 0 {
			minCycle = cycleDur
		}

		if err != nil {
			h.result.Failed++
			h.result.Errors = append(h.result.Errors, CycleError{Cycle: cycle, Error: err.Error()})
		} else {
			h.result.Passed++
		}
		h.mu.Unlock()

		// Periodic progress reporting
		if h.emitter != nil && time.Since(lastReport) >= h.config.ReportInterval {
			h.emitProgress(cycle, time.Since(start))
			lastReport = time.Now()
		}

		if h.config.CycleInterval > 0 {
			select {
			case <-runCtx.Done():
				goto done
			case <-time.After(h.config.CycleInterval):
			}
		}
	}

done:
	h.mu.Lock()
	h.result.TotalDuration = time.Since(start)
	h.result.MaxCycleTime = maxCycle
	h.result.MinCycleTime = minCycle
	if h.result.TotalCycles > 0 {
		h.result.AvgCycleTime = totalCycleTime / time.Duration(h.result.TotalCycles)
	}
	result := h.result
	h.mu.Unlock()

	// Final signal
	if h.emitter != nil {
		signal.EmitTestResult(h.emitter, signal.TestEvent{
			Suite:    "endurance",
			Passed:   result.Passed,
			Failed:   result.Failed,
			Duration: result.TotalDuration,
		})
	}

	return result
}

func (h *Harness) emitProgress(cycle int, elapsed time.Duration) {
	h.mu.Lock()
	passed, failed := h.result.Passed, h.result.Failed
	h.mu.Unlock()

	h.emitter.Emit(signal.Signal{
		Severity: signal.SeverityInfo,
		Category: signal.CategoryEndurance,
		Title:    fmt.Sprintf("Endurance progress: cycle %d, %d passed, %d failed (%s elapsed)", cycle, passed, failed, elapsed.Round(time.Second)),
		Source:   "endurance-harness",
	})
}

// Summary returns a human-readable result summary.
func (r Result) Summary() string {
	return fmt.Sprintf("Endurance: %d cycles (%d pass, %d fail) in %s | avg=%s min=%s max=%s",
		r.TotalCycles, r.Passed, r.Failed, r.TotalDuration.Round(time.Millisecond),
		r.AvgCycleTime.Round(time.Microsecond),
		r.MinCycleTime.Round(time.Microsecond),
		r.MaxCycleTime.Round(time.Microsecond))
}
