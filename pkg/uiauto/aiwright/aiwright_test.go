package aiwright

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- Type tests ---

func TestBoundingBoxCenter(t *testing.T) {
	tests := []struct {
		name  string
		bb    BoundingBox
		wantX int
		wantY int
	}{
		{"origin", BoundingBox{0, 0, 100, 100}, 50, 50},
		{"offset", BoundingBox{10, 20, 40, 60}, 30, 50},
		{"small", BoundingBox{5, 5, 2, 2}, 6, 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gx, gy := tt.bb.Center()
			if gx != tt.wantX || gy != tt.wantY {
				t.Errorf("Center() = (%d, %d), want (%d, %d)", gx, gy, tt.wantX, tt.wantY)
			}
		})
	}
}

func TestBoundingBoxArea(t *testing.T) {
	bb := BoundingBox{0, 0, 50, 30}
	if got := bb.Area(); got != 1500 {
		t.Errorf("Area() = %d, want 1500", got)
	}
}

func TestBoundingBoxContains(t *testing.T) {
	bb := BoundingBox{10, 10, 100, 50}
	tests := []struct {
		px, py int
		want   bool
	}{
		{50, 30, true},
		{10, 10, true},
		{109, 59, true},
		{110, 60, false},
		{9, 10, false},
		{10, 9, false},
	}
	for _, tt := range tests {
		if got := bb.Contains(tt.px, tt.py); got != tt.want {
			t.Errorf("Contains(%d, %d) = %v, want %v", tt.px, tt.py, got, tt.want)
		}
	}
}

func TestBoundingBoxOverlaps(t *testing.T) {
	a := BoundingBox{0, 0, 100, 100}
	tests := []struct {
		name string
		b    BoundingBox
		want bool
	}{
		{"full overlap", BoundingBox{0, 0, 100, 100}, true},
		{"partial", BoundingBox{50, 50, 100, 100}, true},
		{"no overlap right", BoundingBox{100, 0, 50, 50}, false},
		{"no overlap below", BoundingBox{0, 100, 50, 50}, false},
		{"contained", BoundingBox{10, 10, 10, 10}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := a.Overlaps(tt.b); got != tt.want {
				t.Errorf("Overlaps() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSOMResultFindByID(t *testing.T) {
	result := d2lLoginSOMResult()
	if e := result.FindByID(1); e == nil {
		t.Fatal("FindByID(1) returned nil, expected username field")
	} else if e.Label != "Username" {
		t.Errorf("FindByID(1).Label = %q, want 'Username'", e.Label)
	}
	if e := result.FindByID(999); e != nil {
		t.Error("FindByID(999) should return nil")
	}
}

func TestSOMResultFindByType(t *testing.T) {
	result := d2lLoginSOMResult()
	inputs := result.FindByType(ElementInput)
	if len(inputs) != 2 {
		t.Errorf("FindByType(input) returned %d, want 2", len(inputs))
	}
	buttons := result.FindByType(ElementButton)
	if len(buttons) != 1 {
		t.Errorf("FindByType(button) returned %d, want 1", len(buttons))
	}
}

func TestSOMResultFindByLabel(t *testing.T) {
	result := d2lLoginSOMResult()
	if e := result.FindByLabel("Password"); e == nil {
		t.Fatal("FindByLabel('Password') returned nil")
	} else if e.ID != 2 {
		t.Errorf("FindByLabel('Password').ID = %d, want 2", e.ID)
	}
	if e := result.FindByLabel("Nonexistent"); e != nil {
		t.Error("FindByLabel('Nonexistent') should return nil")
	}
}

// --- Mock server for D2L login page SOM detection ---

func newMockSOMServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/annotate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			http.Error(w, "bad content type", http.StatusBadRequest)
			return
		}
		var req AnnotateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(req.Image) == 0 {
			http.Error(w, "empty image", http.StatusBadRequest)
			return
		}

		result := d2lLoginSOMResult()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	return httptest.NewServer(mux)
}

func TestClientAnnotateD2LLogin(t *testing.T) {
	srv := newMockSOMServer(t)
	defer srv.Close()

	client := NewClient(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	fakeScreenshot := []byte("fake-png-data")
	result, err := client.Annotate(ctx, fakeScreenshot)
	if err != nil {
		t.Fatalf("Annotate() error: %v", err)
	}

	if len(result.Elements) != 5 {
		t.Errorf("expected 5 elements, got %d", len(result.Elements))
	}

	username := result.FindByLabel("Username")
	if username == nil {
		t.Fatal("missing Username input")
	}
	if username.Type != ElementInput {
		t.Errorf("Username type = %q, want input", username.Type)
	}

	loginBtn := result.FindByLabel("Log In")
	if loginBtn == nil {
		t.Fatal("missing Log In button")
	}
	if loginBtn.Type != ElementButton {
		t.Errorf("Log In type = %q, want button", loginBtn.Type)
	}
	if loginBtn.Confidence < 0.9 {
		t.Errorf("Log In confidence = %f, want >= 0.9", loginBtn.Confidence)
	}

	if result.ScreenWidth != 1920 || result.ScreenHeight != 1080 {
		t.Errorf("screen = %dx%d, want 1920x1080", result.ScreenWidth, result.ScreenHeight)
	}

	if result.Latency <= 0 {
		t.Error("Latency should be positive")
	}
}

func TestClientAnnotateEmptyImage(t *testing.T) {
	srv := newMockSOMServer(t)
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.Annotate(context.Background(), []byte{})
	if err == nil {
		t.Error("expected error for empty image")
	}
}

func TestClientAnnotateServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal failure", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	_, err := client.Annotate(context.Background(), []byte("img"))
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestClientHealthCheck(t *testing.T) {
	srv := newMockSOMServer(t)
	defer srv.Close()

	client := NewClient(srv.URL)
	if err := client.HealthCheck(context.Background()); err != nil {
		t.Errorf("HealthCheck() error: %v", err)
	}
}

func TestClientHealthCheckFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	if err := client.HealthCheck(context.Background()); err == nil {
		t.Error("expected health check error")
	}
}

func TestClientOptionOverrides(t *testing.T) {
	custom := &http.Client{Timeout: 99 * time.Second}
	c := NewClient("http://localhost", WithHTTPClient(custom))
	if c.httpClient != custom {
		t.Error("WithHTTPClient not applied")
	}
}

// --- D2L login page fixture ---

func d2lLoginSOMResult() SOMResult {
	return SOMResult{
		ScreenWidth:  1920,
		ScreenHeight: 1080,
		ModelVersion: "ai-wright-v0.3",
		Elements: []SOMElement{
			{
				ID:          1,
				Label:       "Username",
				Type:        ElementInput,
				BoundingBox: BoundingBox{X: 760, Y: 340, Width: 400, Height: 40},
				Confidence:  0.97,
				Attributes:  map[string]string{"placeholder": "Enter your username"},
			},
			{
				ID:          2,
				Label:       "Password",
				Type:        ElementInput,
				BoundingBox: BoundingBox{X: 760, Y: 400, Width: 400, Height: 40},
				Confidence:  0.96,
				Attributes:  map[string]string{"type": "password"},
			},
			{
				ID:          3,
				Label:       "Log In",
				Type:        ElementButton,
				BoundingBox: BoundingBox{X: 860, Y: 470, Width: 200, Height: 44},
				Confidence:  0.99,
			},
			{
				ID:          4,
				Label:       "Forgot Password?",
				Type:        ElementLink,
				BoundingBox: BoundingBox{X: 880, Y: 530, Width: 160, Height: 20},
				Confidence:  0.91,
			},
			{
				ID:          5,
				Label:       "Deakin University",
				Type:        ElementText,
				BoundingBox: BoundingBox{X: 810, Y: 260, Width: 300, Height: 50},
				Confidence:  0.88,
			},
		},
	}
}
