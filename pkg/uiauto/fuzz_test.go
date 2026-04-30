package uiauto

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nfsarch33/uiauto-framework/pkg/llm"
)

func FuzzPatternTrackerStore(f *testing.F) {
	f.Add(`{"patterns": {"login": {"element_type": "button"}}}`)
	f.Add("invalid json")
	f.Add("")
	f.Fuzz(func(t *testing.T, jsonStr string) {
		tempDir := t.TempDir()
		path := filepath.Join(tempDir, "patterns.json")
		_ = os.WriteFile(path, []byte(jsonStr), 0644)

		store, _ := NewPatternStore(path)
		if store != nil {
			_, _ = store.Load(context.Background())
		}
	})
}

func FuzzUIPatternJSONUnmarshal(f *testing.F) {
	f.Add([]byte(`{"id":"btn","selector":"#submit","fingerprint":{"tag_counts":{}}}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"selector":"div.class[attr]"}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var p UIPattern
		_ = json.Unmarshal(data, &p)
	})
}

func FuzzPatternStoreLoad(f *testing.F) {
	f.Add(`{"login":{"id":"login","selector":"#btn","fingerprint":{}}}`)
	f.Add(`{"patterns":{}}`)
	f.Add("")
	f.Fuzz(func(t *testing.T, jsonStr string) {
		tempDir := t.TempDir()
		path := filepath.Join(tempDir, "p.json")
		_ = os.WriteFile(path, []byte(jsonStr), 0644)
		store, err := NewPatternStore(path)
		if err == nil && store != nil {
			_, _ = store.Load(context.Background())
		}
	})
}

func FuzzWaitConfig(f *testing.F) {
	f.Add(int64(0), int64(1), int64(2))
	f.Add(int64(15e9), int64(500e6), int64(100e6))
	f.Fuzz(func(t *testing.T, timeoutNs, stableNs, pollNs int64) {
		cfg := WaitConfig{
			Timeout:      durationFromNs(timeoutNs),
			StableFor:    durationFromNs(stableNs),
			PollInterval: durationFromNs(pollNs),
		}
		_ = NewPageWaiterFromConfig(cfg)
	})
}

func durationFromNs(ns int64) time.Duration {
	if ns < 0 {
		ns = 0
	}
	if ns > 1e12 {
		ns = 1e12
	}
	return time.Duration(ns)
}

func FuzzModelRouterTaskClassification(f *testing.F) {
	f.Add("discover elements on page")
	f.Add("pattern replay cached selector")
	f.Add("screenshot visual image")
	f.Add("evaluate score rubric")
	f.Add("general task")
	f.Add("")
	f.Fuzz(func(t *testing.T, systemContent string) {
		req := llm.CompletionRequest{
			Messages: []llm.Message{{Role: "system", Content: systemContent}},
		}
		_ = llm.ClassifyTask(req)
	})
}

func FuzzPageWaiterFromConfig(f *testing.F) {
	f.Add(int64(5e9), int64(500e6), int64(100e6))
	f.Add(int64(0), int64(0), int64(0))
	f.Add(int64(30e9), int64(2e9), int64(500e6))
	f.Fuzz(func(t *testing.T, timeoutNs, stableNs, pollNs int64) {
		cfg := WaitConfig{
			Timeout:      durationFromNs(timeoutNs),
			StableFor:    durationFromNs(stableNs),
			PollInterval: durationFromNs(pollNs),
		}
		pw := NewPageWaiterFromConfig(cfg)
		if pw == nil {
			t.Fatal("expected non-nil PageWaiter")
		}
	})
}

func FuzzUIPatternMarshalRoundtrip(f *testing.F) {
	f.Add("btn-login", "#submit", "login button")
	f.Add("", "", "")
	f.Add("nav-menu", "nav.main > ul", "navigation menu")
	f.Fuzz(func(t *testing.T, id, selector, description string) {
		p := UIPattern{
			ID:          id,
			Selector:    selector,
			Description: description,
		}
		data, err := json.Marshal(p)
		if err != nil {
			return
		}
		var decoded UIPattern
		_ = json.Unmarshal(data, &decoded)
	})
}

func FuzzPlaywrightComparisonReportJSON(f *testing.F) {
	f.Add([]byte(`{"description":"test","total_pages":1,"strategies_tested":["WaitNetworkIdle"]}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`invalid`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var r PlaywrightComparisonReport
		_ = json.Unmarshal(data, &r)
	})
}

func FuzzWaitConfigMarshalRoundtrip(f *testing.F) {
	f.Add(int64(5e9), int64(500e6), int64(100e6))
	f.Add(int64(0), int64(0), int64(0))
	f.Fuzz(func(t *testing.T, timeoutNs, stableNs, pollNs int64) {
		cfg := WaitConfig{
			Timeout:      durationFromNs(timeoutNs),
			StableFor:    durationFromNs(stableNs),
			PollInterval: durationFromNs(pollNs),
		}
		data, err := json.Marshal(cfg)
		if err != nil {
			return
		}
		var decoded WaitConfig
		_ = json.Unmarshal(data, &decoded)
	})
}

func FuzzPatternMetadata(f *testing.F) {
	f.Add("login_btn", "#submit", 0.95)
	f.Add("", "", 0.0)
	f.Add("nav", "nav.primary", 1.0)
	f.Fuzz(func(t *testing.T, id, selector string, confidence float64) {
		if confidence < 0 || confidence > 1 {
			return
		}
		p := UIPattern{
			ID:         id,
			Selector:   selector,
			Confidence: confidence,
			Metadata:   map[string]string{"source": "fuzz"},
		}
		data, err := json.Marshal(p)
		if err != nil {
			return
		}
		var decoded UIPattern
		_ = json.Unmarshal(data, &decoded)
	})
}
