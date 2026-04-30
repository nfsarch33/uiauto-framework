package mutation

import (
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Result captures the outcome of a single mutation application.
type Result struct {
	Operator OperatorType
	Tier     Tier
	Selector string
	Mutated  int
	Err      error
}

// RunResult aggregates all mutation results from a single run.
type RunResult struct {
	Results      []Result
	TotalMutated int
	Errors       int
}

// Runner applies a sequence of mutations to an HTML document.
type Runner struct {
	config    Config
	operators []*Operator
}

// NewRunner creates a mutation runner with the given config and operators.
func NewRunner(cfg Config, ops ...*Operator) *Runner {
	return &Runner{
		config:    cfg,
		operators: ops,
	}
}

// Run applies each operator to the document for the given selector.
func (r *Runner) Run(html string, selector string) (string, RunResult, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", RunResult{}, fmt.Errorf("parsing html: %w", err)
	}

	var result RunResult
	for _, op := range r.operators {
		if r.config.MaxMutations > 0 && result.TotalMutated >= r.config.MaxMutations {
			break
		}

		n, err := op.Apply(doc, selector)
		res := Result{
			Operator: op.Type,
			Tier:     op.Tier,
			Selector: selector,
			Mutated:  n,
			Err:      err,
		}
		result.Results = append(result.Results, res)
		result.TotalMutated += n
		if err != nil {
			result.Errors++
		}
	}

	out, err := doc.Html()
	if err != nil {
		return "", result, fmt.Errorf("rendering html: %w", err)
	}
	return out, result, nil
}

// RunMulti applies operators to multiple selectors.
func (r *Runner) RunMulti(html string, selectors []string) (string, RunResult, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", RunResult{}, fmt.Errorf("parsing html: %w", err)
	}

	var result RunResult
	for _, sel := range selectors {
		for _, op := range r.operators {
			if r.config.MaxMutations > 0 && result.TotalMutated >= r.config.MaxMutations {
				break
			}
			n, applyErr := op.Apply(doc, sel)
			res := Result{
				Operator: op.Type,
				Tier:     op.Tier,
				Selector: sel,
				Mutated:  n,
				Err:      applyErr,
			}
			result.Results = append(result.Results, res)
			result.TotalMutated += n
			if applyErr != nil {
				result.Errors++
			}
		}
	}

	out, err := doc.Html()
	if err != nil {
		return "", result, fmt.Errorf("rendering html: %w", err)
	}
	return out, result, nil
}
