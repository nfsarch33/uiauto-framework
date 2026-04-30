package uiauto

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"runtime"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// RuntimeMetrics exports Go runtime stats as Prometheus gauges.
type RuntimeMetrics struct {
	HeapAlloc    prometheus.Gauge
	HeapSys      prometheus.Gauge
	Goroutines   prometheus.Gauge
	GCPauseNs    prometheus.Gauge
	GCCycles     prometheus.Counter
	NumGC        prometheus.Gauge
	StackInUse   prometheus.Gauge
	HeapObjects  prometheus.Gauge
	HeapReleased prometheus.Gauge
}

// NewRuntimeMetrics registers runtime gauges with the provided registerer.
func NewRuntimeMetrics(reg prometheus.Registerer) *RuntimeMetrics {
	m := &RuntimeMetrics{
		HeapAlloc: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "uiauto",
			Subsystem: "runtime",
			Name:      "heap_alloc_bytes",
			Help:      "Current heap allocation in bytes.",
		}),
		HeapSys: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "uiauto",
			Subsystem: "runtime",
			Name:      "heap_sys_bytes",
			Help:      "Heap memory obtained from OS.",
		}),
		Goroutines: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "uiauto",
			Subsystem: "runtime",
			Name:      "goroutines_active",
			Help:      "Number of active goroutines.",
		}),
		GCPauseNs: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "uiauto",
			Subsystem: "runtime",
			Name:      "gc_pause_ns",
			Help:      "Last GC pause duration in nanoseconds.",
		}),
		GCCycles: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "uiauto",
			Subsystem: "runtime",
			Name:      "gc_cycles_total",
			Help:      "Total GC cycles completed.",
		}),
		NumGC: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "uiauto",
			Subsystem: "runtime",
			Name:      "gc_completed_total",
			Help:      "Total GC cycles (gauge view for snapshots).",
		}),
		StackInUse: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "uiauto",
			Subsystem: "runtime",
			Name:      "stack_inuse_bytes",
			Help:      "Stack memory in use.",
		}),
		HeapObjects: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "uiauto",
			Subsystem: "runtime",
			Name:      "heap_objects",
			Help:      "Number of allocated heap objects.",
		}),
		HeapReleased: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "uiauto",
			Subsystem: "runtime",
			Name:      "heap_released_bytes",
			Help:      "Heap memory released to OS.",
		}),
	}

	reg.MustRegister(
		m.HeapAlloc, m.HeapSys, m.Goroutines,
		m.GCPauseNs, m.GCCycles, m.NumGC,
		m.StackInUse, m.HeapObjects, m.HeapReleased,
	)
	return m
}

// Update reads current runtime.MemStats and goroutine count into Prometheus gauges.
func (m *RuntimeMetrics) Update() {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	m.HeapAlloc.Set(float64(ms.HeapAlloc))
	m.HeapSys.Set(float64(ms.HeapSys))
	m.Goroutines.Set(float64(runtime.NumGoroutine()))
	if ms.NumGC > 0 {
		m.GCPauseNs.Set(float64(ms.PauseNs[(ms.NumGC+255)%256]))
	}
	m.NumGC.Set(float64(ms.NumGC))
	m.StackInUse.Set(float64(ms.StackInuse))
	m.HeapObjects.Set(float64(ms.HeapObjects))
	m.HeapReleased.Set(float64(ms.HeapReleased))
}

// DebugServerConfig configures the pprof/metrics debug HTTP server.
type DebugServerConfig struct {
	Addr     string // defaults to ":6060"
	Registry *prometheus.Registry
	Logger   *slog.Logger
}

// StartDebugServer launches an HTTP server exposing pprof endpoints and
// Prometheus metrics. It blocks until ctx is cancelled.
func StartDebugServer(ctx context.Context, cfg DebugServerConfig) error {
	if cfg.Addr == "" {
		cfg.Addr = ":6060"
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	if cfg.Registry != nil {
		mux.Handle("/metrics", promhttp.HandlerFor(cfg.Registry, promhttp.HandlerOpts{}))
	} else {
		mux.Handle("/metrics", promhttp.Handler())
	}

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	srv := &http.Server{
		Addr:         cfg.Addr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	cfg.Logger.Info("debug server starting", "addr", cfg.Addr)

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("debug server failed: %w", err)
	}
	return nil
}
