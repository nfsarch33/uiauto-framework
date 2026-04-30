package uiauto

import (
	"context"
	"fmt"
	"strings"

	"github.com/nfsarch33/uiauto-framework/pkg/llm"
)

// SmartDiscoverer uses an LLM to analyze the DOM and find the best CSS selector for a target.
type SmartDiscoverer struct {
	provider llm.Provider
	models   []string
}

// NewSmartDiscoverer creates a new SmartDiscoverer with a list of fallback models.
func NewSmartDiscoverer(provider llm.Provider, models ...string) *SmartDiscoverer {
	if len(models) == 0 {
		models = []string{"gpt-5.4-mini", "gpt-4o"}
	}
	return &SmartDiscoverer{
		provider: provider,
		models:   models,
	}
}

// DiscoverSelector asks the LLM to find a CSS selector for the described element in the given HTML.
func (d *SmartDiscoverer) DiscoverSelector(ctx context.Context, description string, html string) (string, error) {
	// Truncate HTML if it's too large to fit in context window
	// For a real implementation, we should use the accessibility tree or strip out scripts/styles
	if len(html) > 50000 {
		html = html[:50000] + "..."
	}

	prompt := fmt.Sprintf(`You are an expert UI automation engineer. 
I need to find a robust CSS selector for an element on a web page.

Element Description: %s

Here is the HTML of the page (or a truncated version of it):
%s

Analyze the HTML and return ONLY the best, most robust CSS selector to find this element.
Do not include any explanation, markdown formatting, or backticks. Just the raw CSS selector string.
Prefer data-testid, id, or unique semantic attributes over brittle class chains.`, description, html)

	temp := 0.1
	var lastErr error
	for _, model := range d.models {
		req := llm.CompletionRequest{
			Model: model,
			Messages: []llm.Message{
				{Role: "user", Content: prompt},
			},
			Temperature: &temp, // Low temperature for deterministic output
		}

		resp, err := d.provider.Complete(ctx, req)
		if err != nil {
			lastErr = fmt.Errorf("model %s failed: %w", model, err)
			continue // try next model
		}

		if len(resp.Choices) == 0 {
			lastErr = fmt.Errorf("model %s returned no choices", model)
			continue
		}

		selector := strings.TrimSpace(resp.Choices[0].Message.Content)
		// Clean up potential markdown backticks if the model ignored instructions
		selector = strings.TrimPrefix(selector, "```css")
		selector = strings.TrimPrefix(selector, "```")
		selector = strings.TrimSuffix(selector, "```")
		selector = strings.TrimSpace(selector)

		return selector, nil
	}

	return "", fmt.Errorf("all models failed, last error: %w", lastErr)
}

// DiscoverScript asks the LLM to write a JavaScript snippet to perform the described action.
func (d *SmartDiscoverer) DiscoverScript(ctx context.Context, description string, html string) (string, error) {
	if len(html) > 50000 {
		html = html[:50000] + "..."
	}

	prompt := fmt.Sprintf(`You are an expert UI automation engineer. 
I need a JavaScript snippet to execute in the browser to perform the following action:

Action Description: %s

Here is the HTML of the page (or a truncated version of it):
%s

Analyze the HTML and return ONLY a valid JavaScript IIFE (Immediately Invoked Function Expression) that performs the action.
If the action is to extract data, the script should return the data.
Do not include any explanation, markdown formatting, or backticks. Just the raw JavaScript code.`, description, html)

	temp := 0.1
	var lastErr error
	for _, model := range d.models {
		req := llm.CompletionRequest{
			Model: model,
			Messages: []llm.Message{
				{Role: "user", Content: prompt},
			},
			Temperature: &temp,
		}

		resp, err := d.provider.Complete(ctx, req)
		if err != nil {
			lastErr = fmt.Errorf("model %s failed: %w", model, err)
			continue
		}

		if len(resp.Choices) == 0 {
			lastErr = fmt.Errorf("model %s returned no choices", model)
			continue
		}

		script := strings.TrimSpace(resp.Choices[0].Message.Content)
		script = strings.TrimPrefix(script, "```javascript")
		script = strings.TrimPrefix(script, "```js")
		script = strings.TrimPrefix(script, "```")
		script = strings.TrimSuffix(script, "```")
		script = strings.TrimSpace(script)

		return script, nil
	}

	return "", fmt.Errorf("all models failed, last error: %w", lastErr)
}
