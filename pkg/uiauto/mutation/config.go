package mutation

import "math/rand"

// Intensity controls how aggressively mutations are applied.
type Intensity string

const (
	IntensityLow    Intensity = "low"    // 10% of elements
	IntensityMedium Intensity = "medium" // 30% of elements
	IntensityHigh   Intensity = "high"   // 60% of elements
)

// Config controls a mutation run.
type Config struct {
	Intensity    Intensity
	TierWeights  map[Tier]float64
	Seed         int64
	MaxMutations int
}

// DefaultConfig returns a Config suitable for evaluation testing.
func DefaultConfig() Config {
	return Config{
		Intensity: IntensityMedium,
		TierWeights: map[Tier]float64{
			TierA: 0.6,
			TierB: 0.3,
			TierC: 0.1,
		},
		Seed:         0,
		MaxMutations: 50,
	}
}

// IntensityRate returns the fraction of elements to mutate.
func (c Config) IntensityRate() float64 {
	switch c.Intensity {
	case IntensityLow:
		return 0.10
	case IntensityMedium:
		return 0.30
	case IntensityHigh:
		return 0.60
	default:
		return 0.30
	}
}

// Rng returns a seeded random source. If Seed is 0, uses a default seed.
func (c Config) Rng() *rand.Rand {
	seed := c.Seed
	if seed == 0 {
		seed = 42
	}
	return rand.New(rand.NewSource(seed))
}
