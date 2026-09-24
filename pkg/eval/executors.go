package eval

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nfsarch33/uiauto-framework/pkg/uiauto"
	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/browseruse"
	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/laya"
)

// BrowserUseExecutor runs browser-use scenarios through the containerized
// executor lane. Success is the lane's own ok PLUS the scenario's
// final_contains check when present — a lane that "succeeds" without the
// expected outcome text is a failed scenario.
type BrowserUseExecutor struct {
	ServiceURL string // browser-use service base URL
}

func (e *BrowserUseExecutor) Name() string { return "browser-use" }

func (e *BrowserUseExecutor) Run(ctx context.Context, sc Scenario, attempt int) RunRecord {
	rec := RunRecord{ScenarioID: sc.ID, Attempt: attempt}
	started := time.Now()
	res, err := browseruse.New(e.ServiceURL).Run(ctx, browseruse.RunRequest{
		Task:     sc.Task,
		URL:      sc.URL,
		MaxSteps: sc.MaxSteps,
		CDPURL:   sc.CDPURL,
	})
	rec.DurationSec = time.Since(started).Seconds()
	rec.Steps = res.Steps
	rec.Errors = res.Errors
	if err != nil {
		rec.Errors = append(rec.Errors, err.Error())
		return rec
	}
	if sc.FinalContains != "" && !strings.Contains(res.FinalResult, sc.FinalContains) {
		rec.Errors = append(rec.Errors, fmt.Sprintf("final result %q does not contain %q", res.FinalResult, sc.FinalContains))
		return rec
	}
	rec.OK = true
	return rec
}

// LayaDecideExecutor runs laya-decide scenarios: capture the page through
// the browser lane, ask the typed-decision service, and grade every
// answer that has a golden. The capture loop mirrors the one in
// cmd/ui-agent laya-decide (plain Navigate + one settle re-capture; the
// empty-state refusal is repeated here: deciding on no evidence is worse
// than failing).
type LayaDecideExecutor struct {
	ServiceURL  string // laya decision service base URL
	ChromeDebug string // optional shared CDP debug URL
	ChromePath  string // optional explicit chrome binary
	// Capture overrides the browser capture (tests inject a fake; the
	// default navigates a chromedp session and distils the page state,
	// with the same one-shot settle re-capture the laya-decide loop uses).
	Capture func(ctx context.Context, sc Scenario) (laya.State, error)
}

func (e *LayaDecideExecutor) Name() string { return "laya-decide" }

func (e *LayaDecideExecutor) Run(ctx context.Context, sc Scenario, attempt int) RunRecord {
	rec := RunRecord{ScenarioID: sc.ID, Attempt: attempt}
	started := time.Now()
	defer func() { rec.DurationSec = time.Since(started).Seconds() }()

	questions, err := laya.QuestionSet(sc.QuestionSet)
	if err != nil {
		rec.Errors = append(rec.Errors, err.Error())
		return rec
	}
	// A golden the question set cannot answer is a harness bug, and an
	// ungradable decision is NOT a pass: without this check a misspelled
	// golden key or a wrong-shaped golden silently drops out of grading
	// and a wrong answer walks through the rubric. Validation fails the
	// scenario instead (fail-open: the record and errors survive; the
	// run does not get a verdict it did not earn).
	if errs := validateGolden(sc.Golden, questions); len(errs) > 0 {
		rec.Errors = append(rec.Errors, errs...)
		return rec
	}

	capture := e.Capture
	if capture == nil {
		capture = e.browserCapture
	}
	state, err := capture(ctx, sc)
	if err != nil {
		rec.Errors = append(rec.Errors, err.Error())
		return rec
	}
	if state["visible_text"] == "" {
		rec.Errors = append(rec.Errors, "page state empty after re-capture; refusing to decide")
		return rec
	}
	state["url"] = sc.URL

	answers, err := laya.New(e.ServiceURL).Decide(ctx, state, questions)
	if err != nil {
		rec.Errors = append(rec.Errors, err.Error())
		return rec
	}

	rec.OK = true
	rec.Steps = 1
	rec.Decisions = gradeAnswers(answers, sc.Golden)
	return rec
}

// validateGolden checks a scenario's golden set against the question set
// it claims to grade: every key must name a question, a choice golden must
// carry a label that is one of the question's criteria, and a noul golden
// must carry its boolean. The misspelled-key case is the dangerous one --
// it silently ungrades a decision -- so it is reported, never ignored.
func validateGolden(golden map[string]Golden, questions laya.Questions) []string {
	var errs []string
	for name, g := range golden {
		q, ok := questions[name]
		if !ok {
			errs = append(errs, fmt.Sprintf("golden key %q has no matching question in the set", name))
			continue
		}
		switch qtype, _ := q["type"].(string); qtype {
		case "choice":
			if g.Label == "" {
				errs = append(errs, fmt.Sprintf("golden %q on a choice question needs a label", name))
				continue
			}
			if criteria, ok := q["criteria"].(map[string]any); ok && len(criteria) > 0 {
				if _, in := criteria[g.Label]; !in {
					errs = append(errs, fmt.Sprintf("golden %q label %q is not one of the question's criteria", name, g.Label))
				}
			}
		case "noul":
			if g.True == nil {
				errs = append(errs, fmt.Sprintf("golden %q on a noul question needs true", name))
			}
		default:
			errs = append(errs, fmt.Sprintf("golden %q names question %q of unsupported type %q", name, name, qtype))
		}
	}
	return errs
}

// gradeAnswers turns typed answers into graded decision records against
// the scenario's golden set. A decision without a golden stays ungraded:
// it still counts toward the confidence metric via Confidence, while
// Correct stays false and the accuracy/Brier math skips it (Want empty /
// WantBool nil).
func gradeAnswers(answers map[string]laya.Answer, golden map[string]Golden) []DecisionRecord {
	out := make([]DecisionRecord, 0, len(answers))
	for name, a := range answers {
		want, hasGolden := golden[name]
		d := DecisionRecord{Question: name, Type: a.Type, Confidence: a.Confidence}
		if a.Type == "choice" {
			d.Got = a.Choice
			d.Dist = a.Probabilities
			if hasGolden && want.Label != "" {
				d.Want = want.Label
				d.Probability = a.Probabilities[want.Label]
			}
		} else if a.Type == "noul" && a.Noul != nil {
			d.NoulValue = a.Noul
			if hasGolden && want.True != nil {
				d.WantBool = want.True
				d.Probability = *a.Noul
			}
		}
		// The verdict is derived, never hand-set: aggregation recomputes
		// it through the same rule, so evidence and metrics cannot drift.
		d.Correct = d.isCorrect()
		out = append(out, d)
	}
	return out
}

// browserCapture is the default capture: navigate a chromedp session,
// distil the DOM into the laya state, and give an SPA shell one settle
// beat before the one-shot re-capture (same contract as the laya-decide
// loop in cmd/ui-agent).
func (e *LayaDecideExecutor) browserCapture(ctx context.Context, sc Scenario) (laya.State, error) {
	agent, err := e.attach()
	if err != nil {
		return nil, fmt.Errorf("attach browser: %w", err)
	}
	defer agent.Close()
	if err := agent.Navigate(sc.URL); err != nil {
		return nil, fmt.Errorf("navigate %s: %w", sc.URL, err)
	}
	dom, err := agent.CaptureDOM()
	if err != nil {
		return nil, fmt.Errorf("capture: %w", err)
	}
	state := laya.StateFromHTML(dom)
	if state["visible_text"] != "" {
		return state, nil
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(3 * time.Second):
	}
	if dom, err = agent.CaptureDOM(); err != nil {
		return nil, fmt.Errorf("re-capture: %w", err)
	}
	return laya.StateFromHTML(dom), nil
}

func (e *LayaDecideExecutor) attach() (*uiauto.BrowserAgent, error) {
	if e.ChromeDebug != "" {
		return uiauto.NewBrowserAgentWithRemote(e.ChromeDebug)
	}
	return uiauto.NewBrowserAgentWithChromePath(uiauto.ResolveChromePath(), true)
}
