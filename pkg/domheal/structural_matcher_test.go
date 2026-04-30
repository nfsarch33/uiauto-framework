package domheal

import (
	"log/slog"
	"os"
	"testing"
)

func TestParseStructuralSignature(t *testing.T) {
	html := `
		<html>
			<body class="main-body">
				<div class="container wrapper">
					<p id="p1">Hello</p>
					<p class="text">World</p>
				</div>
				</div>
			</body>
		</html>
	`

	sig := ParseStructuralSignature(html)

	if sig.TotalNodes != 5 {
		t.Errorf("expected 5 nodes, got %d", sig.TotalNodes)
	}

	if sig.TagCounts["html"] != 1 || sig.TagCounts["body"] != 1 || sig.TagCounts["div"] != 1 || sig.TagCounts["p"] != 2 {
		t.Errorf("unexpected tag counts: %v", sig.TagCounts)
	}

	if sig.MaxDepth != 5 {
		t.Errorf("expected max depth 5, got %d", sig.MaxDepth)
	}
}

func TestStructuralSimilarity(t *testing.T) {
	sig1 := StructuralSignature{
		TagCounts:  map[string]int{"div": 2, "p": 3, "a": 1},
		TotalNodes: 6,
	}
	sig2 := StructuralSignature{
		TagCounts:  map[string]int{"div": 2, "p": 2, "span": 1},
		TotalNodes: 5,
	}

	// Intersection: min(div)=2, min(p)=2, min(a)=0, min(span)=0 => 4
	// Union: max(div)=2, max(p)=3, max(a)=1, max(span)=1 => 7
	// Sim = 4/7 = 0.5714...
	sim := StructuralSimilarity(sig1, sig2)
	expected := 4.0 / 7.0
	if sim < expected-0.001 || sim > expected+0.001 {
		t.Errorf("expected similarity %f, got %f", expected, sim)
	}

	// Empty signatures
	if StructuralSimilarity(StructuralSignature{}, StructuralSignature{}) != 1.0 {
		t.Error("expected similarity 1.0 for empty signatures")
	}
}

func TestStructuralMatcher(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	matcher := NewStructuralMatcher(0.7, logger)

	html1 := `<div><p>A</p><p>B</p></div>`
	html2 := `<div><p>A</p><p>C</p></div>`
	html3 := `<span><a>Link</a></span>`

	res1 := matcher.CheckAndUpdate("page1", html1)
	if res1.Drifted {
		t.Error("expected no drift for first check")
	}

	res2 := matcher.CheckAndUpdate("page2", html2)
	// html1 and html2 have exactly the same structure, so no drift
	if res2.Drifted {
		t.Error("expected no drift for identical structure")
	}

	res3 := matcher.CheckAndUpdate("page3", html3)
	if res3.Drifted {
		t.Error("expected no drift for first check")
	}

	// Test drift
	res4 := matcher.CheckAndUpdate("page1", html3)
	if !res4.Drifted {
		t.Error("expected drift for different structure")
	}

	match := matcher.FindClosestMatch(html1)
	if match.PageID != "page1" && match.PageID != "page2" {
		t.Errorf("expected page1 or page2, got %s", match.PageID)
	}

	match2 := matcher.FindClosestMatch(html2)
	if match2.PageID != "page2" && match2.PageID != "page1" {
		t.Errorf("expected page2 or page1, got %s", match2.PageID)
	}

	// Test no match found
	noMatch := matcher.FindClosestMatch("<html><body>Nothing here</body></html>")
	if noMatch.Found {
		t.Error("expected no match found")
	}

	// Known signatures
	known := matcher.KnownSignatures()
	if len(known) != 3 {
		t.Errorf("expected 3 known signatures, got %d", len(known))
	}

	// Empty HTML
	emptySig := ParseStructuralSignature("")
	if emptySig.TotalNodes != 0 {
		t.Error("expected 0 nodes for empty HTML")
	}

	// Negative threshold
	badMatcher := NewStructuralMatcher(-1.0, nil)
	// The threshold is unexported, but we can verify it doesn't panic
	if badMatcher == nil {
		t.Error("expected matcher to be created")
	}
}
