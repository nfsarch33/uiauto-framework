package doctor

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetrics_RecordReport(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	report := &Report{
		Suites: []Suite{
			{
				Name: "evolver",
				Checks: []Check{
					{Name: "docker", Status: StatusPass, Message: "ok"},
					{Name: "llm", Status: StatusFail, Message: "down"},
					{Name: "mem0", Status: StatusWarn, Message: "degraded"},
				},
			},
			{
				Name: "research",
				Checks: []Check{
					{Name: "browser", Status: StatusPass, Message: "ok"},
					{Name: "pdf", Status: StatusPass, Message: "ok"},
				},
			},
		},
		Overall:  "unhealthy",
		Duration: 5 * time.Second,
	}

	m.RecordReport(report)

	families, err := reg.Gather()
	require.NoError(t, err)

	familyMap := make(map[string]*dto.MetricFamily)
	for _, f := range families {
		familyMap[f.GetName()] = f
	}

	overall := familyMap["agent_doctor_overall_healthy"]
	require.NotNil(t, overall)
	assert.Equal(t, 0.0, overall.Metric[0].Gauge.GetValue())

	totalChecks := familyMap["agent_doctor_total_checks"]
	require.NotNil(t, totalChecks)
	assert.Equal(t, 5.0, totalChecks.Metric[0].Gauge.GetValue())

	passedChecks := familyMap["agent_doctor_passed_checks"]
	require.NotNil(t, passedChecks)
	assert.Equal(t, 3.0, passedChecks.Metric[0].Gauge.GetValue())

	duration := familyMap["agent_doctor_run_duration_seconds"]
	require.NotNil(t, duration)
	assert.Equal(t, 5.0, duration.Metric[0].Gauge.GetValue())
}

func TestMetrics_RecordReport_Nil(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	m.RecordReport(nil)

	families, err := reg.Gather()
	require.NoError(t, err)
	for _, f := range families {
		if f.GetName() == "agent_doctor_total_checks" {
			assert.Equal(t, 0.0, f.Metric[0].Gauge.GetValue())
		}
	}
}

func TestMetrics_RecordReport_Healthy(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	report := &Report{
		Suites: []Suite{
			{
				Name: "test",
				Checks: []Check{
					{Name: "a", Status: StatusPass},
				},
			},
		},
		Overall:  "healthy",
		Duration: 1 * time.Second,
	}

	m.RecordReport(report)

	families, err := reg.Gather()
	require.NoError(t, err)

	for _, f := range families {
		if f.GetName() == "agent_doctor_overall_healthy" {
			assert.Equal(t, 1.0, f.Metric[0].Gauge.GetValue())
		}
	}
}
