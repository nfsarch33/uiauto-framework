package bandits

import (
	"math"
	"math/rand/v2"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.InitialAlpha != 1.0 {
		t.Errorf("expected InitialAlpha=1.0, got %f", cfg.InitialAlpha)
	}
	if cfg.WarmupRounds != 5 {
		t.Errorf("expected WarmupRounds=5, got %d", cfg.WarmupRounds)
	}
	if cfg.CostWeight < 0 || cfg.CostWeight > 1 {
		t.Errorf("CostWeight %f out of [0,1]", cfg.CostWeight)
	}
	if cfg.DecayFactor <= 0 || cfg.DecayFactor > 1 {
		t.Errorf("DecayFactor %f out of (0,1]", cfg.DecayFactor)
	}
}

func TestArmStringAndCost(t *testing.T) {
	tests := []struct {
		arm     Arm
		name    string
		minCost float64
		maxCost float64
	}{
		{ArmLight, "light", 0, 0},
		{ArmSmart, "smart", 0.5, 2.0},
		{ArmVLM, "vlm", 3.0, 10.0},
	}
	for _, tt := range tests {
		if tt.arm.String() != tt.name {
			t.Errorf("Arm(%d).String() = %q, want %q", tt.arm, tt.arm.String(), tt.name)
		}
		cost := tt.arm.CostPerCall()
		if cost < tt.minCost || cost > tt.maxCost {
			t.Errorf("Arm(%d).CostPerCall() = %f, want [%f, %f]", tt.arm, cost, tt.minCost, tt.maxCost)
		}
	}
}

func TestFeaturesContextKey(t *testing.T) {
	tests := []struct {
		name     string
		features Features
		want     string
	}{
		{"simple stable page", Features{PageComplexity: 0.1, HasDataTestID: true}, "low:stable"},
		{"complex unstable", Features{PageComplexity: 0.8, MutationIntensity: 0.5}, "high:unstable"},
		{"mid stable", Features{PageComplexity: 0.4, HasDataTestID: true}, "mid:stable"},
		{"low but no test-id", Features{PageComplexity: 0.1, HasDataTestID: false}, "low:unstable"},
		{"mid with failures", Features{PageComplexity: 0.5, PreviousFailures: 3, HasDataTestID: true}, "mid:unstable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.features.ContextKey()
			if got != tt.want {
				t.Errorf("ContextKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBetaPosteriorMean(t *testing.T) {
	p := BetaPosterior{Alpha: 10, Beta: 10}
	if math.Abs(p.Mean()-0.5) > 0.01 {
		t.Errorf("Mean() = %f, want ~0.5", p.Mean())
	}

	p2 := BetaPosterior{Alpha: 90, Beta: 10}
	if math.Abs(p2.Mean()-0.9) > 0.01 {
		t.Errorf("Mean() = %f, want ~0.9", p2.Mean())
	}
}

func TestBetaSample(t *testing.T) {
	rng := rand.New(rand.NewPCG(42, 42))
	p := BetaPosterior{Alpha: 5, Beta: 5}

	var sum float64
	n := 10000
	for range n {
		s := p.Sample(rng)
		if s < 0 || s > 1 {
			t.Fatalf("Sample() = %f, out of [0,1]", s)
		}
		sum += s
	}
	mean := sum / float64(n)
	if math.Abs(mean-0.5) > 0.05 {
		t.Errorf("sample mean = %f, expected ~0.5 for Beta(5,5)", mean)
	}
}

func TestWarmupExploresAllArms(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WarmupRounds = 3
	bandit := NewContextualBandit(cfg, 42)
	features := Features{PageComplexity: 0.2, HasDataTestID: true}

	// During warmup, the bandit fills each arm's trial count to WarmupRounds
	// before moving to Thompson Sampling. Track which arms were selected.
	armCounts := [NumArms]int{}
	totalWarmup := cfg.WarmupRounds * NumArms
	for range totalWarmup {
		arm := bandit.SelectArm(features)
		bandit.Update(features, arm, 1.0)
		armCounts[arm]++
	}

	// After warmup, every arm must have exactly WarmupRounds observations
	for i := 0; i < NumArms; i++ {
		if armCounts[i] != cfg.WarmupRounds {
			t.Errorf("arm %d got %d warmup pulls, want %d", i, armCounts[i], cfg.WarmupRounds)
		}
	}
}

func TestConvergenceToHighRewardArm(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WarmupRounds = 2
	cfg.CostWeight = 0.0 // disable cost penalty for this test
	cfg.DecayFactor = 1.0
	bandit := NewContextualBandit(cfg, 123)

	features := Features{PageComplexity: 0.5, HasDataTestID: true}

	// Reward schedule: light=0.3, smart=0.9, vlm=0.6
	rewards := [NumArms]float64{0.3, 0.9, 0.6}

	rng := rand.New(rand.NewPCG(456, 789))

	// Run warmup
	for i := 0; i < cfg.WarmupRounds*NumArms; i++ {
		arm := bandit.SelectArm(features)
		reward := 0.0
		if rng.Float64() < rewards[arm] {
			reward = 1.0
		}
		bandit.Update(features, arm, reward)
	}

	// Run 500 more rounds
	armCounts := [NumArms]int{}
	for range 500 {
		arm := bandit.SelectArm(features)
		reward := 0.0
		if rng.Float64() < rewards[arm] {
			reward = 1.0
		}
		bandit.Update(features, arm, reward)
		armCounts[arm]++
	}

	// Smart (arm 1) should be selected most often since it has the highest reward
	if armCounts[ArmSmart] < armCounts[ArmLight] || armCounts[ArmSmart] < armCounts[ArmVLM] {
		t.Errorf("expected smart arm to dominate: light=%d, smart=%d, vlm=%d",
			armCounts[ArmLight], armCounts[ArmSmart], armCounts[ArmVLM])
	}
}

func TestCostPenaltyFavorsLight(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WarmupRounds = 2
	cfg.CostWeight = 0.8 // heavy cost penalty
	cfg.DecayFactor = 1.0
	bandit := NewContextualBandit(cfg, 42)

	features := Features{PageComplexity: 0.2, HasDataTestID: true}

	// All arms have equal reward probability (0.8)
	rng := rand.New(rand.NewPCG(100, 200))

	for i := 0; i < cfg.WarmupRounds*NumArms; i++ {
		arm := bandit.SelectArm(features)
		reward := 0.0
		if rng.Float64() < 0.8 {
			reward = 1.0
		}
		bandit.Update(features, arm, reward)
	}

	armCounts := [NumArms]int{}
	for range 300 {
		arm := bandit.SelectArm(features)
		reward := 0.0
		if rng.Float64() < 0.8 {
			reward = 1.0
		}
		bandit.Update(features, arm, reward)
		armCounts[arm]++
	}

	// With heavy cost penalty and equal rewards, light arm should dominate
	if armCounts[ArmLight] < armCounts[ArmVLM] {
		t.Errorf("expected light arm to dominate with high cost penalty: light=%d, smart=%d, vlm=%d",
			armCounts[ArmLight], armCounts[ArmSmart], armCounts[ArmVLM])
	}
}

func TestMultipleContextsIndependent(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WarmupRounds = 1
	cfg.CostWeight = 0.0
	cfg.DecayFactor = 1.0
	bandit := NewContextualBandit(cfg, 42)

	stableFeatures := Features{PageComplexity: 0.1, HasDataTestID: true}
	unstableFeatures := Features{PageComplexity: 0.9, MutationIntensity: 0.5}

	// Train stable context: light always wins
	for range 50 {
		bandit.Update(stableFeatures, ArmLight, 1.0)
		bandit.Update(stableFeatures, ArmSmart, 0.0)
		bandit.Update(stableFeatures, ArmVLM, 0.0)
	}

	// Train unstable context: VLM always wins
	for range 50 {
		bandit.Update(unstableFeatures, ArmLight, 0.0)
		bandit.Update(unstableFeatures, ArmSmart, 0.0)
		bandit.Update(unstableFeatures, ArmVLM, 1.0)
	}

	// Verify separate contexts track independently
	stableStats := bandit.Stats(stableFeatures)
	unstableStats := bandit.Stats(unstableFeatures)

	if stableStats[ArmLight].Mean() < 0.8 {
		t.Errorf("stable context light mean = %f, expected > 0.8", stableStats[ArmLight].Mean())
	}
	if unstableStats[ArmVLM].Mean() < 0.8 {
		t.Errorf("unstable context VLM mean = %f, expected > 0.8", unstableStats[ArmVLM].Mean())
	}

	if bandit.ContextCount() < 2 {
		t.Errorf("expected at least 2 contexts, got %d", bandit.ContextCount())
	}
}

func TestDecayReducesOldObservations(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WarmupRounds = 0
	cfg.CostWeight = 0.0
	cfg.DecayFactor = 0.9 // aggressive decay for testing

	bandit := NewContextualBandit(cfg, 42)
	features := Features{PageComplexity: 0.5, HasDataTestID: true}

	// Build strong prior for light arm
	for range 20 {
		bandit.Update(features, ArmLight, 1.0)
	}

	beforeMean := bandit.Stats(features)[ArmLight].Mean()

	// Now update smart arm many times (decay erodes light's advantage)
	for range 30 {
		bandit.Update(features, ArmSmart, 1.0)
	}

	afterMean := bandit.Stats(features)[ArmLight].Mean()

	// Light's mean should have decreased due to decay
	if afterMean >= beforeMean {
		t.Errorf("decay should reduce old means: before=%f, after=%f", beforeMean, afterMean)
	}
}

func TestResetClearsPosteriors(t *testing.T) {
	cfg := DefaultConfig()
	bandit := NewContextualBandit(cfg, 42)
	features := Features{PageComplexity: 0.5, HasDataTestID: true}

	bandit.Update(features, ArmLight, 1.0)
	if bandit.TotalPulls() != 1 {
		t.Fatalf("expected 1 pull, got %d", bandit.TotalPulls())
	}

	bandit.Reset()

	if bandit.TotalPulls() != 0 {
		t.Errorf("expected 0 pulls after reset, got %d", bandit.TotalPulls())
	}
	if bandit.ContextCount() != 0 {
		t.Errorf("expected 0 contexts after reset, got %d", bandit.ContextCount())
	}
}

func TestBestArmGreedy(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WarmupRounds = 0
	cfg.CostWeight = 0.0
	cfg.DecayFactor = 1.0
	bandit := NewContextualBandit(cfg, 42)

	features := Features{PageComplexity: 0.5, HasDataTestID: true}

	// Make smart arm clearly best
	for range 100 {
		bandit.Update(features, ArmSmart, 1.0)
	}
	for range 100 {
		bandit.Update(features, ArmLight, 0.0)
	}
	for range 100 {
		bandit.Update(features, ArmVLM, 0.5)
	}

	best := bandit.BestArm(features)
	if best != ArmSmart {
		t.Errorf("BestArm() = %v, want smart", best)
	}
}

func TestGammaSamplePositive(t *testing.T) {
	rng := rand.New(rand.NewPCG(42, 42))
	for range 1000 {
		s := gammaSample(rng, 0.5)
		if s < 0 {
			t.Fatalf("gammaSample returned negative: %f", s)
		}
	}
}
