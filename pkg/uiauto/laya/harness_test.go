package laya

import (
	"strings"
	"testing"
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
func TestStateFromHTMLCapsSize(t *testing.T) {
	big := "<html><head><title>t</title></head><body>" + strings.Repeat("lorem ipsum dolor ", 200_000) + "</body></html>"
	st := StateFromHTML(big)
	if len(st["visible_text"]) > maxStateTextBytes {
		t.Fatalf("visible_text len = %d, cap %d", len(st["visible_text"]), maxStateTextBytes)
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
