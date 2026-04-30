package uiauto

import (
	"context"
	"fmt"
	"testing"

	"github.com/nfsarch33/uiauto-framework/pkg/llm"
)

type MockVLMProvider struct {
	Response string
	Err      error
}

func (m *MockVLMProvider) Complete(_ context.Context, _ llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return &llm.CompletionResponse{
		Choices: []llm.Choice{
			{Message: llm.Message{Role: "assistant", Content: m.Response}},
		},
	}, nil
}

func TestVLMBridge_AnalyzeScreenshot(t *testing.T) {
	mock := &MockVLMProvider{Response: "The login button is at #submit-btn"}
	bridge := NewVLMBridge(mock, []string{"test-model"})

	result, err := bridge.AnalyzeScreenshot(context.Background(), "login button", []byte("fake-screenshot"))
	if err != nil {
		t.Fatalf("Expected success, got: %v", err)
	}
	if result != "The login button is at #submit-btn" {
		t.Errorf("Unexpected result: %s", result)
	}
}

func TestVLMBridge_AnalyzeScreenshot_AllModelsFail(t *testing.T) {
	mock := &MockVLMProvider{Err: fmt.Errorf("model offline")}
	bridge := NewVLMBridge(mock, []string{"m1", "m2"})

	_, err := bridge.AnalyzeScreenshot(context.Background(), "login button", []byte("fake"))
	if err == nil {
		t.Error("Expected error when all models fail")
	}
}

func TestVLMBridge_DetectElements(t *testing.T) {
	jsonResp := `[{"label":"Login","type":"button","bounding_box":[10,20,100,40],"confidence":0.95}]`
	mock := &MockVLMProvider{Response: jsonResp}
	bridge := NewVLMBridge(mock, []string{"test-model"})

	result, err := bridge.DetectElements(context.Background(), []byte("fake-screenshot"))
	if err != nil {
		t.Fatalf("Expected success, got: %v", err)
	}
	if len(result.Elements) != 1 {
		t.Fatalf("Expected 1 element, got %d", len(result.Elements))
	}
	if result.Elements[0].Label != "Login" {
		t.Errorf("Expected label 'Login', got '%s'", result.Elements[0].Label)
	}
	if result.Elements[0].Confidence != 0.95 {
		t.Errorf("Expected confidence 0.95, got %f", result.Elements[0].Confidence)
	}
}

func TestVLMBridge_DetectElements_BadJSON(t *testing.T) {
	mock := &MockVLMProvider{Response: "not json"}
	bridge := NewVLMBridge(mock, []string{"test-model"})

	result, err := bridge.DetectElements(context.Background(), []byte("fake"))
	if err != nil {
		t.Fatalf("Expected success (with nil elements), got error: %v", err)
	}
	if len(result.Elements) != 0 {
		t.Errorf("Expected 0 elements from bad JSON, got %d", len(result.Elements))
	}
}

func TestVLMBridge_VerifyElement(t *testing.T) {
	mock := &MockVLMProvider{Response: `{"match": true, "confidence": 0.92, "reason": "matches description"}`}
	bridge := NewVLMBridge(mock, []string{"test-model"})

	match, conf, err := bridge.VerifyElement(context.Background(), "login button", []byte("fake"), "#login-btn")
	if err != nil {
		t.Fatalf("Expected success, got: %v", err)
	}
	if !match {
		t.Error("Expected match=true")
	}
	if conf < 0.9 {
		t.Errorf("Expected confidence >= 0.9, got %f", conf)
	}
}

func TestVLMBridge_VerifyElement_TextFallback(t *testing.T) {
	mock := &MockVLMProvider{Response: "Yes, the element matches. true"}
	bridge := NewVLMBridge(mock, []string{"test-model"})

	match, conf, err := bridge.VerifyElement(context.Background(), "button", []byte("fake"), "#btn")
	if err != nil {
		t.Fatalf("Expected success, got: %v", err)
	}
	if !match {
		t.Error("Expected match=true from text fallback containing 'true'")
	}
	if conf != 0.5 {
		t.Errorf("Expected confidence 0.5 for text fallback, got %f", conf)
	}
}

func TestVLMBridge_VLMAsJudge(t *testing.T) {
	jsonResp := `{"present": true, "confidence": 0.92, "explanation": "Login button visible", "suggested_selector": "#login-btn"}`
	mock := &MockVLMProvider{Response: jsonResp}
	bridge := NewVLMBridge(mock, []string{"test-model"})

	result, err := bridge.VLMAsJudge(context.Background(), "login button", []byte("fake-screenshot"))
	if err != nil {
		t.Fatalf("Expected success, got: %v", err)
	}
	if !result.Present {
		t.Error("Expected present=true")
	}
	if result.Confidence < 0.9 {
		t.Errorf("Expected confidence >= 0.9, got %f", result.Confidence)
	}
	if result.SuggestedSelector != "#login-btn" {
		t.Errorf("Expected suggested_selector #login-btn, got %s", result.SuggestedSelector)
	}
	if result.Explanation != "Login button visible" {
		t.Errorf("Expected explanation 'Login button visible', got %s", result.Explanation)
	}
}

func TestVLMBridge_VLMAsJudge_NotPresent(t *testing.T) {
	jsonResp := `{"present": false, "confidence": 0.85, "explanation": "Element not found", "suggested_selector": ""}`
	mock := &MockVLMProvider{Response: jsonResp}
	bridge := NewVLMBridge(mock, []string{"test-model"})

	result, err := bridge.VLMAsJudge(context.Background(), "logout button", []byte("fake"))
	if err != nil {
		t.Fatalf("Expected success, got: %v", err)
	}
	if result.Present {
		t.Error("Expected present=false")
	}
	if result.SuggestedSelector != "" {
		t.Errorf("Expected empty suggested_selector, got %s", result.SuggestedSelector)
	}
}

func TestVLMBridge_VLMAsJudge_BadJSON(t *testing.T) {
	mock := &MockVLMProvider{Response: "The element is visible. true"}
	bridge := NewVLMBridge(mock, []string{"test-model"})

	result, err := bridge.VLMAsJudge(context.Background(), "button", []byte("fake"))
	if err != nil {
		t.Fatalf("Expected success with fallback parse, got: %v", err)
	}
	if !result.Present {
		t.Error("Expected present=true from text fallback containing 'true'")
	}
	if result.Confidence != 0.5 {
		t.Errorf("Expected confidence 0.5 for fallback, got %f", result.Confidence)
	}
}

func TestVLMBridge_VLMAsJudge_AllModelsFail(t *testing.T) {
	mock := &MockVLMProvider{Err: fmt.Errorf("model offline")}
	bridge := NewVLMBridge(mock, []string{"m1", "m2"})

	_, err := bridge.VLMAsJudge(context.Background(), "button", []byte("fake"))
	if err == nil {
		t.Error("Expected error when all models fail")
	}
}

func TestVLMBridge_GenerateSelectorFromVLM(t *testing.T) {
	jsonResp := `{"selector": "#submit-btn", "confidence": 0.88}`
	mock := &MockVLMProvider{Response: jsonResp}
	bridge := NewVLMBridge(mock, []string{"test-model"})

	result, err := bridge.GenerateSelectorFromVLM(context.Background(), "submit button", []byte("fake"))
	if err != nil {
		t.Fatalf("Expected success, got: %v", err)
	}
	if result.Selector != "#submit-btn" {
		t.Errorf("Expected selector #submit-btn, got %s", result.Selector)
	}
	if result.Confidence != 0.88 {
		t.Errorf("Expected confidence 0.88, got %f", result.Confidence)
	}
}

func TestVLMBridge_GenerateSelectorFromVLM_BadJSON(t *testing.T) {
	mock := &MockVLMProvider{Response: "not json"}
	bridge := NewVLMBridge(mock, []string{"test-model"})

	result, err := bridge.GenerateSelectorFromVLM(context.Background(), "button", []byte("fake"))
	if err != nil {
		t.Fatalf("Expected success with fallback, got: %v", err)
	}
	if result.Selector != "" {
		t.Errorf("Expected empty selector for bad JSON, got %s", result.Selector)
	}
	if result.Confidence != 0 {
		t.Errorf("Expected confidence 0 for bad JSON, got %f", result.Confidence)
	}
}

func TestVLMBridge_GenerateSelectorFromVLM_AllModelsFail(t *testing.T) {
	mock := &MockVLMProvider{Err: fmt.Errorf("network error")}
	bridge := NewVLMBridge(mock, []string{"m1"})

	_, err := bridge.GenerateSelectorFromVLM(context.Background(), "button", []byte("fake"))
	if err == nil {
		t.Error("Expected error when all models fail")
	}
}

func TestVLMBridge_Metrics(t *testing.T) {
	mock := &MockVLMProvider{Response: "result"}
	bridge := NewVLMBridge(mock, []string{"m1"})

	_, _ = bridge.AnalyzeScreenshot(context.Background(), "x", []byte("y"))
	_, _ = bridge.AnalyzeScreenshot(context.Background(), "x", []byte("y"))

	snap := bridge.Metrics.Snapshot()
	if snap.TotalCalls != 2 {
		t.Errorf("Expected 2 total calls, got %d", snap.TotalCalls)
	}
	if snap.SuccessCalls != 2 {
		t.Errorf("Expected 2 success calls, got %d", snap.SuccessCalls)
	}
}

func TestVLMBridge_OmniParserAvailable(t *testing.T) {
	mock := &MockVLMProvider{Response: "ok"}
	bridge := NewVLMBridge(mock, []string{"m1"})
	if bridge.IsOmniParserAvailable() {
		t.Error("Should not be available without config")
	}

	bridge2 := NewVLMBridge(mock, []string{"m1"}, WithOmniParser(OmniParserConfig{
		Endpoint: "http://localhost:8080",
		Enabled:  true,
	}))
	if !bridge2.IsOmniParserAvailable() {
		t.Error("Should be available with config")
	}

	bridge3 := NewVLMBridge(mock, []string{"m1"}, WithOmniParser(OmniParserConfig{
		Endpoint: "http://localhost:8080",
		Enabled:  false,
	}))
	if bridge3.IsOmniParserAvailable() {
		t.Error("Should not be available when disabled")
	}
}

func TestUIElement_Fields(t *testing.T) {
	e := UIElement{
		Label:       "Submit",
		Type:        "button",
		BoundingBox: [4]int{10, 20, 100, 40},
		Confidence:  0.95,
		Selector:    "#submit",
	}
	if e.Label != "Submit" || e.Type != "button" || e.Confidence != 0.95 {
		t.Error("UIElement field mismatch")
	}
}

func TestParseElementResponse(t *testing.T) {
	// Valid JSON
	elements := parseElementResponse(`[{"label":"x","type":"button","bounding_box":[0,0,0,0],"confidence":0.9}]`)
	if len(elements) != 1 {
		t.Errorf("Expected 1 element, got %d", len(elements))
	}

	// With markdown wrapping
	elements = parseElementResponse("```json\n[{\"label\":\"y\",\"type\":\"input\",\"bounding_box\":[0,0,0,0],\"confidence\":0.8}]\n```")
	if len(elements) != 1 {
		t.Errorf("Expected 1 element from markdown-wrapped, got %d", len(elements))
	}

	// Invalid
	elements = parseElementResponse("not json")
	if elements != nil {
		t.Error("Expected nil for invalid JSON")
	}

	// Empty array
	elements = parseElementResponse("[]")
	if len(elements) != 0 {
		t.Errorf("Expected 0 elements, got %d", len(elements))
	}
}

func TestMin(t *testing.T) {
	if min(3, 5) != 3 {
		t.Error("min(3,5) should be 3")
	}
	if min(5, 3) != 3 {
		t.Error("min(5,3) should be 3")
	}
	if min(4, 4) != 4 {
		t.Error("min(4,4) should be 4")
	}
}
