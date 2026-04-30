package uiauto

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

var validSeverities = []DriftSeverity{DriftSeverityLow, DriftSeverityMedium, DriftSeverityHigh, DriftSeverityCritical}

func pickSeverity(i int64) DriftSeverity {
	idx := i % int64(len(validSeverities))
	if idx < 0 {
		idx = -idx
	}
	return validSeverities[idx]
}

func FuzzDriftAlertJSONRoundtrip(f *testing.F) {
	f.Add("login_page", "submit_btn", int64(2), "#old", ".new", 0.45, false)
	f.Add("", "", int64(0), "", "", 0.0, true)
	f.Add("dashboard", "nav", int64(3), "nav#main", "aside.sidebar", 0.99, false)
	f.Fuzz(func(t *testing.T, pageID, patternID string, sevIdx int64, oldSel, newSel string, sim float64, resolved bool) {
		if sim < 0 || sim > 1 {
			return
		}
		alert := DriftAlert{
			PageID:      pageID,
			PatternID:   patternID,
			Severity:    pickSeverity(sevIdx),
			OldSelector: oldSel,
			NewSelector: newSel,
			Similarity:  sim,
			Resolved:    resolved,
			CreatedAt:   time.Now(),
		}
		data, err := json.Marshal(alert)
		if err != nil {
			return
		}
		var decoded DriftAlert
		_ = json.Unmarshal(data, &decoded)
	})
}

func FuzzModelHandoffJSONRoundtrip(f *testing.F) {
	f.Add("btn_a", "light", "smart", "cache_miss", true)
	f.Add("", "", "", "", false)
	f.Add("form_x", "smart", "vlm", "llm_fail", false)
	f.Fuzz(func(t *testing.T, patternID, from, to, reason string, success bool) {
		h := ModelHandoff{
			PatternID: patternID,
			FromTier:  from,
			ToTier:    to,
			Reason:    reason,
			Success:   success,
			CreatedAt: time.Now(),
		}
		data, err := json.Marshal(h)
		if err != nil {
			return
		}
		var decoded ModelHandoff
		_ = json.Unmarshal(data, &decoded)
	})
}

func FuzzHealResultJSONRoundtrip(f *testing.F) {
	f.Add("target_1", "#old", "#new", "fingerprint", 0.85, true)
	f.Add("", "", "", "", 0.0, false)
	f.Fuzz(func(t *testing.T, targetID, oldSel, newSel, method string, confidence float64, success bool) {
		if confidence < 0 || confidence > 1 {
			return
		}
		r := HealResult{
			TargetID:    targetID,
			OldSelector: oldSel,
			NewSelector: newSel,
			Method:      method,
			Confidence:  confidence,
			Duration:    time.Millisecond * 100,
			Success:     success,
		}
		data, err := json.Marshal(r)
		if err != nil {
			return
		}
		var decoded HealResult
		_ = json.Unmarshal(data, &decoded)
	})
}

func FuzzActionJSONRoundtrip(f *testing.F) {
	f.Add("click", "submit_btn", "Click submit", "")
	f.Add("type", "email_input", "Type email", "user@test.com")
	f.Add("wait", "", "Wait for page", "#content")
	f.Add("", "", "", "")
	f.Fuzz(func(t *testing.T, actionType, targetID, desc, value string) {
		a := Action{
			Type:        actionType,
			TargetID:    targetID,
			Description: desc,
			Value:       value,
		}
		data, err := json.Marshal(a)
		if err != nil {
			return
		}
		var decoded Action
		_ = json.Unmarshal(data, &decoded)
	})
}

func FuzzClassifyDriftSeverity(f *testing.F) {
	f.Add(0.0)
	f.Add(0.15)
	f.Add(0.3)
	f.Add(0.5)
	f.Add(0.7)
	f.Add(0.9)
	f.Add(1.0)
	f.Fuzz(func(t *testing.T, similarity float64) {
		sev := ClassifyDriftSeverity(similarity)
		switch sev {
		case DriftSeverityLow, DriftSeverityMedium, DriftSeverityHigh, DriftSeverityCritical:
		default:
			t.Errorf("invalid severity %q for similarity %.4f", sev, similarity)
		}
	})
}

func FuzzCircuitBreakerConfig(f *testing.F) {
	f.Add(int64(5), int64(3), int64(30e9), int64(1), int64(10))
	f.Add(int64(0), int64(0), int64(0), int64(0), int64(0))
	f.Add(int64(100), int64(50), int64(60e9), int64(10), int64(50))
	f.Fuzz(func(t *testing.T, failThresh, successThresh, openNs, halfOpen, window int64) {
		if failThresh < 0 || successThresh < 0 || openNs < 0 || halfOpen < 0 || window < 0 {
			return
		}
		cfg := CircuitBreakerConfig{
			FailureThreshold: int(failThresh % 1000),
			SuccessThreshold: int(successThresh % 1000),
			OpenDuration:     time.Duration(openNs % int64(time.Hour)),
			HalfOpenMax:      int(halfOpen % 100),
			WindowSize:       int(window % 1000),
		}
		cb := NewCircuitBreaker("fuzz_test", cfg)
		if cb == nil {
			t.Fatal("NewCircuitBreaker returned nil")
		}
	})
}

func FuzzAggregatedMetricsJSON(f *testing.F) {
	f.Add([]byte(`{"heal_attempts":10,"heal_successes":8,"tier":"smart"}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`invalid json`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var m AggregatedMetrics
		_ = json.Unmarshal(data, &m)
	})
}

func FuzzTaskResultJSONRoundtrip(f *testing.F) {
	f.Add("task_1", int64(1), 3, true)
	f.Add("", int64(0), 0, false)
	f.Fuzz(func(t *testing.T, taskID string, status int64, patternCount int, converged bool) {
		r := TaskResult{
			TaskID:       taskID,
			Status:       TaskStatus(status % 3),
			Duration:     time.Millisecond * 250,
			PatternCount: patternCount,
			Converged:    converged,
		}
		data, err := json.Marshal(r)
		if err != nil {
			return
		}
		var decoded TaskResult
		_ = json.Unmarshal(data, &decoded)
	})
}

func FuzzHealerMetricsSnapshot(f *testing.F) {
	f.Add(int64(100), int64(90), int64(50), int64(30), int64(5), int64(5), int64(10))
	f.Add(int64(0), int64(0), int64(0), int64(0), int64(0), int64(0), int64(0))
	f.Fuzz(func(t *testing.T, total, success, fingerprint, structural, smart, vlm, failed int64) {
		if total < 0 || success < 0 || fingerprint < 0 || structural < 0 || smart < 0 || vlm < 0 || failed < 0 {
			return
		}
		if success > total {
			return
		}
		m := &HealerMetrics{
			TotalAttempts:    total,
			SuccessfulHeals:  success,
			FingerprintHeals: fingerprint,
			StructuralHeals:  structural,
			SmartLLMHeals:    smart,
			VLMHeals:         vlm,
			FailedHeals:      failed,
		}
		snap := m.Snapshot()
		if snap.TotalAttempts != total {
			t.Errorf("snapshot mismatch: got %d, want %d", snap.TotalAttempts, total)
		}
		rate := m.SuccessRate()
		if total > 0 && (rate < 0 || rate > 1) {
			t.Errorf("success rate out of range: %.4f (total=%d, success=%d)", rate, total, success)
		}
	})
}

func FuzzModelTierString(f *testing.F) {
	f.Add(int64(0))
	f.Add(int64(1))
	f.Add(int64(2))
	f.Add(int64(99))
	f.Fuzz(func(t *testing.T, tier int64) {
		mt := ModelTier(tier)
		s := mt.String()
		if s == "" {
			t.Error("ModelTier.String() should never be empty")
		}
	})
}

func FuzzPatternTrackerRegisterAndGet(f *testing.F) {
	f.Add("btn_1", "#submit", "submit button", `<html><body><button id="submit">Go</button></body></html>`)
	f.Add("", "", "", "")
	f.Add("nav", "nav.main", "navigation", `<nav class="main"><a href="/">Home</a></nav>`)
	f.Fuzz(func(t *testing.T, id, selector, desc, html string) {
		if id == "" {
			return
		}
		dir := t.TempDir()
		tracker, err := NewPatternTracker(dir+"/p.json", dir)
		if err != nil {
			return
		}
		ctx := context.Background()
		_ = tracker.RegisterPattern(ctx, id, selector, desc, html)
		_, _ = tracker.store.Get(ctx, id)
	})
}

func FuzzDriftSeverityBoundaries(f *testing.F) {
	f.Add(0.0)
	f.Add(0.15)
	f.Add(0.199)
	f.Add(0.2)
	f.Add(0.4)
	f.Add(0.5)
	f.Add(0.7)
	f.Add(0.8)
	f.Add(0.9)
	f.Add(1.0)
	f.Fuzz(func(t *testing.T, sim float64) {
		if sim < 0 || sim > 1 {
			return
		}
		sev := ClassifyDriftSeverity(sim)
		switch {
		case sim >= 0.8:
			if sev != DriftSeverityLow {
				t.Errorf("sim=%.4f expected Low, got %q", sim, sev)
			}
		case sim >= 0.5:
			if sev != DriftSeverityMedium {
				t.Errorf("sim=%.4f expected Medium, got %q", sim, sev)
			}
		case sim >= 0.2:
			if sev != DriftSeverityHigh {
				t.Errorf("sim=%.4f expected High, got %q", sim, sev)
			}
		default:
			if sev != DriftSeverityCritical {
				t.Errorf("sim=%.4f expected Critical, got %q", sim, sev)
			}
		}
	})
}

func FuzzInMemoryDriftAlertStore(f *testing.F) {
	f.Add("page_1", "btn_1", int64(1), 0.5)
	f.Add("", "", int64(0), 0.0)
	f.Fuzz(func(t *testing.T, pageID, patternID string, sevIdx int64, sim float64) {
		if sim < 0 || sim > 1 {
			return
		}
		store := NewInMemoryDriftAlertStore(nil)
		alert := DriftAlert{
			PageID:     pageID,
			PatternID:  patternID,
			Severity:   pickSeverity(sevIdx),
			Similarity: sim,
		}
		store.Insert(alert)
		_ = store.Unresolved()
	})
}

func FuzzInMemoryHandoffStore(f *testing.F) {
	f.Add("pattern_a", "light", "smart", "miss", true)
	f.Add("", "", "", "", false)
	f.Fuzz(func(t *testing.T, patternID, from, to, reason string, success bool) {
		store := NewInMemoryHandoffStore()
		h := ModelHandoff{
			PatternID: patternID,
			FromTier:  from,
			ToTier:    to,
			Reason:    reason,
			Success:   success,
		}
		store.Insert(h)
		_ = store.Recent(10)
	})
}

func FuzzHealStrategyBitflags(f *testing.F) {
	f.Add(int64(1))
	f.Add(int64(3))
	f.Add(int64(7))
	f.Add(int64(15))
	f.Add(int64(31))
	f.Add(int64(0))
	f.Fuzz(func(t *testing.T, flags int64) {
		s := HealStrategy(flags)
		_ = s&HealFingerprint != 0
		_ = s&HealStructural != 0
		_ = s&HealSmartLLM != 0
		_ = s&HealVLM != 0
		_ = s&HealVLMJudge != 0
	})
}

func FuzzMemberAgentConfigJSON(f *testing.F) {
	f.Add([]byte(`{"headless":true,"pattern_file":"p.json"}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`invalid`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var cfg MemberAgentConfig
		_ = json.Unmarshal(data, &cfg)
	})
}
