// Page-state extraction and question sets for the decision-driven UI
// loop: the browser lane captures DOM, StateFromHTML distils it into the
// flat text state laya consumes, QuestionSet provides the test plan's
// decision schema.
package laya

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// maxStateTextBytes caps visible_text for the English checkpoint's
// 512-token context (~4 KB of prose) so a heavy DOM cannot silently push
// the decision's evidence out of the encoder window.
const maxStateTextBytes = 4000

var spaceRe = regexp.MustCompile(`\s+`)

// StateFromHTML distils a captured DOM into the decision state: the page
// title and its visible text with scripts/styles removed and whitespace
// collapsed. Text is hard-capped at maxStateTextBytes (head kept, tail
// dropped, cut marked) so the state stays inside the encoder context.
func StateFromHTML(html string) State {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return State{"visible_text": norm(html)[:min(len(norm(html)), maxStateTextBytes)]}
	}
	doc.Find("script, style, noscript").Remove()
	title := strings.TrimSpace(doc.Find("title").First().Text())
	text := norm(doc.Find("body").Text())
	if len(text) > maxStateTextBytes {
		text = text[:maxStateTextBytes] + "…"
	}
	if title == "" {
		title = strings.TrimSpace(doc.Find("h1").First().Text())
	}
	return State{"title": title, "visible_text": text}
}

func norm(s string) string { return strings.TrimSpace(spaceRe.ReplaceAllString(s, " ")) }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// QuestionSet returns a named decision schema for the UI loop. Each set
// carries at least one action choice (with criteria the executor can act
// on) and one state assertion (noul) — the two decision kinds every run
// must leave in its evidence.
func QuestionSet(name string) (Questions, error) {
	switch name {
	case "checkout_recovery":
		return Questions{
			"next_action": {
				"type":         "choice",
				"instructions": "Pick the next UI automation action for a checkout recovery test.",
				"criteria": map[string]any{
					"retry_payment":  "A retry payment control is visible and the failure looks transient.",
					"change_card":    "The failure suggests the card itself; switch payment method.",
					"escalate":       "No recovery path is visible; hand the run to a human.",
					"assert_failure": "The test should record this state as the expected failure.",
				},
			},
			"error_visible": {
				"type":         "noul",
				"instructions": "Does the page state show a payment error to the user?",
			},
		}, nil
	case "board_health":
		return Questions{
			"next_action": {
				"type":         "choice",
				"instructions": "Pick the next UI automation action for a board health check.",
				"criteria": map[string]any{
					"assert_loaded": "The board page rendered its ticket content; assert and finish.",
					"retry_wait":    "The page is still loading; wait and re-capture.",
					"escalate":      "The page failed to render; hand the run to a human.",
				},
			},
			"tickets_visible": {
				"type":         "noul",
				"instructions": "Does the page state show ticket/board content to the user?",
			},
		}, nil
	default:
		return nil, fmt.Errorf("laya: unknown question set %q", name)
	}
}

// ErrNoQuestionSet is returned by callers that fail to resolve a set.
var ErrNoQuestionSet = errors.New("laya: question set required")
