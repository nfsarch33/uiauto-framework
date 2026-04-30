package accessibility

import (
	"context"
	"encoding/json"
	"fmt"
)

// axeCoreMinJS is a placeholder for the embedded axe-core library.
// In production, this would use //go:embed with a vendored axe.min.js (~300KB).
// For now we inject axe-core from CDN as a fallback.
const axeCDN = "https://cdnjs.cloudflare.com/ajax/libs/axe-core/4.10.2/axe.min.js"

// JSEvaluator abstracts the browser's JS evaluation capability.
// Satisfied by chromedp's chromedp.Evaluate or any Browser implementation.
type JSEvaluator interface {
	Evaluate(ctx context.Context, expression string, result interface{}) error
}

// InjectAndRun loads axe-core into the page (if not already present),
// runs an accessibility audit, and returns the parsed result.
func InjectAndRun(ctx context.Context, eval JSEvaluator, opts ...RunOption) (*AuditResult, error) {
	cfg := defaultRunConfig()
	for _, o := range opts {
		o(&cfg)
	}

	injectionJS := fmt.Sprintf(`
(async () => {
  if (typeof axe === 'undefined') {
    await new Promise((resolve, reject) => {
      const s = document.createElement('script');
      s.src = '%s';
      s.onload = resolve;
      s.onerror = reject;
      document.head.appendChild(s);
    });
  }
  const result = await axe.run(%s);
  return JSON.stringify(result);
})()
`, cfg.axeSource, cfg.runConfigJSON())

	var resultJSON string
	if err := eval.Evaluate(ctx, injectionJS, &resultJSON); err != nil {
		return nil, fmt.Errorf("axe-core injection/run failed: %w", err)
	}

	var result AuditResult
	if err := json.Unmarshal([]byte(resultJSON), &result); err != nil {
		return nil, fmt.Errorf("parsing axe-core result: %w", err)
	}

	return &result, nil
}

// RunOption configures an axe-core audit run.
type RunOption func(*runConfig)

type runConfig struct {
	axeSource string
	tags      []string
	context   string
}

func defaultRunConfig() runConfig {
	return runConfig{
		axeSource: axeCDN,
		tags:      []string{"wcag2aa", "wcag21aa"},
		context:   "document",
	}
}

func (c *runConfig) runConfigJSON() string {
	if len(c.tags) == 0 {
		return c.context
	}
	tagsJSON, _ := json.Marshal(c.tags)
	return fmt.Sprintf(`%s, {runOnly: {type: 'tag', values: %s}}`, c.context, string(tagsJSON))
}

// WithAxeSource overrides the axe-core JS source URL.
func WithAxeSource(url string) RunOption {
	return func(c *runConfig) { c.axeSource = url }
}

// WithTags filters the audit to only run specified tag sets.
func WithTags(tags ...string) RunOption {
	return func(c *runConfig) { c.tags = tags }
}

// WithContext limits the audit scope to a specific CSS selector.
func WithContext(selector string) RunOption {
	return func(c *runConfig) {
		c.context = fmt.Sprintf(`{include: ['%s']}`, selector)
	}
}
