package domheal

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func FuzzParseDOMFingerprint(f *testing.F) {
	f.Add("<html><body><div class='x'>text</div></body></html>")
	f.Add("<form><input name='q'/></form>")
	f.Add("")
	f.Add("just text")
	f.Fuzz(func(t *testing.T, html string) {
		fp := ParseDOMFingerprint(html)
		if fp.TagCounts == nil {
			t.Error("nil TagCounts")
		}
		if fp.DataAttrs == nil {
			t.Error("nil DataAttrs")
		}
		if fp.RoleMap == nil {
			t.Error("nil RoleMap")
		}
	})
}

func FuzzParseStructuralSignature(f *testing.F) {
	f.Add("<html><body><div class='x'>text</div></body></html>")
	f.Add("<form><input name='q'/></form>")
	f.Fuzz(func(t *testing.T, html string) {
		sig := ParseStructuralSignature(html)
		if sig.TagCounts == nil {
			t.Error("nil TagCounts")
		}
	})
}

func FuzzStringSimilarity(f *testing.F) {
	f.Add("a,b,c", "b,c,d")
	f.Add("", "")
	f.Add("hello", "world")
	f.Fuzz(func(t *testing.T, a, b string) {
		sim := StringSimilarity(a, b)
		if sim < 0.0 || sim > 1.0 {
			t.Errorf("similarity out of bounds: %f", sim)
		}
	})
}

func FuzzExtractTextPatterns(f *testing.F) {
	f.Add("<html><body><div class='x'>This is a very long sentence that should be extracted as a text pattern because it has enough words and length.</div></body></html>")
	f.Add("short")
	f.Fuzz(func(t *testing.T, html string) {
		patterns := ExtractTextPatterns(html)
		_ = patterns
	})
}

func FuzzSelectorRepairV2Repair(f *testing.F) {
	f.Add(".btn-primary", "button")
	f.Add("#main-content", "content")
	f.Add("div.class[data-id=x]", "elem")
	f.Add("", "")
	f.Fuzz(func(t *testing.T, brokenSelector, elementType string) {
		tempDir := t.TempDir()
		rl := NewRepairLog(filepath.Join(tempDir, "log.json"))
		sr := NewSelectorRepairV2(rl, slog.New(slog.NewTextHandler(io.Discard, nil)))
		eval := func(ctx context.Context, sel string) (int, error) { return 0, nil }
		_ = sr.Repair(context.Background(), elementType, brokenSelector, eval)
	})
}

func FuzzRepairSuggestionJSONUnmarshal(f *testing.F) {
	f.Add([]byte(`{"element_type":"btn","old_selector":"#x","new_selector":".y","confidence":0.9,"method":"css"}`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var s RepairSuggestion
		_ = json.Unmarshal(data, &s)
	})
}

func FuzzRepairLogRead(f *testing.F) {
	f.Add(`[{"element_type":"btn","old_selector":"#x","new_selector":".y","confidence":0.9,"method":"css"}]`)
	f.Add(`[]`)
	f.Add("")
	f.Fuzz(func(t *testing.T, jsonStr string) {
		tempDir := t.TempDir()
		path := filepath.Join(tempDir, "repair.json")
		_ = os.WriteFile(path, []byte(jsonStr), 0644)
		rl := NewRepairLog(path)
		_, _ = rl.Read()
	})
}

func FuzzDOMFingerprintSimilarity(f *testing.F) {
	f.Add("<div class='a'>x</div>", "<div class='b'>y</div>")
	f.Add("", "")
	f.Add("<form><input/></form>", "<form><input/></form>")
	f.Fuzz(func(t *testing.T, htmlA, htmlB string) {
		a := ParseDOMFingerprint(htmlA)
		b := ParseDOMFingerprint(htmlB)
		sim := DOMFingerprintSimilarity(a, b)
		if sim < 0.0 || sim > 1.0 {
			t.Errorf("similarity out of bounds: %f", sim)
		}
	})
}

func FuzzStructuralSimilarity(f *testing.F) {
	f.Add("<div><span></span></div>", "<div><p></p></div>")
	f.Add("", "")
	f.Fuzz(func(t *testing.T, htmlA, htmlB string) {
		a := ParseStructuralSignature(htmlA)
		b := ParseStructuralSignature(htmlB)
		sim := StructuralSimilarity(a, b)
		if sim < 0.0 || sim > 1.0 {
			t.Errorf("similarity out of bounds: %f", sim)
		}
	})
}

func FuzzFingerprintMatcherCheckAndUpdate(f *testing.F) {
	f.Add("page1", "<html><body><div>content</div></body></html>")
	f.Add("", "")
	f.Fuzz(func(t *testing.T, pageID, html string) {
		fm := NewFingerprintMatcher(0.7, slog.New(slog.NewTextHandler(io.Discard, nil)))
		_ = fm.CheckAndUpdate(pageID, html)
	})
}

func FuzzDriftDetectorCheckAndUpdate(f *testing.F) {
	f.Add("page1", "<html><body><div>old</div></body></html>")
	f.Add("page2", "<html><body><div>new</div></body></html>")
	f.Fuzz(func(t *testing.T, pageID, html string) {
		tempDir := t.TempDir()
		dd := NewDriftDetector(tempDir)
		_, _ = dd.CheckAndUpdate(pageID, html)
	})
}

func FuzzCircuitBreakerAllow(f *testing.F) {
	f.Add(3, 60)
	f.Add(1, 1)
	f.Add(10, 300)
	f.Fuzz(func(t *testing.T, threshold, cooldownSec int) {
		if threshold < 1 {
			threshold = 1
		}
		if cooldownSec < 1 {
			cooldownSec = 1
		}
		cb := NewCircuitBreaker(threshold, cooldownSec)
		_ = cb.Allow()
		cb.RecordSuccess()
		cb.RecordFailure()
		_ = cb.State()
		_ = cb.Failures()
	})
}

func FuzzRepairLogAppend(f *testing.F) {
	f.Add(`{"element_type":"btn","old_selector":"#x","new_selector":".y","confidence":0.9,"method":"css"}`)
	f.Add(`{}`)
	f.Fuzz(func(t *testing.T, jsonStr string) {
		var s RepairSuggestion
		if json.Unmarshal([]byte(jsonStr), &s) != nil {
			return
		}
		tempDir := t.TempDir()
		rl := NewRepairLog(filepath.Join(tempDir, "log.json"))
		_ = rl.Append(s)
	})
}

func FuzzStructuralSignatureJSONUnmarshal(f *testing.F) {
	f.Add([]byte(`{"tag_counts":{"div":5,"span":3},"max_depth":4,"total_nodes":8,"class_fingerprint":"x"}`))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		var sig StructuralSignature
		_ = json.Unmarshal(data, &sig)
	})
}
