package evolver

import (
	"encoding/json"
	"os"
	"testing"
)

func FuzzGeneUnmarshalValidate(f *testing.F) {
	f.Add(`{"id":"g1","name":"test","description":"desc","category":"prompt","payload":"{}"}`)
	f.Add(`{}`)
	f.Add(`{"id":"","name":""}`)
	f.Add(`invalid json`)
	f.Add(`{"id":"g2","name":"n","description":"d","category":"tool","blast_radius":"low","validation":[{"command":"echo ok"}]}`)
	f.Fuzz(func(t *testing.T, data string) {
		var g Gene
		if err := json.Unmarshal([]byte(data), &g); err != nil {
			return
		}
		_ = g.Validate()
	})
}

func FuzzCapsuleUnmarshalValidate(f *testing.F) {
	f.Add(`{"id":"c1","name":"cap","description":"test capsule","gene_ids":["g1"],"status":"active"}`)
	f.Add(`{}`)
	f.Add(`{"id":"","gene_ids":[]}`)
	f.Add(`invalid`)
	f.Fuzz(func(t *testing.T, data string) {
		var c Capsule
		if err := json.Unmarshal([]byte(data), &c); err != nil {
			return
		}
		_ = c.Validate()
	})
}

func FuzzMutationUnmarshalValidate(f *testing.F) {
	f.Add(`{"id":"m1","signal_id":"s1","reasoning":"fix bug","risk_estimate":"low","strategy":"repair"}`)
	f.Add(`{}`)
	f.Add(`{"id":"","reasoning":""}`)
	f.Fuzz(func(t *testing.T, data string) {
		var m Mutation
		if err := json.Unmarshal([]byte(data), &m); err != nil {
			return
		}
		_ = m.Validate()
	})
}

func FuzzEvolutionEventUnmarshalValidate(f *testing.F) {
	f.Add(`{"id":"e1","type":"create_capsule","actor_id":"agent-1","outcome":"success"}`)
	f.Add(`{}`)
	f.Add(`{"id":"","type":"unknown"}`)
	f.Fuzz(func(t *testing.T, data string) {
		var e EvolutionEvent
		if err := json.Unmarshal([]byte(data), &e); err != nil {
			return
		}
		_ = e.Validate()
	})
}

func FuzzLoadTracesJSONL(f *testing.F) {
	f.Add(`{"task_name":"t1","start_time":"2024-01-01T00:00:00Z","end_time":"2024-01-01T00:01:00Z"}`)
	f.Add(`{"task_name":"t1"}` + "\n" + `{"task_name":"t2"}`)
	f.Add(`{}`)
	f.Add(`invalid json line`)
	f.Add("")
	f.Fuzz(func(t *testing.T, data string) {
		dir := t.TempDir()
		path := dir + "/traces.jsonl"
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Skip("cannot write temp file")
		}
		_, _ = LoadTraces(path)
	})
}

func FuzzSignalUnmarshal(f *testing.F) {
	f.Add(`{"id":"s1","type":"repeated_failure","severity":"warning","description":"latency spike"}`)
	f.Add(`{}`)
	f.Add(`{"id":"","type":""}`)
	f.Add(`invalid`)
	f.Fuzz(func(t *testing.T, data string) {
		var s Signal
		_ = json.Unmarshal([]byte(data), &s)
	})
}

func FuzzEvaluationResultUnmarshal(f *testing.F) {
	f.Add(`{"overall_score":0.85,"rubric_scores":[],"passed":true}`)
	f.Add(`{}`)
	f.Add(`{"overall_score":-1}`)
	f.Fuzz(func(t *testing.T, data string) {
		var er EvaluationResult
		_ = json.Unmarshal([]byte(data), &er)
	})
}

func FuzzPromotionRecordUnmarshal(f *testing.F) {
	f.Add(`{"capsule_id":"c1","status":"approved","reviewer_id":"human"}`)
	f.Add(`{}`)
	f.Add(`{"status":"rejected","reason":"too risky"}`)
	f.Fuzz(func(t *testing.T, data string) {
		var pr PromotionRecord
		_ = json.Unmarshal([]byte(data), &pr)
	})
}

func FuzzSignalMinerConfig(f *testing.F) {
	f.Add(3, 5000.0, 2.0, 0.5, 100)
	f.Add(0, 0.0, 0.0, 0.0, 0)
	f.Add(-1, -1.0, -1.0, -1.0, -1)
	f.Fuzz(func(t *testing.T, failThreshold int, latencyMs, costMult, successRate float64, windowSize int) {
		cfg := SignalMinerConfig{
			RepeatedFailureThreshold: failThreshold,
			HighLatencyThresholdMs:   latencyMs,
			CostSpikeMultiplier:      costMult,
			LowSuccessRateThreshold:  successRate,
			WindowSize:               windowSize,
		}
		_ = cfg
	})
}

func FuzzOptimizationConfigUnmarshal(f *testing.F) {
	f.Add(`{"strategy":"textgrad","max_iterations":10}`)
	f.Add(`{}`)
	f.Add(`{"strategy":"","max_iterations":-1}`)
	f.Fuzz(func(t *testing.T, data string) {
		var c OptimizationConfig
		_ = json.Unmarshal([]byte(data), &c)
	})
}
