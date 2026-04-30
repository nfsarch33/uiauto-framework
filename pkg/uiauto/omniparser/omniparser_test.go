package omniparser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func mockParseResult() ParseResult {
	return ParseResult{
		Layout: LayoutInfo{Width: 1920, Height: 1080, PageType: "login", Regions: 3},
		Elements: []UIElement{
			{ID: 1, Type: "input", Text: "Username", BoundingBox: BoundingBox{760, 340, 400, 40}, Confidence: 0.95, Interactable: true},
			{ID: 2, Type: "input", Text: "Password", BoundingBox: BoundingBox{760, 400, 400, 40}, Confidence: 0.94, Interactable: true},
			{ID: 3, Type: "button", Text: "Sign In", BoundingBox: BoundingBox{860, 470, 200, 44}, Confidence: 0.98, Interactable: true},
			{ID: 4, Type: "text", Text: "Welcome", BoundingBox: BoundingBox{800, 200, 320, 60}, Confidence: 0.88, Interactable: false},
			{ID: 5, Type: "link", Text: "Reset Password", BoundingBox: BoundingBox{880, 530, 160, 20}, Confidence: 0.90, Interactable: true},
		},
	}
}

func newMockServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/parse", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}
		var req ParseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(req.Base64Image) == 0 {
			http.Error(w, "empty base64_image", http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(mockParseResult())
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return httptest.NewServer(mux)
}

func TestClientParse(t *testing.T) {
	srv := newMockServer(t)
	defer srv.Close()

	c := NewClient(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := c.Parse(ctx, []byte("fake-screenshot"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Elements) != 5 {
		t.Errorf("elements = %d, want 5", len(result.Elements))
	}
	if result.Layout.PageType != "login" {
		t.Errorf("page_type = %q, want login", result.Layout.PageType)
	}
	if result.Latency <= 0 {
		t.Error("latency should be positive")
	}
}

func TestClientParseEmptyImage(t *testing.T) {
	srv := newMockServer(t)
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.Parse(context.Background(), []byte{})
	if err == nil {
		t.Error("expected error for empty image")
	}
}

func TestClientParseServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "gpu oom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.Parse(context.Background(), []byte("img"))
	if err == nil {
		t.Error("expected 500 error")
	}
}

func TestClientHealthCheck(t *testing.T) {
	srv := newMockServer(t)
	defer srv.Close()
	c := NewClient(srv.URL)
	if err := c.HealthCheck(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestClientHealthCheckDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c := NewClient(srv.URL)
	if err := c.HealthCheck(context.Background()); err == nil {
		t.Error("expected health error")
	}
}

func TestFindInteractable(t *testing.T) {
	result := mockParseResult()
	inter := result.FindInteractable()
	if len(inter) != 4 {
		t.Errorf("interactable = %d, want 4", len(inter))
	}
}

func TestFindByType(t *testing.T) {
	result := mockParseResult()
	inputs := result.FindByType("input")
	if len(inputs) != 2 {
		t.Errorf("inputs = %d, want 2", len(inputs))
	}
	buttons := result.FindByType("button")
	if len(buttons) != 1 {
		t.Errorf("buttons = %d, want 1", len(buttons))
	}
}

func TestWithHTTPClient(t *testing.T) {
	custom := &http.Client{Timeout: 99 * time.Second}
	c := NewClient("http://localhost", WithHTTPClient(custom))
	if c.httpClient != custom {
		t.Error("custom http client not applied")
	}
}

// TDD: HealthCheck should succeed when /health returns 404 but /probe/ returns 200.
// This matches the actual OmniParser FastAPI server which serves GET /probe/ not /health.
func TestHealthCheck_FallbackProbe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/probe/":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"message":"Omniparser API ready"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.HealthCheck(context.Background())
	if err != nil {
		t.Errorf("HealthCheck should succeed via /probe/ fallback, got: %v", err)
	}
}
