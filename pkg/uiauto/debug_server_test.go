package uiauto

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestRuntimeMetricsUpdate(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	rm := NewRuntimeMetrics(reg)
	rm.Update()

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather failed: %v", err)
	}

	expected := map[string]bool{
		"uiauto_runtime_heap_alloc_bytes":    false,
		"uiauto_runtime_goroutines_active":   false,
		"uiauto_runtime_heap_sys_bytes":      false,
		"uiauto_runtime_gc_pause_ns":         false,
		"uiauto_runtime_gc_completed_total":  false,
		"uiauto_runtime_stack_inuse_bytes":   false,
		"uiauto_runtime_heap_objects":        false,
		"uiauto_runtime_heap_released_bytes": false,
	}
	for _, f := range families {
		if _, ok := expected[f.GetName()]; ok {
			expected[f.GetName()] = true
		}
	}
	for name, found := range expected {
		if !found {
			t.Errorf("metric %q not found after Update()", name)
		}
	}
}

func TestDebugServerHealthz(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reg := prometheus.NewRegistry()
	cfg := DebugServerConfig{
		Addr:     ":0",
		Registry: reg,
	}

	// Use a fixed port for test
	cfg.Addr = "127.0.0.1:16061"
	errCh := make(chan error, 1)
	go func() {
		errCh <- StartDebugServer(ctx, cfg)
	}()

	// Give the server time to start
	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://127.0.0.1:16061/healthz")
	if err != nil {
		t.Fatalf("healthz request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("healthz status = %d, want 200", resp.StatusCode)
	}

	resp2, err := http.Get("http://127.0.0.1:16061/metrics")
	if err != nil {
		t.Fatalf("metrics request failed: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("metrics status = %d, want 200", resp2.StatusCode)
	}

	cancel()
	select {
	case srvErr := <-errCh:
		if srvErr != nil {
			t.Errorf("server error: %v", srvErr)
		}
	case <-time.After(3 * time.Second):
		t.Error("server did not shut down in time")
	}
}
