package uiauto

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func FuzzModelEscalationPolicyNextTier(f *testing.F) {
	f.Add(0) // TierLight
	f.Add(1) // TierSmart
	f.Add(2) // TierVLM
	f.Add(-1)
	f.Add(99)

	f.Fuzz(func(t *testing.T, tier int) {
		p := DefaultEscalationPolicy()
		mt := ModelTier(tier)
		next, ok := p.NextTier(mt)
		if ok && next == mt {
			t.Error("NextTier should not return same tier")
		}
	})
}

func FuzzModelEscalationPolicyShouldEscalate(f *testing.F) {
	f.Add(0, 3, 0.5, 10)
	f.Add(0, 0, 0.4, 3)
	f.Add(1, 2, 0.0, 0)
	f.Add(2, 100, 0.0, 100)

	f.Fuzz(func(t *testing.T, tier, failures int, rate float64, attempts int) {
		p := DefaultEscalationPolicy()
		// Should never panic regardless of input
		_ = p.ShouldEscalate(ModelTier(tier), failures, rate, attempts)
	})
}

func FuzzPatternMaturityTrackerPromoteDemote(f *testing.F) {
	f.Add(5, 3)  // successes then failures
	f.Add(20, 0) // all successes
	f.Add(0, 5)  // all failures
	f.Add(1, 1)  // alternating

	f.Fuzz(func(t *testing.T, successes, failures int) {
		if successes < 0 {
			successes = 0
		}
		if failures < 0 {
			failures = 0
		}
		if successes > 100 {
			successes = 100
		}
		if failures > 100 {
			failures = 100
		}

		tracker := NewPatternMaturityTracker(DefaultMaturityConfig())
		ctx := context.Background()

		for i := 0; i < successes; i++ {
			tracker.RecordSuccess(ctx, "fuzz-pattern")
		}
		for i := 0; i < failures; i++ {
			tracker.RecordFailure(ctx, "fuzz-pattern")
		}

		pm, ok := tracker.GetMaturity(ctx, "fuzz-pattern")
		if !ok && (successes > 0 || failures > 0) {
			t.Error("pattern should exist after recording")
		}
		if ok {
			if pm.TotalSuccesses != successes {
				t.Errorf("TotalSuccesses = %d, want %d", pm.TotalSuccesses, successes)
			}
			if pm.TotalFailures != failures {
				t.Errorf("TotalFailures = %d, want %d", pm.TotalFailures, failures)
			}
			// Level should always be valid
			_ = pm.Level.String()
			// SuccessRate should be [0, 1]
			rate := pm.SuccessRate()
			if rate < 0 || rate > 1 {
				t.Errorf("SuccessRate = %f, should be [0,1]", rate)
			}
		}
	})
}

func FuzzMaturityLevelString(f *testing.F) {
	f.Add(0)
	f.Add(1)
	f.Add(2)
	f.Add(3)
	f.Add(4)
	f.Add(-1)
	f.Add(99)

	f.Fuzz(func(t *testing.T, level int) {
		ml := MaturityLevel(level)
		s := ml.String()
		if s == "" {
			t.Error("String() should never return empty")
		}
	})
}

func FuzzSanitizeID(f *testing.F) {
	f.Add("button.login")
	f.Add("#main-content")
	f.Add("a[href='test']")
	f.Add("")
	f.Add("<script>alert('xss')</script>")
	f.Add("../../../etc/passwd")
	f.Add("input[name=\"email\"]")

	f.Fuzz(func(t *testing.T, input string) {
		result := sanitizeID(input)
		if len(result) > 64 {
			t.Errorf("sanitizeID output length = %d, should be <= 64", len(result))
		}
		for _, c := range result {
			isAlpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
			isDigit := c >= '0' && c <= '9'
			isAllowed := isAlpha || isDigit || c == '_' || c == '-'
			if !isAllowed {
				t.Errorf("invalid char %q in sanitized output", c)
			}
		}
	})
}

func FuzzScoringConfigJSONRoundtrip(f *testing.F) {
	def := DefaultScoringConfig()
	data, _ := json.Marshal(def)
	f.Add(string(data))
	f.Add(`{"SimilarityWeight":0.5,"ConfidenceWeight":0.5}`)
	f.Add(`{}`)
	f.Add(`{"invalid": true}`)

	f.Fuzz(func(t *testing.T, jsonStr string) {
		var cfg ScoringConfig
		err := json.Unmarshal([]byte(jsonStr), &cfg)
		if err != nil {
			return
		}
		data2, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		var cfg2 ScoringConfig
		if err := json.Unmarshal(data2, &cfg2); err != nil {
			t.Fatalf("re-Unmarshal failed: %v", err)
		}
	})
}

func FuzzDiscoveryConfigFields(f *testing.F) {
	f.Add(50, 0.5, int64(30*time.Second))
	f.Add(0, 0.0, int64(0))
	f.Add(-1, -1.0, int64(-1))

	f.Fuzz(func(t *testing.T, maxElements int, minConfidence float64, timeoutNs int64) {
		cfg := DiscoveryConfig{
			MaxElements:   maxElements,
			MinConfidence: minConfidence,
			ScanTimeout:   time.Duration(timeoutNs),
			ElementTypes:  []string{"button"},
		}
		// Should never panic when creating
		dm := NewDiscoveryMode(nil, nil, nil, cfg, nil)
		if dm == nil {
			t.Fatal("NewDiscoveryMode returned nil")
		}
	})
}

func FuzzEscalationConditionCombinations(f *testing.F) {
	f.Add(3, 0.5, 5, 2, 0.0, 0)
	f.Add(1, 0.0, 0, 0, 0.0, 0)

	f.Fuzz(func(t *testing.T, cf int, srb float64, minAtt int, cf2 int, srb2 float64, minAtt2 int) {
		policy := ModelEscalationPolicy{
			TierOrder: []ModelTier{TierLight, TierSmart, TierVLM},
			EscalateConditions: map[ModelTier]EscalationCondition{
				TierLight: {ConsecutiveFailures: cf, SuccessRateBelow: srb, MinAttempts: minAtt},
			},
			DemoteConditions: map[ModelTier]EscalationCondition{
				TierSmart: {ConsecutiveFailures: cf2, SuccessRateBelow: srb2, MinAttempts: minAtt2},
			},
		}
		// Should never panic
		_ = policy.ShouldEscalate(TierLight, 5, 0.3, 10)
		_ = policy.ShouldDemote(TierSmart, 5, 0.9)
	})
}
