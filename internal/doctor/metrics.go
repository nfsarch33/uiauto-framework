package doctor

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics exposes doctor check results as Prometheus gauges.
type Metrics struct {
	CheckStatus   *prometheus.GaugeVec
	SuiteStatus   *prometheus.GaugeVec
	OverallStatus prometheus.Gauge
	RunDuration   prometheus.Gauge
	TotalChecks   prometheus.Gauge
	PassedChecks  prometheus.Gauge
}

// NewMetrics registers doctor metrics with the given registerer.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		CheckStatus: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "agent_doctor",
			Name:      "check_status",
			Help:      "Status of each doctor check (0=pass, 1=warn, 2=fail, 3=skip).",
		}, []string{"suite", "check"}),
		SuiteStatus: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "agent_doctor",
			Name:      "suite_pass_ratio",
			Help:      "Ratio of passing checks per suite (0.0-1.0).",
		}, []string{"suite"}),
		OverallStatus: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "agent_doctor",
			Name:      "overall_healthy",
			Help:      "1 if overall healthy, 0 otherwise.",
		}),
		RunDuration: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "agent_doctor",
			Name:      "run_duration_seconds",
			Help:      "Duration of last doctor run in seconds.",
		}),
		TotalChecks: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "agent_doctor",
			Name:      "total_checks",
			Help:      "Total number of checks in last run.",
		}),
		PassedChecks: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "agent_doctor",
			Name:      "passed_checks",
			Help:      "Number of passing checks in last run.",
		}),
	}

	reg.MustRegister(
		m.CheckStatus, m.SuiteStatus,
		m.OverallStatus, m.RunDuration,
		m.TotalChecks, m.PassedChecks,
	)
	return m
}

// RecordReport updates all gauges from a doctor Report.
func (m *Metrics) RecordReport(r *Report) {
	if r == nil {
		return
	}

	for _, suite := range r.Suites {
		pass := 0
		for _, check := range suite.Checks {
			m.CheckStatus.WithLabelValues(suite.Name, check.Name).Set(float64(check.Status))
			if check.Status == StatusPass {
				pass++
			}
		}
		ratio := 0.0
		if len(suite.Checks) > 0 {
			ratio = float64(pass) / float64(len(suite.Checks))
		}
		m.SuiteStatus.WithLabelValues(suite.Name).Set(ratio)
	}

	if r.Overall == "healthy" {
		m.OverallStatus.Set(1)
	} else {
		m.OverallStatus.Set(0)
	}

	m.RunDuration.Set(r.Duration.Seconds())
	m.TotalChecks.Set(float64(r.TotalChecks()))
	m.PassedChecks.Set(float64(r.TotalPass()))
}
