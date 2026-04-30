package uiauto

import (
	"context"
	"testing"

	"github.com/nfsarch33/uiauto-framework/pkg/llm"
)

// MockProvider implements llm.Provider for testing.
type MockProvider struct {
	Response string
	Err      error
}

func (m *MockProvider) Complete(ctx context.Context, req llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return &llm.CompletionResponse{
		Choices: []llm.Choice{
			{
				Message: llm.Message{
					Role:    "assistant",
					Content: m.Response,
				},
			},
		},
	}, nil
}

func TestSmartDiscoverer(t *testing.T) {
	mock := &MockProvider{
		Response: "```css\n#login-btn\n```",
	}

	discoverer := NewSmartDiscoverer(mock, "test-model")

	html := `<html><body><button id="login-btn">Login</button></body></html>`
	selector, err := discoverer.DiscoverSelector(context.Background(), "The login button", html)
	if err != nil {
		t.Fatalf("DiscoverSelector failed: %v", err)
	}

	if selector != "#login-btn" {
		t.Errorf("Expected '#login-btn', got '%s'", selector)
	}
}
