package bandits

// Arm represents a tier in the self-healing cascade.
type Arm int

const (
	ArmLight Arm = iota // T1: cached patterns / lightweight heuristics
	ArmSmart            // T2: LLM-based DOM analysis
	ArmVLM              // T3: screenshot-based VLM fallback
	NumArms  = 3
)

func (a Arm) String() string {
	switch a {
	case ArmLight:
		return "light"
	case ArmSmart:
		return "smart"
	case ArmVLM:
		return "vlm"
	default:
		return "unknown"
	}
}

// CostPerCall returns the approximate relative cost for each arm.
// Values are normalized so T1=0 (cached), T2=1.0 (API call), T3=5.0 (VLM).
func (a Arm) CostPerCall() float64 {
	switch a {
	case ArmLight:
		return 0.0
	case ArmSmart:
		return 1.0
	case ArmVLM:
		return 5.0
	default:
		return 10.0
	}
}

// Config controls bandit behavior.
type Config struct {
	// InitialAlpha and InitialBeta are the Beta distribution priors.
	// Alpha=1, Beta=1 gives a uniform prior (no bias).
	InitialAlpha float64
	InitialBeta  float64

	// WarmupRounds is the minimum number of trials per arm before
	// Thompson Sampling takes effect. During warmup, arms are
	// selected round-robin.
	WarmupRounds int

	// CostWeight controls the cost-reward trade-off [0,1].
	// 0 = pure reward maximization, 1 = pure cost minimization.
	CostWeight float64

	// DecayFactor controls how quickly old observations lose influence.
	// 1.0 = no decay (all history equally weighted).
	// 0.99 = recent observations dominate after ~100 rounds.
	DecayFactor float64
}

// DefaultConfig returns sensible defaults for the UI test cascade.
func DefaultConfig() Config {
	return Config{
		InitialAlpha: 1.0,
		InitialBeta:  1.0,
		WarmupRounds: 5,
		CostWeight:   0.15,
		DecayFactor:  0.995,
	}
}
