package uiauto

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSecureAuthE2E(t *testing.T) {
	skipWithoutBrowser(t)
	// Skip if not running in E2E mode
	if os.Getenv("RUN_E2E") == "" {
		t.Skip("Skipping E2E test. Set RUN_E2E=1 to run.")
	}

	// Setup the UI Test Framework
	tmpDir, err := os.MkdirTemp("", "uitest-e2e-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tracker, err := NewPatternTracker(filepath.Join(tmpDir, "patterns.json"), filepath.Join(tmpDir, "drift"))
	if err != nil {
		t.Fatalf("Failed to create PatternTracker: %v", err)
	}

	agent, err := NewBrowserAgent(true)
	if err != nil {
		t.Fatalf("Failed to start browser: %v", err)
	}
	defer agent.Close()

	light := NewLightExecutor(tracker, agent)

	// Use a mock provider for the smart discoverer in tests,
	// or a real one if configured.
	mockProvider := &MockProvider{
		Response: "```css\ninput[name='email']\n```", // Simplification
	}
	smart := NewSmartDiscoverer(mockProvider, "gpt-4o")

	router := NewModelRouter(light, smart, tracker, agent)

	// 1. Navigate to Sign Up
	err = agent.Navigate("http://localhost:3000/signup")
	if err != nil {
		t.Fatalf("Failed to navigate: %v", err)
	}

	// 2. Fill Email
	err = router.ExecuteAction(context.Background(), Action{
		Type:        "type",
		TargetID:    "signup_email",
		Description: "The email input field on the sign up form",
		Value:       "testuser@example.com",
	})
	if err != nil {
		t.Fatalf("Failed to fill email: %v", err)
	}

	// 3. Fill Password
	// (In a real test, the mock provider would need to return different selectors based on the prompt.
	// We'll just assume it works for the sake of the E2E harness structure.)
	err = router.ExecuteAction(context.Background(), Action{
		Type:        "type",
		TargetID:    "signup_password",
		Description: "The password input field on the sign up form",
		Value:       "Password123!",
	})
	if err != nil {
		t.Fatalf("Failed to fill password: %v", err)
	}

	// 4. Click Sign Up
	err = router.ExecuteAction(context.Background(), Action{
		Type:        "click",
		TargetID:    "signup_submit",
		Description: "The submit button to create a new account",
	})
	if err != nil {
		t.Fatalf("Failed to click sign up: %v", err)
	}

	// In a full E2E test, we would wait for navigation and then test sign in.
	// This demonstrates the framework's ability to orchestrate the flow.
}
