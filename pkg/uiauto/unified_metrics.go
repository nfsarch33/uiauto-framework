package uiauto

import (
	"context"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// UnifiedMetricsRegistry provides a single point of registration for all
// uiauto Prometheus metrics: application metrics, runtime metrics, and
// the operations counter used across all packages.
type UnifiedMetricsRegistry struct {
	AppMetrics     *Metrics
	RuntimeMetrics *RuntimeMetrics
	Operations     *prometheus.CounterVec
	Latency        *prometheus.HistogramVec
	Registry       *prometheus.Registry
}

// NewUnifiedMetricsRegistry creates and registers all metrics in a single
// Prometheus registry. This is the canonical entrypoint for production use.
func NewUnifiedMetricsRegistry() *UnifiedMetricsRegistry {
	reg := prometheus.NewRegistry()

	ops := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "uiauto",
		Name:      "operations_total",
		Help:      "Universal operation counter across all packages.",
	}, []string{"package", "operation", "status"})

	latency := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "uiauto",
		Name:      "operation_duration_seconds",
		Help:      "Duration of operations by package.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"package", "operation"})

	reg.MustRegister(ops, latency)

	return &UnifiedMetricsRegistry{
		AppMetrics:     NewMetrics(reg),
		RuntimeMetrics: NewRuntimeMetrics(reg),
		Operations:     ops,
		Latency:        latency,
		Registry:       reg,
	}
}

// RecordOp is a helper to record a single operation outcome.
func (u *UnifiedMetricsRegistry) RecordOp(pkg, operation string, err error, dur time.Duration) {
	status := "success"
	if err != nil {
		status = "error"
	}
	u.Operations.WithLabelValues(pkg, operation, status).Inc()
	u.Latency.WithLabelValues(pkg, operation).Observe(dur.Seconds())
}

// SlogMetricsHandler wraps a slog.Handler to also emit Prometheus metrics
// for log levels (error, warn, info counts).
type SlogMetricsHandler struct {
	inner    slog.Handler
	logTotal *prometheus.CounterVec
}

// NewSlogMetricsHandler creates a handler that counts log messages by level.
func NewSlogMetricsHandler(inner slog.Handler, reg prometheus.Registerer) *SlogMetricsHandler {
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "uiauto",
		Subsystem: "log",
		Name:      "messages_total",
		Help:      "Total log messages by level.",
	}, []string{"level"})
	reg.MustRegister(counter)

	for _, lvl := range []string{"DEBUG", "INFO", "WARN", "ERROR"} {
		counter.WithLabelValues(lvl)
	}

	return &SlogMetricsHandler{inner: inner, logTotal: counter}
}

// Enabled delegates to the inner handler.
func (h *SlogMetricsHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle counts the log message and delegates to the inner handler.
func (h *SlogMetricsHandler) Handle(ctx context.Context, record slog.Record) error {
	h.logTotal.WithLabelValues(record.Level.String()).Inc()
	return h.inner.Handle(ctx, record)
}

// WithAttrs delegates to the inner handler.
func (h *SlogMetricsHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &SlogMetricsHandler{inner: h.inner.WithAttrs(attrs), logTotal: h.logTotal}
}

// WithGroup delegates to the inner handler.
func (h *SlogMetricsHandler) WithGroup(name string) slog.Handler {
	return &SlogMetricsHandler{inner: h.inner.WithGroup(name), logTotal: h.logTotal}
}
