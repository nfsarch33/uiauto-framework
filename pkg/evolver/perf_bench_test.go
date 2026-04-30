package evolver

import (
	"context"
	"testing"
)

func BenchmarkFleetCoordinator_RegisterNode(b *testing.B) {
	fc := NewFleetCoordinator()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fc.RegisterNode(FleetNode{ID: "node-bench", Hostname: "bench", Platform: "linux"})
	}
}

func BenchmarkFleetCoordinator_SharePattern(b *testing.B) {
	fc := NewFleetCoordinator()
	fc.RegisterNode(FleetNode{ID: "src", Hostname: "src", Platform: "linux"})
	fc.RegisterNode(FleetNode{ID: "dst", Hostname: "dst", Platform: "linux"})
	ctx := context.Background()
	share := PatternShare{SourceNode: "src", TargetNode: "dst", PatternID: "p1", PatternData: "data"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = fc.SharePattern(ctx, share)
	}
}

func BenchmarkFeedbackLoop_Evaluate(b *testing.B) {
	fl := NewFeedbackLoop()
	for i := 0; i < 100; i++ {
		fl.IngestSignal(FeedbackSignal{Source: "bench", Metric: "rate", Value: 0.5, Threshold: 0.3})
	}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fl.Evaluate(ctx)
	}
}

func BenchmarkEvolutionSandbox_RunEvolution(b *testing.B) {
	sandbox := NewEvolutionSandbox(DefaultSandboxConfig(), NewHITLGate(true))
	ctx := context.Background()
	mutFn := func() (string, error) { return "mutated", nil }
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sandbox.RunEvolution(ctx, "cap-bench", mutFn)
	}
}
