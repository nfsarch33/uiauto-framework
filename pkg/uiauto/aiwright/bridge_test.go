package aiwright

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeScreenshot struct {
	data []byte
	err  error
}

func (f *fakeScreenshot) CaptureScreenshot() ([]byte, error) { return f.data, f.err }

func TestBridgeAnalyze(t *testing.T) {
	srv := newMockSOMServer(t)
	defer srv.Close()

	client := NewClient(srv.URL)
	sp := &fakeScreenshot{data: []byte("fake-png")}
	bridge := NewBridge(client, sp)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	em, err := bridge.Analyze(ctx)
	if err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}

	if len(em.Result.Elements) != 5 {
		t.Errorf("expected 5 elements, got %d", len(em.Result.Elements))
	}
	if len(em.Selectors) != 5 {
		t.Errorf("expected 5 selectors, got %d", len(em.Selectors))
	}
	if em.Latency <= 0 {
		t.Error("Latency should be positive")
	}
	if em.CaptureAt.IsZero() {
		t.Error("CaptureAt should be set")
	}
}

func TestBridgeAnalyzeScreenshotError(t *testing.T) {
	client := NewClient("http://unused")
	sp := &fakeScreenshot{err: fmt.Errorf("camera broken")}
	bridge := NewBridge(client, sp)

	_, err := bridge.Analyze(context.Background())
	if err == nil {
		t.Error("expected error from broken screenshot")
	}
}

func TestBridgeAnalyzeEmptyScreenshot(t *testing.T) {
	client := NewClient("http://unused")
	sp := &fakeScreenshot{data: []byte{}}
	bridge := NewBridge(client, sp)

	_, err := bridge.Analyze(context.Background())
	if err == nil {
		t.Error("expected error for empty screenshot")
	}
}

func TestBridgeAnalyzeServerDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	sp := &fakeScreenshot{data: []byte("img")}
	bridge := NewBridge(client, sp)

	_, err := bridge.Analyze(context.Background())
	if err == nil {
		t.Error("expected error for server 503")
	}
}

func TestDefaultMapperD2LElements(t *testing.T) {
	mapper := &DefaultMapper{}
	result := d2lLoginSOMResult()

	tests := []struct {
		elemID int
		want   string
	}{
		{1, `input[placeholder="Enter your username"]`},
		{2, `input[type="password"]`},
		{3, `button:has-text("Log In")`},
		{4, `a:has-text("Forgot Password?")`},
		{5, `[data-som-id="5"]`},
	}
	for _, tt := range tests {
		elem := result.FindByID(tt.elemID)
		if elem == nil {
			t.Fatalf("element %d not found", tt.elemID)
		}
		got := mapper.MapToSelector(*elem)
		if got != tt.want {
			t.Errorf("MapToSelector(id=%d) = %q, want %q", tt.elemID, got, tt.want)
		}
	}
}

func TestDefaultMapperWithIDAttribute(t *testing.T) {
	mapper := &DefaultMapper{}
	elem := SOMElement{
		ID:         99,
		Label:      "Login",
		Type:       ElementButton,
		Attributes: map[string]string{"id": "btn-login"},
	}
	got := mapper.MapToSelector(elem)
	if got != "#btn-login" {
		t.Errorf("MapToSelector with id = %q, want '#btn-login'", got)
	}
}

func TestDefaultMapperWithDataTestID(t *testing.T) {
	mapper := &DefaultMapper{}
	elem := SOMElement{
		ID:         10,
		Type:       ElementInput,
		Attributes: map[string]string{"data-testid": "user-email"},
	}
	got := mapper.MapToSelector(elem)
	if got != `[data-testid="user-email"]` {
		t.Errorf("MapToSelector with data-testid = %q", got)
	}
}

func TestDefaultMapperWithName(t *testing.T) {
	mapper := &DefaultMapper{}
	elem := SOMElement{
		ID:         11,
		Type:       ElementInput,
		Attributes: map[string]string{"name": "email"},
	}
	got := mapper.MapToSelector(elem)
	if got != `[name="email"]` {
		t.Errorf("MapToSelector with name = %q", got)
	}
}

func TestCustomMapper(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(SOMResult{
			ScreenWidth: 800, ScreenHeight: 600,
			Elements: []SOMElement{{ID: 1, Label: "Test", Type: ElementButton}},
		})
	}))
	defer srv.Close()

	custom := &xpathMapper{}
	client := NewClient(srv.URL)
	sp := &fakeScreenshot{data: []byte("img")}
	bridge := NewBridge(client, sp, WithElementMapper(custom))

	em, err := bridge.Analyze(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if em.Selectors[1] != "//button[text()='Test']" {
		t.Errorf("custom mapper = %q, want xpath", em.Selectors[1])
	}
}

type xpathMapper struct{}

func (x *xpathMapper) MapToSelector(elem SOMElement) string {
	switch elem.Type {
	case ElementButton:
		return fmt.Sprintf("//button[text()='%s']", elem.Label)
	default:
		return fmt.Sprintf("//*[@data-som-id='%d']", elem.ID)
	}
}
