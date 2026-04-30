package uiauto

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds all Prometheus metrics for the uiauto subsystem.
// All metric names are prefixed with "uiauto_" to avoid collisions
// with other packages.
type Metrics struct {
	// Executor metrics
	ExecutorActionsTotal  *prometheus.CounterVec
	ExecutorCacheTotal    *prometheus.CounterVec
	ExecutorStructMatches prometheus.Counter
	ExecutorLatency       prometheus.Histogram

	// Router metrics
	RouterAttemptsTotal  *prometheus.CounterVec
	RouterSuccessesTotal *prometheus.CounterVec
	RouterPromotions     prometheus.Counter
	RouterDemotions      prometheus.Counter
	RouterLatency        prometheus.Histogram

	// Healer metrics
	HealerAttemptsTotal prometheus.Counter
	HealerResultTotal   *prometheus.CounterVec
	HealerMethodTotal   *prometheus.CounterVec
	HealerLatency       prometheus.Histogram

	// VLM metrics
	VLMCallsTotal *prometheus.CounterVec
	VLMLatency    prometheus.Histogram

	// PageWaiter metrics
	PageWaitDuration *prometheus.HistogramVec
	PageWaitTotal    *prometheus.CounterVec

	// Benchmark metrics
	BenchmarkPageWaitSeconds *prometheus.HistogramVec
	BenchmarkAccuracyRatio   *prometheus.GaugeVec
	BenchmarkTotal           *prometheus.CounterVec

	// Comparison metrics (PageWaiter vs Playwright gap tracking)
	ComparisonGapSeconds       *prometheus.HistogramVec
	ComparisonStrategyAccuracy *prometheus.GaugeVec

	// VLM Judge evaluation metrics
	VLMJudgeF1Score    prometheus.Gauge
	VLMJudgePrecision  prometheus.Gauge
	VLMJudgeRecall     prometheus.Gauge
	VLMJudgeAccuracy   prometheus.Gauge
	VLMJudgeCasesTotal *prometheus.CounterVec
	VLMJudgeLatency    prometheus.Histogram

	// Pattern phase metrics
	PatternPhaseCurrent   prometheus.Gauge
	PatternDiscoveryTotal prometheus.Counter
	PatternStableTotal    prometheus.Counter
	ModelEscalationTotal  *prometheus.CounterVec

	// Evaluation metrics (R111/R113)
	FalsePositiveTotal          *prometheus.CounterVec
	MutationAppliedTotal        *prometheus.CounterVec
	ComparisonResultTotal       *prometheus.CounterVec
	AccessibilityViolationTotal *prometheus.CounterVec
}

// NewMetrics registers and returns all uiauto metrics with the given
// prometheus.Registerer. Pass prometheus.DefaultRegisterer for global or a
// custom registry for testing.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		ExecutorActionsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "uiauto",
			Subsystem: "executor",
			Name:      "actions_total",
			Help:      "Total executor actions by outcome.",
		}, []string{"result"}),

		ExecutorCacheTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "uiauto",
			Subsystem: "executor",
			Name:      "cache_total",
			Help:      "Executor cache lookups by outcome.",
		}, []string{"result"}),

		ExecutorStructMatches: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "uiauto",
			Subsystem: "executor",
			Name:      "structural_matches_total",
			Help:      "Total structural similarity matches used.",
		}),

		ExecutorLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "uiauto",
			Subsystem: "executor",
			Name:      "action_duration_seconds",
			Help:      "Duration of executor actions.",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}),

		RouterAttemptsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "uiauto",
			Subsystem: "router",
			Name:      "attempts_total",
			Help:      "Router model tier attempts.",
		}, []string{"tier"}),

		RouterSuccessesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "uiauto",
			Subsystem: "router",
			Name:      "successes_total",
			Help:      "Router model tier successes.",
		}, []string{"tier"}),

		RouterPromotions: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "uiauto",
			Subsystem: "router",
			Name:      "promotions_total",
			Help:      "Total model tier promotions (light -> smart).",
		}),

		RouterDemotions: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "uiauto",
			Subsystem: "router",
			Name:      "demotions_total",
			Help:      "Total model tier demotions (smart -> light).",
		}),

		RouterLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "uiauto",
			Subsystem: "router",
			Name:      "action_duration_seconds",
			Help:      "Duration of router-dispatched actions.",
			Buckets:   []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
		}),

		HealerAttemptsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "uiauto",
			Subsystem: "healer",
			Name:      "attempts_total",
			Help:      "Total healing attempts.",
		}),

		HealerResultTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "uiauto",
			Subsystem: "healer",
			Name:      "results_total",
			Help:      "Healing outcomes by result.",
		}, []string{"result"}),

		HealerMethodTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "uiauto",
			Subsystem: "healer",
			Name:      "method_total",
			Help:      "Successful heals by method.",
		}, []string{"method"}),

		HealerLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "uiauto",
			Subsystem: "healer",
			Name:      "heal_duration_seconds",
			Help:      "Duration of healing operations.",
			Buckets:   []float64{0.1, 0.5, 1, 2.5, 5, 10, 30, 60},
		}),

		VLMCallsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "uiauto",
			Subsystem: "vlm",
			Name:      "calls_total",
			Help:      "VLM bridge calls by outcome.",
		}, []string{"result"}),

		VLMLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "uiauto",
			Subsystem: "vlm",
			Name:      "call_duration_seconds",
			Help:      "Duration of VLM calls.",
			Buckets:   []float64{0.5, 1, 2.5, 5, 10, 30, 60},
		}),

		PageWaitDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "uiauto",
			Subsystem: "page_wait",
			Name:      "duration_seconds",
			Help:      "Duration of page wait operations by strategy.",
			Buckets:   []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 15, 30},
		}, []string{"strategy"}),

		PageWaitTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "uiauto",
			Subsystem: "page_wait",
			Name:      "total",
			Help:      "Total page wait operations by strategy and outcome.",
		}, []string{"strategy", "result"}),

		BenchmarkPageWaitSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "uiauto",
			Subsystem: "benchmark",
			Name:      "page_wait_seconds",
			Help:      "Benchmark page wait duration by page_type and strategy.",
			Buckets:   []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 15, 30},
		}, []string{"page_type", "strategy"}),

		BenchmarkAccuracyRatio: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "uiauto",
			Subsystem: "benchmark",
			Name:      "accuracy_ratio",
			Help:      "Element-found accuracy ratio (0-1) by page_type and strategy.",
		}, []string{"page_type", "strategy"}),

		BenchmarkTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "uiauto",
			Subsystem: "benchmark",
			Name:      "total",
			Help:      "Total benchmark runs by page_type, strategy, and result.",
		}, []string{"page_type", "strategy", "result"}),

		ComparisonGapSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "uiauto",
			Subsystem: "comparison",
			Name:      "gap_seconds",
			Help:      "Time gap between PageWaiter and expected element visibility by strategy.",
			Buckets:   []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10},
		}, []string{"strategy", "page_type"}),

		ComparisonStrategyAccuracy: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "uiauto",
			Subsystem: "comparison",
			Name:      "strategy_accuracy",
			Help:      "Element-found accuracy (0-1) per strategy across benchmark runs.",
		}, []string{"strategy"}),

		VLMJudgeF1Score: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "uiauto",
			Subsystem: "vlm_judge",
			Name:      "f1_score",
			Help:      "Current F1 score of VLM-as-Judge evaluations.",
		}),

		VLMJudgePrecision: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "uiauto",
			Subsystem: "vlm_judge",
			Name:      "precision",
			Help:      "Current precision of VLM-as-Judge evaluations.",
		}),

		VLMJudgeRecall: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "uiauto",
			Subsystem: "vlm_judge",
			Name:      "recall",
			Help:      "Current recall of VLM-as-Judge evaluations.",
		}),

		VLMJudgeAccuracy: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "uiauto",
			Subsystem: "vlm_judge",
			Name:      "accuracy",
			Help:      "Current accuracy of VLM-as-Judge evaluations.",
		}),

		VLMJudgeCasesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "uiauto",
			Subsystem: "vlm_judge",
			Name:      "cases_total",
			Help:      "Total VLM judge evaluation cases by outcome.",
		}, []string{"result"}),

		VLMJudgeLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "uiauto",
			Subsystem: "vlm_judge",
			Name:      "eval_duration_seconds",
			Help:      "Duration of VLM judge evaluation runs.",
			Buckets:   []float64{0.5, 1, 2.5, 5, 10, 30, 60, 120},
		}),

		PatternPhaseCurrent: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "uiauto",
			Subsystem: "pattern",
			Name:      "phase_current",
			Help:      "Current pattern phase (0=discovery, 1=cruise, 2=escalation).",
		}),

		PatternDiscoveryTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "uiauto",
			Subsystem: "pattern",
			Name:      "discovery_total",
			Help:      "Total transitions into discovery phase.",
		}),

		PatternStableTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "uiauto",
			Subsystem: "pattern",
			Name:      "stable_total",
			Help:      "Total transitions into cruise (stable) phase.",
		}),

		ModelEscalationTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "uiauto",
			Subsystem: "model",
			Name:      "escalation_total",
			Help:      "Total phase escalations by from_phase and to_phase.",
		}, []string{"from_phase", "to_phase"}),

		FalsePositiveTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "uiauto",
			Subsystem: "eval",
			Name:      "false_positive_total",
			Help:      "Total false positive detections by page type.",
		}, []string{"page_type"}),

		MutationAppliedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "uiauto",
			Subsystem: "eval",
			Name:      "mutation_applied_total",
			Help:      "Total DOM mutations applied by tier and operator.",
		}, []string{"tier", "operator"}),

		ComparisonResultTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "uiauto",
			Subsystem: "eval",
			Name:      "comparison_result_total",
			Help:      "A/B comparison results by strategy and outcome.",
		}, []string{"strategy", "outcome"}),

		AccessibilityViolationTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "uiauto",
			Subsystem: "accessibility",
			Name:      "violation_total",
			Help:      "Accessibility violations detected by severity and rule.",
		}, []string{"severity", "rule"}),
	}

	reg.MustRegister(
		m.ExecutorActionsTotal,
		m.ExecutorCacheTotal,
		m.ExecutorStructMatches,
		m.ExecutorLatency,
		m.RouterAttemptsTotal,
		m.RouterSuccessesTotal,
		m.RouterPromotions,
		m.RouterDemotions,
		m.RouterLatency,
		m.HealerAttemptsTotal,
		m.HealerResultTotal,
		m.HealerMethodTotal,
		m.HealerLatency,
		m.VLMCallsTotal,
		m.VLMLatency,
		m.PageWaitDuration,
		m.PageWaitTotal,
		m.BenchmarkPageWaitSeconds,
		m.BenchmarkAccuracyRatio,
		m.BenchmarkTotal,
		m.ComparisonGapSeconds,
		m.ComparisonStrategyAccuracy,
		m.VLMJudgeF1Score,
		m.VLMJudgePrecision,
		m.VLMJudgeRecall,
		m.VLMJudgeAccuracy,
		m.VLMJudgeCasesTotal,
		m.VLMJudgeLatency,
		m.PatternPhaseCurrent,
		m.PatternDiscoveryTotal,
		m.PatternStableTotal,
		m.ModelEscalationTotal,
		m.FalsePositiveTotal,
		m.MutationAppliedTotal,
		m.ComparisonResultTotal,
		m.AccessibilityViolationTotal,
	)

	return m
}

// MetricsCollector bridges the internal atomic metrics from uiauto components
// to Prometheus. Call Collect() periodically or after each significant event.
type MetricsCollector struct {
	prom  *Metrics
	agent *MemberAgent

	// Track previously-seen counter values to compute deltas.
	prevExecutor        ExecutorMetricsSnapshot
	prevRouter          RouterMetrics
	prevHealer          HealerMetrics
	prevVLM             VLMMetrics
	prevPhaseDiscovery  int64
	prevPhaseStable     int64
	prevPhaseEscalation int64
	prevPhaseHistoryLen int
}

// NewMetricsCollector creates a collector that exports a MemberAgent's internal
// metrics to Prometheus. The caller must have already registered Metrics
// via NewMetrics.
func NewMetricsCollector(prom *Metrics, agent *MemberAgent) *MetricsCollector {
	return &MetricsCollector{prom: prom, agent: agent}
}

// Collect snapshots the MemberAgent's internal metrics and pushes deltas
// to Prometheus counters/histograms.
func (c *MetricsCollector) Collect() {
	agg := c.agent.Metrics()

	c.collectExecutor(agg.Executor)
	c.collectRouter(agg.Router)
	c.collectHealer(agg.Healer)
	if agg.VLM != nil {
		c.collectVLM(*agg.VLM)
	}
	c.collectPhase()
}

func (c *MetricsCollector) collectExecutor(cur ExecutorMetricsSnapshot) {
	if d := cur.SuccessActions - c.prevExecutor.SuccessActions; d > 0 {
		c.prom.ExecutorActionsTotal.WithLabelValues("success").Add(float64(d))
	}
	if d := cur.FailedActions - c.prevExecutor.FailedActions; d > 0 {
		c.prom.ExecutorActionsTotal.WithLabelValues("failure").Add(float64(d))
	}
	if d := cur.CacheHits - c.prevExecutor.CacheHits; d > 0 {
		c.prom.ExecutorCacheTotal.WithLabelValues("hit").Add(float64(d))
	}
	if d := cur.CacheMisses - c.prevExecutor.CacheMisses; d > 0 {
		c.prom.ExecutorCacheTotal.WithLabelValues("miss").Add(float64(d))
	}
	if d := cur.StructuralMatches - c.prevExecutor.StructuralMatches; d > 0 {
		c.prom.ExecutorStructMatches.Add(float64(d))
	}

	c.prevExecutor = cur
}

func (c *MetricsCollector) collectRouter(cur RouterMetrics) {
	if d := cur.LightAttempts - c.prevRouter.LightAttempts; d > 0 {
		c.prom.RouterAttemptsTotal.WithLabelValues("light").Add(float64(d))
	}
	if d := cur.SmartAttempts - c.prevRouter.SmartAttempts; d > 0 {
		c.prom.RouterAttemptsTotal.WithLabelValues("smart").Add(float64(d))
	}
	if d := cur.VLMAttempts - c.prevRouter.VLMAttempts; d > 0 {
		c.prom.RouterAttemptsTotal.WithLabelValues("vlm").Add(float64(d))
	}

	if d := cur.LightSuccesses - c.prevRouter.LightSuccesses; d > 0 {
		c.prom.RouterSuccessesTotal.WithLabelValues("light").Add(float64(d))
	}
	if d := cur.SmartSuccesses - c.prevRouter.SmartSuccesses; d > 0 {
		c.prom.RouterSuccessesTotal.WithLabelValues("smart").Add(float64(d))
	}
	if d := cur.VLMSuccesses - c.prevRouter.VLMSuccesses; d > 0 {
		c.prom.RouterSuccessesTotal.WithLabelValues("vlm").Add(float64(d))
	}

	if d := cur.Promotions - c.prevRouter.Promotions; d > 0 {
		c.prom.RouterPromotions.Add(float64(d))
	}
	if d := cur.Demotions - c.prevRouter.Demotions; d > 0 {
		c.prom.RouterDemotions.Add(float64(d))
	}

	c.prevRouter = cur
}

func (c *MetricsCollector) collectHealer(cur HealerMetrics) {
	if d := cur.TotalAttempts - c.prevHealer.TotalAttempts; d > 0 {
		c.prom.HealerAttemptsTotal.Add(float64(d))
	}
	if d := cur.SuccessfulHeals - c.prevHealer.SuccessfulHeals; d > 0 {
		c.prom.HealerResultTotal.WithLabelValues("success").Add(float64(d))
	}
	if d := cur.FailedHeals - c.prevHealer.FailedHeals; d > 0 {
		c.prom.HealerResultTotal.WithLabelValues("failure").Add(float64(d))
	}

	if d := cur.FingerprintHeals - c.prevHealer.FingerprintHeals; d > 0 {
		c.prom.HealerMethodTotal.WithLabelValues("fingerprint").Add(float64(d))
	}
	if d := cur.StructuralHeals - c.prevHealer.StructuralHeals; d > 0 {
		c.prom.HealerMethodTotal.WithLabelValues("structural").Add(float64(d))
	}
	if d := cur.SmartLLMHeals - c.prevHealer.SmartLLMHeals; d > 0 {
		c.prom.HealerMethodTotal.WithLabelValues("smart_llm").Add(float64(d))
	}
	if d := cur.VLMHeals - c.prevHealer.VLMHeals; d > 0 {
		c.prom.HealerMethodTotal.WithLabelValues("vlm").Add(float64(d))
	}

	c.prevHealer = cur
}

func (c *MetricsCollector) collectVLM(cur VLMMetrics) {
	if d := cur.SuccessCalls - c.prevVLM.SuccessCalls; d > 0 {
		c.prom.VLMCallsTotal.WithLabelValues("success").Add(float64(d))
	}
	if d := cur.FailedCalls - c.prevVLM.FailedCalls; d > 0 {
		c.prom.VLMCallsTotal.WithLabelValues("failure").Add(float64(d))
	}

	c.prevVLM = cur
}

func (c *MetricsCollector) collectPhase() {
	router := c.agent.Router()
	if router == nil {
		return
	}
	pt := router.PhaseTracker()
	if pt == nil {
		return
	}

	phase := pt.CurrentPhase()
	c.prom.PatternPhaseCurrent.Set(float64(phase))

	stats := pt.Stats()
	if d := stats.DiscoveryEntries - c.prevPhaseDiscovery; d > 0 {
		c.prom.PatternDiscoveryTotal.Add(float64(d))
	}
	if d := stats.StableEntries - c.prevPhaseStable; d > 0 {
		c.prom.PatternStableTotal.Add(float64(d))
	}

	history := pt.History()
	for i := c.prevPhaseHistoryLen; i < len(history); i++ {
		t := history[i]
		if t.To == PhaseEscalation {
			c.prom.ModelEscalationTotal.WithLabelValues(t.From.String(), t.To.String()).Inc()
		}
	}

	c.prevPhaseDiscovery = stats.DiscoveryEntries
	c.prevPhaseStable = stats.StableEntries
	c.prevPhaseEscalation = stats.EscalationCount
	c.prevPhaseHistoryLen = len(history)
}

// CollectJudgeReport publishes a JudgeEvalReport's metrics to Prometheus gauges.
func (c *MetricsCollector) CollectJudgeReport(report *JudgeEvalReport) {
	if report == nil {
		return
	}
	c.prom.VLMJudgeF1Score.Set(report.F1Score)
	c.prom.VLMJudgePrecision.Set(report.Precision)
	c.prom.VLMJudgeRecall.Set(report.Recall)
	c.prom.VLMJudgeAccuracy.Set(report.Accuracy)

	for _, r := range report.Results {
		if r.Correct {
			c.prom.VLMJudgeCasesTotal.WithLabelValues("correct").Inc()
		} else {
			c.prom.VLMJudgeCasesTotal.WithLabelValues("incorrect").Inc()
		}
	}
	if report.AvgLatencyMs > 0 {
		c.prom.VLMJudgeLatency.Observe(report.AvgLatencyMs / 1000.0)
	}
}
