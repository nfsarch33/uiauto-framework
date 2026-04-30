package uiauto_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/signal"
	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/store"
)

func TestM5IntegrationStoreWithCircuitBreaker(t *testing.T) {
	primary, err := store.NewJSONFileStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer primary.Close()

	fallback, err := store.NewJSONFileStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer fallback.Close()

	cfg := store.CircuitBreakerConfig{
		FailureThreshold: 2,
		HalfOpenTimeout:  50 * time.Millisecond,
	}
	fs := store.NewFallbackStore(primary, fallback, cfg)

	ctx := context.Background()

	// Write patterns through the fallback store
	for i := 0; i < 5; i++ {
		p := store.Pattern{
			ID:         fmt.Sprintf("elem-%d", i),
			Selector:   fmt.Sprintf("#btn-%d", i),
			LastSeen:   time.Now(),
			Confidence: 0.9,
		}
		if err := fs.Set(ctx, p); err != nil {
			t.Fatalf("Set elem-%d: %v", i, err)
		}
	}

	// Verify all patterns accessible
	patterns, err := fs.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(patterns) != 5 {
		t.Errorf("Load() = %d patterns, want 5", len(patterns))
	}

	// Circuit should be closed
	if fs.State() != store.CircuitClosed {
		t.Errorf("state = %v, want closed", fs.State())
	}
}

func TestM5IntegrationStoreDecayAndBoost(t *testing.T) {
	s, _ := store.NewJSONFileStore(":memory:")
	defer s.Close()
	ctx := context.Background()

	s.Set(ctx, store.Pattern{
		ID: "decaying", Selector: ".old", LastSeen: time.Now().Add(-72 * time.Hour), Confidence: 1.0,
	})
	s.Set(ctx, store.Pattern{
		ID: "fresh", Selector: ".new", LastSeen: time.Now(), Confidence: 0.8,
	})

	s.DecayConfidence(ctx, 24*time.Hour, 0.5)

	old, _ := s.Get(ctx, "decaying")
	if old.Confidence > 0.55 {
		t.Errorf("decayed confidence = %f, want ~0.5", old.Confidence)
	}

	s.BoostConfidence(ctx, "decaying", 0.3)
	boosted, _ := s.Get(ctx, "decaying")
	if boosted.Confidence < 0.75 || boosted.Confidence > 0.85 {
		t.Errorf("boosted confidence = %f, want ~0.8", boosted.Confidence)
	}
}

func TestM5IntegrationSignalWithStore(t *testing.T) {
	emitter := signal.NewEmitter(signal.WithDebounce(10 * time.Millisecond))
	handler, getter := signal.CollectorHandler()
	emitter.On(handler)

	primary, _ := store.NewJSONFileStore(":memory:")
	fallback, _ := store.NewJSONFileStore(":memory:")
	defer primary.Close()
	defer fallback.Close()

	cfg := store.CircuitBreakerConfig{FailureThreshold: 2, HalfOpenTimeout: 30 * time.Millisecond}
	fs := store.NewFallbackStore(primary, fallback, cfg)

	ctx := context.Background()

	// Simulate writing patterns and emitting signals
	for i := 0; i < 3; i++ {
		p := store.Pattern{
			ID: fmt.Sprintf("sig-%d", i), Selector: fmt.Sprintf(".s%d", i),
			LastSeen: time.Now(), Confidence: 0.9,
		}
		fs.Set(ctx, p)

		signal.EmitHealResult(emitter, signal.HealEvent{
			TargetID: p.ID, Method: "data-attribute", Success: true, Duration: 100 * time.Millisecond,
		})
	}

	// Due to debounce, not all heal signals may have been delivered
	signals := getter()
	if len(signals) < 1 {
		t.Error("expected at least 1 signal")
	}

	// Emit circuit state change
	signal.EmitCircuitChange(emitter, signal.CircuitEvent{
		StoreName: "primary", OldState: "closed", NewState: "closed", Failures: 0,
	})

	allSignals := getter()
	hasCircuit := false
	for _, s := range allSignals {
		if s.Category == signal.CategoryCircuit {
			hasCircuit = true
		}
	}
	if !hasCircuit {
		t.Error("expected at least one circuit signal")
	}
}

func TestM5IntegrationSignalBriefFormat(t *testing.T) {
	emitter := signal.NewEmitter()
	handler, getter := signal.CollectorHandler()
	emitter.On(handler)

	signal.EmitTestResult(emitter, signal.TestEvent{
		Suite: "uiauto/store", Passed: 15, Failed: 0, Skipped: 0, Duration: 2 * time.Second,
	})
	signal.EmitTodoCompleted(emitter, signal.TodoEvent{
		ID: "sprint11", Title: "SQLite store", Status: "completed", Sprint: "sprint-11",
	})

	signals := getter()
	for _, s := range signals {
		brief := s.Format(true)
		verbose := s.Format(false)
		if len(brief) == 0 {
			t.Error("brief output empty")
		}
		if len(verbose) <= len(brief) {
			t.Error("verbose should be longer than brief")
		}
	}
}

// TestM5EnduranceDryRun simulates a compressed endurance test cycle.
// In production, this would run for 1 hour; here we do 10 rapid cycles.
func TestM5EnduranceDryRun(t *testing.T) {
	primary, _ := store.NewJSONFileStore(":memory:")
	fallback, _ := store.NewJSONFileStore(":memory:")
	defer primary.Close()
	defer fallback.Close()

	fs := store.NewFallbackStore(primary, fallback, store.DefaultCircuitBreakerConfig())
	emitter := signal.NewEmitter(signal.WithDebounce(5 * time.Millisecond))
	handler, getter := signal.CollectorHandler()
	emitter.On(handler)

	ctx := context.Background()
	cycles := 10
	start := time.Now()

	for cycle := 0; cycle < cycles; cycle++ {
		// Simulate pattern write
		p := store.Pattern{
			ID:         fmt.Sprintf("endurance-%d", cycle),
			Selector:   fmt.Sprintf("#e-%d", cycle),
			LastSeen:   time.Now(),
			Confidence: 0.85,
		}
		fs.Set(ctx, p)

		// Simulate heal
		signal.EmitHealResult(emitter, signal.HealEvent{
			TargetID: p.ID, Method: "css-path", Success: cycle%3 != 0, Duration: 50 * time.Millisecond,
		})

		// Periodic decay
		if cycle%5 == 0 {
			fs.DecayConfidence(ctx, 1*time.Hour, 0.95)
		}
	}

	elapsed := time.Since(start)

	// Verify store integrity
	patterns, err := fs.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(patterns) != cycles {
		t.Errorf("patterns = %d, want %d", len(patterns), cycles)
	}

	// Verify signals emitted
	signals := getter()
	if len(signals) < 1 {
		t.Error("expected signals from endurance run")
	}

	// Circuit should remain closed with healthy primary
	if fs.State() != store.CircuitClosed {
		t.Errorf("circuit = %v after endurance, want closed", fs.State())
	}

	signal.EmitTestResult(emitter, signal.TestEvent{
		Suite: "endurance-dry-run", Passed: cycles, Failed: 0, Duration: elapsed,
	})

	t.Logf("Endurance dry run: %d cycles in %s, %d signals, circuit=%s",
		cycles, elapsed, len(getter()), fs.State())
}
