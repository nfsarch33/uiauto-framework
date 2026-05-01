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
	if result.Mode != "visual" {
		t.Errorf("mode = %q, want visual", result.Mode)
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

// Server returns zero elements from /parse, then OmniParser falls back to
// /parse-ocr which returns a non-empty list. Exercises the parseOCR branch.
func TestParse_FallsBackToOCR(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/parse", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(ParseResult{Elements: []UIElement{}})
	})
	mux.HandleFunc("/parse-ocr", func(w http.ResponseWriter, r *http.Request) {
		var req ParseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad", 400)
			return
		}
		json.NewEncoder(w).Encode(ParseResult{
			Elements: []UIElement{{ID: 1, Type: "ocr", Text: "fallback"}},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL)
	got, err := c.Parse(context.Background(), []byte("img"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Elements) != 1 || got.Elements[0].Type != "ocr" {
		t.Errorf("expected OCR fallback element, got %+v", got)
	}
	if got.Mode != "ocr" {
		t.Errorf("mode = %q, want ocr", got.Mode)
	}
	if got.FallbackReason != "visual_zero_elements" {
		t.Errorf("fallback reason = %q", got.FallbackReason)
	}
	if got.OCRTextCount != 1 {
		t.Errorf("ocr text count = %d, want 1", got.OCRTextCount)
	}
}

// Both /parse and /parse-ocr return zero elements -- the original empty
// result must still be returned without error.
func TestParse_OCRAlsoEmptyReturnsOriginal(t *testing.T) {
	mux := http.NewServeMux()
	emptyHandler := func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(ParseResult{Elements: []UIElement{}})
	}
	mux.HandleFunc("/parse", emptyHandler)
	mux.HandleFunc("/parse-ocr", emptyHandler)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL)
	got, err := c.Parse(context.Background(), []byte("img"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Elements) != 0 {
		t.Errorf("expected zero elements when both endpoints return empty, got %d", len(got.Elements))
	}
	if got.FallbackReason != "visual_zero_elements; ocr_zero_elements" {
		t.Errorf("fallback reason = %q", got.FallbackReason)
	}
}

// /parse returns malformed JSON -- decode error path.
func TestParse_DecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()
	c := NewClient(srv.URL)
	if _, err := c.Parse(context.Background(), []byte("img")); err == nil {
		t.Error("expected decode error")
	}
}

// HealthCheck must surface non-404 server errors immediately rather than
// silently falling back to /probe/.
func TestHealthCheck_ServerErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := NewClient(srv.URL)
	if err := c.HealthCheck(context.Background()); err == nil {
		t.Error("expected error for 500")
	}
}

// HealthCheck stops once it receives a non-404 error from /health.
func TestHealthCheck_OnlyHealthOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := NewClient(srv.URL)
	if err := c.HealthCheck(context.Background()); err != nil {
		t.Errorf("expected ok, got %v", err)
	}
}

// Both endpoints return 404 -- final error must say none found.
func TestHealthCheck_BothMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := NewClient(srv.URL)
	err := c.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !contains(err.Error(), "no health endpoint") {
		t.Errorf("expected helpful message, got %v", err)
	}
}

// Network failure (closed server) must propagate as an error from Parse.
func TestParse_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	srv.Close()
	c := NewClient(srv.URL)
	if _, err := c.Parse(context.Background(), []byte("x")); err == nil {
		t.Error("expected error from closed server")
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
