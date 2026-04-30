package uiauto

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestUnifiedMetricsRegistry(t *testing.T) {
	umr := NewUnifiedMetricsRegistry()
	if umr.Registry == nil {
		t.Fatal("registry is nil")
	}
	if umr.AppMetrics == nil {
		t.Fatal("app metrics nil")
	}
	if umr.RuntimeMetrics == nil {
		t.Fatal("runtime metrics nil")
	}

	umr.RecordOp("store", "get", nil, 5*time.Millisecond)
	umr.RecordOp("store", "set", errors.New("write fail"), 10*time.Millisecond)

	families, err := umr.Registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	found := false
	for _, f := range families {
		if f.GetName() == "uiauto_operations_total" {
			found = true
			break
		}
	}
	if !found {
		t.Error("uiauto_operations_total not found")
	}
}

func TestSlogMetricsHandler(t *testing.T) {
	reg := prometheus.NewPedanticRegistry()
	inner := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
	handler := NewSlogMetricsHandler(inner, reg)

	logger := slog.New(handler)
	logger.Info("test info message")
	logger.Warn("test warn message")

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	found := false
	for _, f := range families {
		if f.GetName() == "uiauto_log_messages_total" {
			found = true
			break
		}
	}
	if !found {
		t.Error("uiauto_log_messages_total not found")
	}

	if !handler.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("handler should be enabled for INFO")
	}
}
