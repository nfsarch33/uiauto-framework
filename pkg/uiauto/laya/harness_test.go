package laya

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestStateFromHTML pins the page-state extraction the decision layer
// consumes: title + visible text, no scripts/styles, whitespace
// normalised, size-capped so huge DOMs cannot swamp the encoder context.
func TestStateFromHTML(t *testing.T) {
	html := `<!doctype html><html><head><title>Checkout - Shop</title>
		<style>body{color:red}</style><script>tracking()</script></head>
		<body><h1>Checkout</h1><p>Payment failed - card declined</p>
		<button>Retry payment</button></body></html>`
	st := StateFromHTML(html)
	if st["title"] != "Checkout - Shop" {
		t.Errorf("title = %q", st["title"])
	}
	text := st["visible_text"]
	if !strings.Contains(text, "Payment failed") || !strings.Contains(text, "Retry payment") {
		t.Errorf("visible_text missing content: %q", text)
	}
	if strings.Contains(text, "tracking()") || strings.Contains(text, "color:red") {
		t.Errorf("script/style leaked into state: %q", text)
	}
	if strings.Contains(text, "\n") || strings.Contains(text, "  ") {
		t.Errorf("whitespace not normalised: %q", text)
	}
}

// TestStateFromHTMLCapsSize: a multi-megabyte DOM must not pass through
// whole — the encoder context is 512 tokens for the English checkpoint.
// Length alone is not a sufficient assertion: a cap that only checks
// length can still corrupt its payload (see the rune-boundary test).
func TestStateFromHTMLCapsSize(t *testing.T) {
	big := "<html><head><title>t</title></head><body>" + strings.Repeat("lorem ipsum dolor ", 200_000) + "</body></html>"
	st := StateFromHTML(big)
	if len(st["visible_text"]) > maxStateTextBytes {
		t.Fatalf("visible_text len = %d, cap %d", len(st["visible_text"]), maxStateTextBytes)
	}
	if !utf8.ValidString(st["visible_text"]) {
		t.Fatalf("visible_text is not valid UTF-8 after cap")
	}
}

// TestStateFromHTMLCapCutsOnRuneBoundary pins the non-ASCII case: ASCII
// body up to just under the cap followed by multi-byte runes means the
// byte cut would split a rune. The JSON encoder coerces such a broken
// tail to U+FFFD, silently changing the state sent to the decision
// service — any localised page hits this routinely.
func TestStateFromHTMLCapCutsOnRuneBoundary(t *testing.T) {
	body := strings.Repeat("a", maxStateTextBytes-4) + strings.Repeat("é", 64)
	st := StateFromHTML("<html><head><title>t</title></head><body>" + body + "</body></html>")
	text := st["visible_text"]
	if len(text) > maxStateTextBytes {
		t.Fatalf("len = %d, cap %d", len(text), maxStateTextBytes)
	}
	if !utf8.ValidString(text) {
		t.Fatalf("cap split a rune: last bytes %q", text[len(text)-8:])
	}
	if !strings.HasSuffix(text, "…") {
		t.Fatalf("cut not marked: tail %q", text[len(text)-8:])
	}
}

// TestTruncateUTF8NeverSplitsRunes walks the helper across cut positions
// that straddle runes of 2-4 bytes: every result is valid UTF-8, within
// the byte budget, and marked with an ellipsis when cut.
func TestTruncateUTF8NeverSplitsRunes(t *testing.T) {
	for _, r := range []string{"é", "€", " 😀"} {
		s := strings.Repeat("a", maxStateTextBytes-2) + strings.Repeat(strings.TrimSpace(r), 32)
		got := truncateUTF8(s, maxStateTextBytes)
		if len(got) > maxStateTextBytes {
			t.Errorf("rune %q: len %d over cap", r, len(got))
		}
		if !utf8.ValidString(got) {
			t.Errorf("rune %q: invalid UTF-8 tail %q", r, got[len(got)-8:])
		}
	}
	if got := truncateUTF8("short", maxStateTextBytes); got != "short" {
		t.Errorf("under-cap string changed: %q", got)
	}
}

// TestQuestionSetShapes: the built-in sets carry exactly the two question
// kinds the loop needs — an action choice with criteria and a state
// assertion as noul — and unknown sets error instead of silently
// returning a set for the wrong page.
func TestQuestionSetShapes(t *testing.T) {
	for _, name := range []string{"checkout_recovery", "board_health"} {
		qs, err := QuestionSet(name)
		if err != nil {
			t.Fatalf("QuestionSet(%q): %v", name, err)
		}
		if len(qs) < 2 {
			t.Fatalf("%q has %d questions, want >= 2 (action + assertion)", name, len(qs))
		}
		sawChoice, sawNoul := false, false
		for _, q := range qs {
			if q["type"] == "choice" && len(q["criteria"].(map[string]any)) >= 2 {
				sawChoice = true
			}
			if q["type"] == "noul" {
				sawNoul = true
			}
		}
		if !sawChoice || !sawNoul {
			t.Errorf("%q missing shape: choice=%v noul=%v", name, sawChoice, sawNoul)
		}
	}
	if _, err := QuestionSet("nope"); err == nil {
		t.Fatal("unknown question set returned no error")
	}
}

// TestQuestionSetCarriesHealthyOption: every built-in choice question
// must offer a healthy-page answer. Week 1 of the eval ledger showed
// page states (order summary with no error, shipping step, empty basket)
// that fit none of the failure options, forcing a wrong decision with
// contestable ground truth.
func TestQuestionSetCarriesHealthyOption(t *testing.T) {
	healthy := map[string]map[string]bool{
		"checkout_recovery": {"no_error_continue": false},
		"board_health":      {"assert_loaded": false},
	}
	for set, want := range healthy {
		qs, err := QuestionSet(set)
		if err != nil {
			t.Fatalf("QuestionSet(%q): %v", set, err)
		}
		criteria, _ := qs["next_action"]["criteria"].(map[string]any)
		for option := range want {
			if _, ok := criteria[option]; !ok {
				t.Errorf("%q next_action missing healthy option %q (criteria: %v)", set, option, criteriaKeys(criteria))
			}
		}
	}
}

func criteriaKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
