package evolver

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds Prometheus metrics for the evolution subsystem.
type Metrics struct {
	MutationsTotal    *prometheus.CounterVec
	GenesAppliedTotal prometheus.Counter
	CapsulesCreated   prometheus.Counter
	SignalSeverity    *prometheus.HistogramVec
	EngineRunsTotal   prometheus.Counter
	EngineRunDuration prometheus.Histogram

	SignalsDetected *prometheus.CounterVec
	PromotionsTotal *prometheus.CounterVec
	TracesRecorded  *prometheus.CounterVec
}

// NewMetrics registers and returns Prometheus metrics for the evolution subsystem.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		MutationsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "evolver",
			Name:      "mutations_total",
			Help:      "Total mutations by status.",
		}, []string{"status"}),
		GenesAppliedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "evolver",
			Name:      "genes_applied_total",
			Help:      "Total genes applied successfully.",
		}),
		CapsulesCreated: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "evolver",
			Name:      "capsules_created_total",
			Help:      "Total capsules created.",
		}),
		SignalSeverity: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "evolver",
			Name:      "signal_severity",
			Help:      "Distribution of signal severities (info=0, warning=1, critical=2).",
			Buckets:   []float64{0, 1, 2},
		}, []string{"type"}),
		EngineRunsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "evolver",
			Name:      "engine_runs_total",
			Help:      "Total evolution engine runs.",
		}),
		EngineRunDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "evolver",
			Name:      "engine_run_duration_seconds",
			Help:      "Duration of evolution engine runs.",
			Buckets:   prometheus.DefBuckets,
		}),
	}

	m.SignalsDetected = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "evolver",
		Name:      "signals_detected_total",
		Help:      "Total signals detected by type.",
	}, []string{"type"})
	m.PromotionsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "evolver",
		Name:      "promotions_total",
		Help:      "Total promotion decisions by outcome.",
	}, []string{"outcome"})
	m.TracesRecorded = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "evolver",
		Name:      "traces_recorded_total",
		Help:      "Total traces recorded by source.",
	}, []string{"source"})

	reg.MustRegister(
		m.MutationsTotal,
		m.GenesAppliedTotal,
		m.CapsulesCreated,
		m.SignalSeverity,
		m.EngineRunsTotal,
		m.EngineRunDuration,
		m.SignalsDetected,
		m.PromotionsTotal,
		m.TracesRecorded,
	)

	for _, s := range []string{"pending", "approved", "applied", "rejected", "rolled_back"} {
		m.MutationsTotal.WithLabelValues(s)
	}
	for _, s := range []string{
		string(SignalRepeatedFailure), string(SignalHighLatency),
		string(SignalCostSpike), string(SignalLowSuccessRate),
	} {
		m.SignalSeverity.WithLabelValues(s)
		m.SignalsDetected.WithLabelValues(s)
	}

	for _, o := range []string{"approved", "rejected", "auto"} {
		m.PromotionsTotal.WithLabelValues(o)
	}
	for _, src := range []string{
		string(TraceSourceUIAuto), string(TraceSourceResearch), string(TraceSourceEvolver),
		string(TraceSourceCursor), string(TraceSourceClaudeCode), string(TraceSourceSubAgent),
	} {
		m.TracesRecorded.WithLabelValues(src)
	}

	return m
}

// RecordSignals records signal severity observations and detection counts.
func (m *Metrics) RecordSignals(signals []Signal) {
	for _, s := range signals {
		val := severityToFloat(s.Severity)
		m.SignalSeverity.WithLabelValues(string(s.Type)).Observe(val)
		m.SignalsDetected.WithLabelValues(string(s.Type)).Inc()
	}
}

// RecordMutations records mutation status counts.
func (m *Metrics) RecordMutations(mutations []Mutation) {
	for _, mut := range mutations {
		m.MutationsTotal.WithLabelValues(string(mut.Status)).Inc()
	}
}

// RecordPromotion records a promotion outcome ("approved", "rejected", "auto").
func (m *Metrics) RecordPromotion(outcome string) {
	m.PromotionsTotal.WithLabelValues(outcome).Inc()
}

// RecordTrace records a trace from a given source ("uiauto", "research", etc.).
func (m *Metrics) RecordTrace(source string) {
	m.TracesRecorded.WithLabelValues(source).Inc()
}

func severityToFloat(s SignalSeverity) float64 {
	switch s {
	case SeverityInfo:
		return 0
	case SeverityWarning:
		return 1
	case SeverityCritical:
		return 2
	default:
		return 0
	}
}
