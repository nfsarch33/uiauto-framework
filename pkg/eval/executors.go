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

	agent, err := e.attach()
	if err != nil {
		rec.Errors = append(rec.Errors, fmt.Sprintf("attach browser: %v", err))
		return rec
	}
	defer agent.Close()

	if err := agent.Navigate(sc.URL); err != nil {
		rec.Errors = append(rec.Errors, fmt.Sprintf("navigate %s: %v", sc.URL, err))
		return rec
	}
	dom, err := agent.CaptureDOM()
	if err != nil {
		rec.Errors = append(rec.Errors, fmt.Sprintf("capture: %v", err))
		return rec
	}
	state := laya.StateFromHTML(dom)
	if state["visible_text"] == "" {
		select {
		case <-ctx.Done():
			rec.Errors = append(rec.Errors, ctx.Err().Error())
			return rec
		case <-time.After(3 * time.Second):
		}
		if dom, err = agent.CaptureDOM(); err != nil {
			rec.Errors = append(rec.Errors, fmt.Sprintf("re-capture: %v", err))
			return rec
		}
		state = laya.StateFromHTML(dom)
		if state["visible_text"] == "" {
			rec.Errors = append(rec.Errors, "page state empty after re-capture; refusing to decide")
			return rec
		}
	}
	state["url"] = sc.URL

	answers, err := laya.New(e.ServiceURL).Decide(ctx, state, questions)
	if err != nil {
		rec.Errors = append(rec.Errors, err.Error())
		return rec
	}

	rec.OK = true
	rec.Steps = 1
	for name, q := range questions {
		want, hasGolden := sc.Golden[name]
		a := answers[name]
		d := DecisionRecord{Question: name, Type: a.Type, Confidence: a.Confidence}
		if a.Type == "choice" {
			d.Got = a.Choice
			d.Dist = a.Probabilities
			if hasGolden && want.Label != "" {
				d.Want = want.Label
				d.Probability = a.Probabilities[want.Label]
				d.Correct = a.Choice == want.Label
			}
		} else if a.Type == "noul" && a.Noul != nil {
			d.NoulValue = a.Noul
			if hasGolden && want.True != nil {
				d.WantBool = want.True
				got := *a.Noul >= 0.5
				d.Probability = *a.Noul
				d.Correct = got == *want.True
			}
		}
		// An ungradable decision (no golden) still counts toward the
		// confidence metric via Confidence; Correct stays false and the
		// Brier/accuracy math skips it (Want empty / WantBool nil).
		_ = q
		rec.Decisions = append(rec.Decisions, d)
	}
	return rec
}

func (e *LayaDecideExecutor) attach() (*uiauto.BrowserAgent, error) {
	if e.ChromeDebug != "" {
		return uiauto.NewBrowserAgentWithRemote(e.ChromeDebug)
	}
	return uiauto.NewBrowserAgentWithChromePath(uiauto.ResolveChromePath(), true)
}
