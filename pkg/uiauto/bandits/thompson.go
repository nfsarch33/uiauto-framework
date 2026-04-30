package bandits

import (
	"math"
	"math/rand/v2"
	"sync"
)

// BetaPosterior tracks the posterior distribution for one arm in one context.
type BetaPosterior struct {
	Alpha  float64 // successes + prior
	Beta   float64 // failures + prior
	Trials int     // total observations
}

// Mean returns the expected reward: alpha / (alpha + beta).
func (b BetaPosterior) Mean() float64 {
	return b.Alpha / (b.Alpha + b.Beta)
}

// Sample draws from the Beta distribution using the Joehnk method
// which is simple, correct, and avoids external dependencies.
func (b BetaPosterior) Sample(rng *rand.Rand) float64 {
	return betaSample(rng, b.Alpha, b.Beta)
}

// betaSample generates a Beta(α,β) variate.
// For α,β >= 1, uses Gamma decomposition: X/(X+Y) where X~Gamma(α), Y~Gamma(β).
func betaSample(rng *rand.Rand, alpha, beta float64) float64 {
	x := gammaSample(rng, alpha)
	y := gammaSample(rng, beta)
	if x+y == 0 {
		return 0.5
	}
	return x / (x + y)
}

// gammaSample generates a Gamma(shape, 1) variate using Marsaglia & Tsang's method.
func gammaSample(rng *rand.Rand, shape float64) float64 {
	if shape < 1 {
		// Boost: Gamma(shape) = Gamma(shape+1) * U^(1/shape)
		return gammaSample(rng, shape+1) * math.Pow(rng.Float64(), 1.0/shape)
	}
	d := shape - 1.0/3.0
	c := 1.0 / math.Sqrt(9.0*d)
	for {
		var x float64
		var v float64
		for {
			x = rng.NormFloat64()
			v = 1.0 + c*x
			if v > 0 {
				break
			}
		}
		v = v * v * v
		u := rng.Float64()
		if u < 1.0-0.0331*(x*x)*(x*x) {
			return d * v
		}
		if math.Log(u) < 0.5*x*x+d*(1.0-v+math.Log(v)) {
			return d * v
		}
	}
}

// ContextualBandit maintains per-context Beta posteriors for each arm
// and uses Thompson Sampling to select the best tier.
type ContextualBandit struct {
	mu         sync.RWMutex
	posteriors map[string][NumArms]BetaPosterior
	cfg        Config
	rng        *rand.Rand
	totalPulls int64
}

// NewContextualBandit creates a bandit with the given configuration.
func NewContextualBandit(cfg Config, seed uint64) *ContextualBandit {
	return &ContextualBandit{
		posteriors: make(map[string][NumArms]BetaPosterior),
		cfg:        cfg,
		rng:        rand.New(rand.NewPCG(seed, seed^0xDEADBEEF)),
	}
}

// initPosteriors returns the initial posteriors for a new context.
func (cb *ContextualBandit) initPosteriors() [NumArms]BetaPosterior {
	var p [NumArms]BetaPosterior
	for i := range p {
		p[i] = BetaPosterior{
			Alpha: cb.cfg.InitialAlpha,
			Beta:  cb.cfg.InitialBeta,
		}
	}
	return p
}

// SelectArm picks the best arm for the given context using Thompson Sampling.
// During warmup, it round-robins across arms to ensure minimum exploration.
func (cb *ContextualBandit) SelectArm(features Features) Arm {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	key := features.ContextKey()
	posts, exists := cb.posteriors[key]
	if !exists {
		posts = cb.initPosteriors()
		cb.posteriors[key] = posts
	}

	// Warmup: round-robin until each arm has minimum trials
	for i := 0; i < NumArms; i++ {
		if posts[i].Trials < cb.cfg.WarmupRounds {
			return Arm(i)
		}
	}

	// Thompson Sampling with cost adjustment
	bestArm := ArmLight
	bestScore := math.Inf(-1)

	for i := 0; i < NumArms; i++ {
		sample := posts[i].Sample(cb.rng)
		costPenalty := cb.cfg.CostWeight * Arm(i).CostPerCall() / 5.0 // normalize by max cost
		score := sample - costPenalty
		if score > bestScore {
			bestScore = score
			bestArm = Arm(i)
		}
	}

	return bestArm
}

// Update records the outcome of pulling an arm in a given context.
// reward should be 1.0 for success, 0.0 for failure, or a value in [0,1]
// for partial success.
func (cb *ContextualBandit) Update(features Features, arm Arm, reward float64) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	key := features.ContextKey()
	posts, exists := cb.posteriors[key]
	if !exists {
		posts = cb.initPosteriors()
	}

	// Apply temporal decay to existing observations
	if cb.cfg.DecayFactor < 1.0 {
		for i := range posts {
			posts[i].Alpha = cb.cfg.InitialAlpha + (posts[i].Alpha-cb.cfg.InitialAlpha)*cb.cfg.DecayFactor
			posts[i].Beta = cb.cfg.InitialBeta + (posts[i].Beta-cb.cfg.InitialBeta)*cb.cfg.DecayFactor
		}
	}

	posts[arm].Alpha += reward
	posts[arm].Beta += (1.0 - reward)
	posts[arm].Trials++
	cb.posteriors[key] = posts

	cb.totalPulls++
}

// Stats returns the current posterior state for a given context.
func (cb *ContextualBandit) Stats(features Features) [NumArms]BetaPosterior {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	key := features.ContextKey()
	if posts, exists := cb.posteriors[key]; exists {
		return posts
	}
	return cb.initPosteriors()
}

// TotalPulls returns the total number of arm pulls across all contexts.
func (cb *ContextualBandit) TotalPulls() int64 {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.totalPulls
}

// ContextCount returns the number of distinct context keys tracked.
func (cb *ContextualBandit) ContextCount() int {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return len(cb.posteriors)
}

// BestArm returns the arm with the highest expected reward for a context,
// without sampling (greedy). Useful for reporting and convergence checks.
func (cb *ContextualBandit) BestArm(features Features) Arm {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	key := features.ContextKey()
	posts, exists := cb.posteriors[key]
	if !exists {
		return ArmLight
	}

	best := ArmLight
	bestMean := 0.0
	for i := 0; i < NumArms; i++ {
		costPenalty := cb.cfg.CostWeight * Arm(i).CostPerCall() / 5.0
		adjusted := posts[i].Mean() - costPenalty
		if adjusted > bestMean || i == 0 {
			bestMean = adjusted
			best = Arm(i)
		}
	}
	return best
}

// Reset clears all learned posteriors, returning to priors.
func (cb *ContextualBandit) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.posteriors = make(map[string][NumArms]BetaPosterior)
	cb.totalPulls = 0
}
