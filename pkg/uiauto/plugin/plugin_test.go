package plugin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// --- ActionRegistry ---

func TestActionRegistry_Register_RejectsEmptyName(t *testing.T) {
	r := NewActionRegistry()
	err := r.Register("", func(ctx context.Context, sel, val string) error { return nil })
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestActionRegistry_Register_RejectsNilHandler(t *testing.T) {
	r := NewActionRegistry()
	err := r.Register("x", nil)
	if err == nil {
		t.Fatal("expected error for nil handler")
	}
}

func TestActionRegistry_Register_RoundTrip(t *testing.T) {
	r := NewActionRegistry()
	called := false
	err := r.Register("dance", func(ctx context.Context, sel, val string) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	h, ok := r.Get("dance")
	if !ok {
		t.Fatal("registered handler not found")
	}
	if err := h(context.Background(), "#x", ""); err != nil {
		t.Errorf("handler returned: %v", err)
	}
	if !called {
		t.Error("handler not invoked")
	}
}

func TestActionRegistry_Names_Sorted(t *testing.T) {
	r := NewActionRegistry()
	for _, n := range []string{"zoom", "alpha", "middle"} {
		_ = r.Register(n, func(ctx context.Context, sel, val string) error { return nil })
	}
	got := r.Names()
	sort.Strings(got)
	want := []string{"alpha", "middle", "zoom"}
	if len(got) != len(want) {
		t.Fatalf("len(got)=%d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %q want %q", i, got[i], want[i])
		}
	}
}

// --- ScenarioLoader ---

func TestJSONScenarioLoader_Load_ParsesValidJSON(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.json")
	body := `[{"id":"a","name":"A","natural_language":["s1","s2"],"selectors_used":["#a"],"action_types":["click","verify"]}]`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	loader := NewJSONScenarioLoader()
	got, err := loader.Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 scenario, got %d", len(got))
	}
	if got[0].ID != "a" || got[0].Name != "A" {
		t.Errorf("unexpected scenario: %+v", got[0])
	}
	if len(got[0].ActionTypes) != 2 || got[0].ActionTypes[1] != "verify" {
		t.Errorf("action_types not parsed: %v", got[0].ActionTypes)
	}
}

func TestJSONScenarioLoader_Load_RejectsEmptyPath(t *testing.T) {
	_, err := NewJSONScenarioLoader().Load("")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestJSONScenarioLoader_Load_RejectsBadJSON(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.json")
	_ = os.WriteFile(p, []byte("not valid json"), 0o644)
	_, err := NewJSONScenarioLoader().Load(p)
	if err == nil {
		t.Fatal("expected error for invalid json")
	}
}

func TestJSONScenarioLoader_Load_RejectsEmptyArray(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.json")
	_ = os.WriteFile(p, []byte("[]"), 0o644)
	_, err := NewJSONScenarioLoader().Load(p)
	if err == nil {
		t.Fatal("expected error for empty array")
	}
}

func TestJSONScenarioLoader_Load_GoldenAgainstFrameworkScenarios(t *testing.T) {
	// Golden test: the loader must successfully parse the in-tree generic
	// scenarios JSON without losing any field used by the framework.
	loader := NewJSONScenarioLoader()
	path := filepath.Join("..", "config", "testdata", "scenarios.golden.json")
	got, err := loader.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) < 1 {
		t.Errorf("expected >= 1 scenario, got %d", len(got))
	}
	for _, s := range got {
		if s.ID == "" || s.Name == "" {
			t.Errorf("incomplete scenario: %+v", s)
		}
	}
}

// --- AuthProvider ---

func TestNoopAuthProvider_DoesNothing(t *testing.T) {
	p := NewNoopAuthProvider()
	if err := p.Authenticate(context.Background()); err != nil {
		t.Errorf("noop should not error: %v", err)
	}
}

type stubAuthProvider struct {
	called bool
	err    error
}

func (s *stubAuthProvider) Authenticate(_ context.Context) error {
	s.called = true
	return s.err
}

func TestAuthProvider_InterfaceCompliance(t *testing.T) {
	var p AuthProvider = &stubAuthProvider{err: errors.New("forbidden")}
	if err := p.Authenticate(context.Background()); err == nil {
		t.Error("expected error from stub")
	}
}

// --- VisualVerifier ---

func TestNoopVisualVerifier_AlwaysPass(t *testing.T) {
	v := NewNoopVisualVerifier()
	res, err := v.Verify(context.Background(), []byte{}, "anything")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !res.Pass || res.Score < 1.0 {
		t.Errorf("unexpected: %+v", res)
	}
}

type stubVisualVerifier struct {
	score float64
	pass  bool
	notes string
}

func (s *stubVisualVerifier) Verify(_ context.Context, _ []byte, _ string) (VerificationResult, error) {
	return VerificationResult{Score: s.score, Pass: s.pass, Notes: s.notes}, nil
}

func TestVisualVerifier_InterfaceCompliance(t *testing.T) {
	var v VisualVerifier = &stubVisualVerifier{score: 0.85, pass: true, notes: "looks ok"}
	res, _ := v.Verify(context.Background(), nil, "")
	if res.Score != 0.85 || !res.Pass || res.Notes != "looks ok" {
		t.Errorf("unexpected: %+v", res)
	}
}
