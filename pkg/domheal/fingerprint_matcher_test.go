package domheal

import (
	"log/slog"
	"os"
	"testing"
)

func TestParseDOMFingerprint(t *testing.T) {
	html := `
		<html>
			<body>
				<form id="login" role="form">
					<input type="text" data-testid="user" />
					<input type="password" data-test-id="pass" />
					<button type="submit">Log In</button>
				</form>
				<a href="/help">Help</a>
			</body>
		</html>
	`

	fp := ParseDOMFingerprint(html)

	if fp.FormElements != 3 { // 2 inputs, 1 button
		t.Errorf("expected 3 form elements, got %d", fp.FormElements)
	}

	if fp.DataAttrs["testid"] != 1 || fp.DataAttrs["test-id"] != 1 {
		t.Errorf("unexpected data attrs: %v", fp.DataAttrs)
	}

	if fp.RoleMap["form"] != 1 {
		t.Errorf("expected role form=1, got %v", fp.RoleMap)
	}

	if fp.IDFingerprint != "login" {
		t.Errorf("expected id fingerprint 'login', got %s", fp.IDFingerprint)
	}

	if fp.TotalNodes == 0 {
		t.Error("expected non-zero total nodes")
	}
}

func TestExtractTextPatterns(t *testing.T) {
	html := `<div>This is a very long sentence that should be extracted as a text pattern because it has enough words and length.</div>`
	patterns := ExtractTextPatterns(html)

	if len(patterns) == 0 {
		t.Error("expected text patterns to be extracted")
	}

	// Short text should not be extracted
	shortHtml := `<div>Short text</div>`
	if len(ExtractTextPatterns(shortHtml)) != 0 {
		t.Error("expected no patterns for short text")
	}
}

func TestFingerprintMatcher(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	matcher := NewFingerprintMatcher(0.6, logger)

	if matcher.Threshold() != 0.6 {
		t.Errorf("expected threshold 0.6, got %f", matcher.Threshold())
	}

	html1 := `
		<div id="main" data-test="container">
			<p>This is some unique content that we can use for testing.</p>
			<a href="#">Link</a>
		</div>
	`

	html2 := `
		<div id="main" data-test="container" class="new-class">
			<p>This is some unique content that we can use for testing.</p>
			<span><a href="#">Link</a></span>
		</div>
	`

	html3 := `
		<div>
			<p>Completely different text here.</p>
		</div>
	`

	res1 := matcher.CheckAndUpdate("page1", html1)
	if res1.Drifted {
		t.Error("expected no drift for first check")
	}

	if matcher.KnownCount() != 1 {
		t.Errorf("expected 1 known page, got %d", matcher.KnownCount())
	}

	res2 := matcher.CheckAndUpdate("page2", html2)
	if res2.Drifted {
		t.Errorf("expected no drift for minor structural changes, sim=%f", res2.Similarity)
	}

	res3 := matcher.CheckAndUpdate("page3", html3)
	if res3.Drifted {
		t.Error("expected no drift for first check")
	}

	// Test drift detection logging
	res4 := matcher.CheckAndUpdate("page1", html3)
	if !res4.Drifted {
		t.Error("expected drift for completely different content")
	}

	match := matcher.FindClosestMatch(html1)
	if match.PageID != "page1" && match.PageID != "page2" {
		t.Errorf("expected page1 or page2, got %s", match.PageID)
	}

	// Test worse match
	match2 := matcher.FindClosestMatch(html2)
	if match2.PageID != "page2" && match2.PageID != "page1" {
		t.Errorf("expected page2 or page1, got %s", match2.PageID)
	}

	// Test no match found
	noMatch := matcher.FindClosestMatch("<html><body>Nothing here</body></html>")
	if noMatch.Found {
		t.Error("expected no match found")
	}

	// Empty HTML
	emptyFP := ParseDOMFingerprint("")
	if emptyFP.TotalNodes != 0 {
		t.Error("expected 0 nodes for empty HTML")
	}

	// Negative threshold
	badMatcher := NewFingerprintMatcher(-1.0, nil)
	if badMatcher.Threshold() != 0.7 {
		t.Errorf("expected default threshold 0.7, got %f", badMatcher.Threshold())
	}
}

func TestStringSimilarity(t *testing.T) {
	sim := StringSimilarity("hello world", "hello world")
	if sim != 1.0 {
		t.Errorf("expected 1.0, got %f", sim)
	}

	sim = StringSimilarity("hello", "world")
	if sim > 0.5 {
		t.Errorf("expected low similarity, got %f", sim)
	}

	sim = StringSimilarity("a,b", "b,c")
	if sim != 0.5 && sim != 0.3333333333333333 {
		t.Errorf("expected partial similarity, got %f", sim)
	}

	if StringSimilarity("", "") != 1.0 {
		t.Error("expected 1.0 for empty strings")
	}

	if StringSimilarity("a", "") != 0.0 {
		t.Error("expected 0.0 for one empty string")
	}

	if StringSimilarity("", "b") != 0.0 {
		t.Error("expected 0.0 for one empty string")
	}
}

func TestMapSimilarity(t *testing.T) {
	m1 := map[string]int{"a": 1, "b": 2}
	m2 := map[string]int{"a": 1, "c": 1}
	sim := MapSimilarity(m1, m2)
	if sim == 0 {
		t.Error("expected non-zero similarity")
	}

	m3 := map[string]int{"x": 1}
	if MapSimilarity(m1, m3) != 0 {
		t.Error("expected zero similarity")
	}

	if MapSimilarity(nil, nil) != 1.0 {
		t.Error("expected 1.0 for empty maps")
	}

	if MapSimilarity(map[string]int{"a": 1}, nil) != 0.0 {
		t.Error("expected 0.0 for one empty map")
	}

	if MapSimilarity(nil, map[string]int{"b": 1}) != 0.0 {
		t.Error("expected 0.0 for one empty map")
	}
}

func TestTextPatternOverlap(t *testing.T) {
	p1 := []string{"hello world", "test pattern"}
	p2 := []string{"hello world", "other pattern"}
	sim := TextPatternOverlap(p1, p2)
	if sim == 0 {
		t.Error("expected non-zero overlap")
	}

	p3 := []string{"no match"}
	if TextPatternOverlap(p1, p3) != 0 {
		t.Error("expected zero overlap")
	}

	if TextPatternOverlap(nil, nil) != 1.0 {
		t.Error("expected 1.0 for empty patterns")
	}

	if TextPatternOverlap([]string{"a"}, nil) != 0.0 {
		t.Error("expected 0.0 for one empty pattern")
	}

	if TextPatternOverlap(nil, []string{"b"}) != 0.0 {
		t.Error("expected 0.0 for one empty pattern")
	}
}
