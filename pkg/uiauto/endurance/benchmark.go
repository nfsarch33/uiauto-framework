package endurance

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"time"
)

// BenchmarkFunc is a benchmarkable function returning latency.
type BenchmarkFunc func(ctx context.Context) (time.Duration, error)

// BenchmarkConfig controls benchmark parameters.
type BenchmarkConfig struct {
	Iterations int
	WarmUp     int
}

// DefaultBenchmarkConfig returns standard benchmark parameters.
func DefaultBenchmarkConfig() BenchmarkConfig {
	return BenchmarkConfig{Iterations: 100, WarmUp: 5}
}

// BenchmarkResult captures performance statistics.
type BenchmarkResult struct {
	Name       string        `json:"name"`
	Iterations int           `json:"iterations"`
	P50        time.Duration `json:"p50"`
	P95        time.Duration `json:"p95"`
	P99        time.Duration `json:"p99"`
	Min        time.Duration `json:"min"`
	Max        time.Duration `json:"max"`
	Avg        time.Duration `json:"avg"`
	MemAllocs  uint64        `json:"mem_allocs"`
	MemBytes   uint64        `json:"mem_bytes"`
	Errors     int           `json:"errors"`
}

// Summary returns a human-readable benchmark summary.
func (r BenchmarkResult) Summary() string {
	return fmt.Sprintf("%s: %d iters | p50=%s p95=%s p99=%s | min=%s max=%s avg=%s | mem=%dKB/%dallocs | errors=%d",
		r.Name, r.Iterations,
		r.P50.Round(time.Microsecond), r.P95.Round(time.Microsecond), r.P99.Round(time.Microsecond),
		r.Min.Round(time.Microsecond), r.Max.Round(time.Microsecond), r.Avg.Round(time.Microsecond),
		r.MemBytes/1024, r.MemAllocs, r.Errors)
}

// RunBenchmark executes a benchmark suite for a named operation.
func RunBenchmark(ctx context.Context, name string, fn BenchmarkFunc, cfg BenchmarkConfig) BenchmarkResult {
	// Warm-up phase
	for i := 0; i < cfg.WarmUp; i++ {
		fn(ctx)
	}

	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	latencies := make([]time.Duration, 0, cfg.Iterations)
	errors := 0

	for i := 0; i < cfg.Iterations; i++ {
		select {
		case <-ctx.Done():
			break
		default:
		}

		dur, err := fn(ctx)
		if err != nil {
			errors++
			continue
		}
		latencies = append(latencies, dur)
	}

	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	if len(latencies) == 0 {
		return BenchmarkResult{Name: name, Iterations: cfg.Iterations, Errors: errors}
	}

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	var total time.Duration
	for _, l := range latencies {
		total += l
	}

	return BenchmarkResult{
		Name:       name,
		Iterations: len(latencies),
		P50:        percentile(latencies, 0.50),
		P95:        percentile(latencies, 0.95),
		P99:        percentile(latencies, 0.99),
		Min:        latencies[0],
		Max:        latencies[len(latencies)-1],
		Avg:        total / time.Duration(len(latencies)),
		MemAllocs:  memAfter.Mallocs - memBefore.Mallocs,
		MemBytes:   memAfter.TotalAlloc - memBefore.TotalAlloc,
		Errors:     errors,
	}
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}
