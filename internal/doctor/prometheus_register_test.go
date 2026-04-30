package doctor

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestCheckMetricsRegisterable_Pass(t *testing.T) {
	ch := CheckMetricsRegisterable("test_metrics", func(reg prometheus.Registerer) {
		c := prometheus.NewCounter(prometheus.CounterOpts{Name: "test_check_metrics_pass_total", Help: "x"})
		reg.MustRegister(c)
	})
	if ch.Status != StatusPass {
		t.Fatalf("expected pass, got %v: %s", ch.Status, ch.Message)
	}
}

func TestCheckMetricsRegisterable_DuplicatePanics(t *testing.T) {
	ch := CheckMetricsRegisterable("dup_metrics", func(reg prometheus.Registerer) {
		c := prometheus.NewCounter(prometheus.CounterOpts{Name: "test_dup_metric_total", Help: "x"})
		reg.MustRegister(c)
		reg.MustRegister(c)
	})
	if ch.Status != StatusFail {
		t.Fatalf("expected fail from duplicate register panic, got %v: %s", ch.Status, ch.Message)
	}
}
