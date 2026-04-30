package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestJSONFileStoreCRUD(t *testing.T) {
	s, err := NewJSONFileStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()
	p := Pattern{
		ID: "btn-login", Selector: "#login-btn", Description: "Login button",
		LastSeen: time.Now(), Confidence: 0.95,
		Metadata: map[string]string{"page": "home"},
	}

	if err := s.Set(ctx, p); err != nil {
		t.Fatal(err)
	}

	got, ok := s.Get(ctx, "btn-login")
	if !ok {
		t.Fatal("pattern not found")
	}
	if got.Selector != "#login-btn" {
		t.Errorf("Selector = %q, want #login-btn", got.Selector)
	}
	if got.Confidence != 0.95 {
		t.Errorf("Confidence = %f, want 0.95", got.Confidence)
	}
	if got.Metadata["page"] != "home" {
		t.Errorf("Metadata[page] = %q, want home", got.Metadata["page"])
	}
}

func TestJSONFileStoreUpsert(t *testing.T) {
	s, err := NewJSONFileStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()
	p := Pattern{ID: "x", Selector: ".old", LastSeen: time.Now(), Confidence: 0.5}
	s.Set(ctx, p)

	p.Selector = ".new"
	p.Confidence = 0.9
	s.Set(ctx, p)

	got, _ := s.Get(ctx, "x")
	if got.Selector != ".new" {
		t.Errorf("Selector = %q after upsert, want .new", got.Selector)
	}
}

func TestJSONFileStoreLoad(t *testing.T) {
	s, err := NewJSONFileStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()
	s.Set(ctx, Pattern{ID: "a", Selector: ".a", LastSeen: time.Now()})
	s.Set(ctx, Pattern{ID: "b", Selector: ".b", LastSeen: time.Now()})

	all, err := s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("Load() = %d patterns, want 2", len(all))
	}
}

func TestJSONFileStoreDecay(t *testing.T) {
	s, err := NewJSONFileStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()
	old := Pattern{ID: "old", Selector: ".old", LastSeen: time.Now().Add(-48 * time.Hour), Confidence: 1.0}
	fresh := Pattern{ID: "fresh", Selector: ".fresh", LastSeen: time.Now(), Confidence: 1.0}
	s.Set(ctx, old)
	s.Set(ctx, fresh)

	if err := s.DecayConfidence(ctx, 24*time.Hour, 0.5); err != nil {
		t.Fatal(err)
	}

	oldP, _ := s.Get(ctx, "old")
	if oldP.Confidence > 0.6 {
		t.Errorf("old confidence = %f, want ~0.5", oldP.Confidence)
	}

	freshP, _ := s.Get(ctx, "fresh")
	if freshP.Confidence < 0.9 {
		t.Errorf("fresh confidence = %f, want ~1.0", freshP.Confidence)
	}
}

func TestJSONFileStoreBoost(t *testing.T) {
	s, err := NewJSONFileStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()
	s.Set(ctx, Pattern{ID: "x", Selector: ".x", LastSeen: time.Now(), Confidence: 0.5})

	if err := s.BoostConfidence(ctx, "x", 0.3); err != nil {
		t.Fatal(err)
	}

	got, _ := s.Get(ctx, "x")
	if got.Confidence < 0.79 || got.Confidence > 0.81 {
		t.Errorf("Confidence = %f, want ~0.8", got.Confidence)
	}

	s.BoostConfidence(ctx, "x", 0.5)
	got, _ = s.Get(ctx, "x")
	if got.Confidence > 1.01 {
		t.Errorf("Confidence = %f, should cap at 1.0", got.Confidence)
	}
}

func TestJSONFileStoreBoostNotFound(t *testing.T) {
	s, err := NewJSONFileStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	err = s.BoostConfidence(context.Background(), "missing", 0.1)
	if err == nil {
		t.Error("expected error for missing pattern")
	}
}

func TestJSONFileStoreGetNotFound(t *testing.T) {
	s, err := NewJSONFileStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	_, ok := s.Get(context.Background(), "nope")
	if ok {
		t.Error("should return false for missing pattern")
	}
}

func TestJSONFileStoreCount(t *testing.T) {
	s, _ := NewJSONFileStore(":memory:")
	defer s.Close()
	ctx := context.Background()

	if s.Count() != 0 {
		t.Errorf("Count() = %d, want 0", s.Count())
	}
	s.Set(ctx, Pattern{ID: "a", Selector: ".a", LastSeen: time.Now()})
	s.Set(ctx, Pattern{ID: "b", Selector: ".b", LastSeen: time.Now()})
	if s.Count() != 2 {
		t.Errorf("Count() = %d, want 2", s.Count())
	}
}

func TestJSONFileStoreDiskPersistence(t *testing.T) {
	tmp := t.TempDir() + "/patterns.json"
	s, err := NewJSONFileStore(tmp)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	s.Set(ctx, Pattern{ID: "persist", Selector: ".persist", LastSeen: time.Now(), Confidence: 0.88})
	s.Close()

	s2, err := NewJSONFileStore(tmp)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, ok := s2.Get(ctx, "persist")
	if !ok {
		t.Fatal("pattern not found after reopen")
	}
	if got.Selector != ".persist" {
		t.Errorf("Selector = %q, want .persist", got.Selector)
	}
	if got.Confidence != 0.88 {
		t.Errorf("Confidence = %f, want 0.88", got.Confidence)
	}
}

// --- Fallback/Circuit breaker tests ---

type failingStore struct {
	failAfter int
	calls     int
}

func (f *failingStore) Get(_ context.Context, id string) (Pattern, bool) {
	f.calls++
	if f.calls > f.failAfter {
		return Pattern{}, false
	}
	return Pattern{ID: id, Selector: ".primary"}, true
}

func (f *failingStore) Set(_ context.Context, _ Pattern) error {
	f.calls++
	if f.calls > f.failAfter {
		return fmt.Errorf("primary down")
	}
	return nil
}

func (f *failingStore) Load(_ context.Context) (map[string]Pattern, error) {
	f.calls++
	if f.calls > f.failAfter {
		return nil, fmt.Errorf("primary down")
	}
	return map[string]Pattern{}, nil
}

func (f *failingStore) DecayConfidence(_ context.Context, _ time.Duration, _ float64) error {
	f.calls++
	if f.calls > f.failAfter {
		return fmt.Errorf("primary down")
	}
	return nil
}

func (f *failingStore) BoostConfidence(_ context.Context, _ string, _ float64) error {
	f.calls++
	if f.calls > f.failAfter {
		return fmt.Errorf("primary down")
	}
	return nil
}

func TestFallbackStoreCircuitBreaker(t *testing.T) {
	fallbackDB, err := NewJSONFileStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer fallbackDB.Close()

	primary := &failingStore{failAfter: 0}
	cfg := CircuitBreakerConfig{FailureThreshold: 3, HalfOpenTimeout: 50 * time.Millisecond}
	fs := NewFallbackStore(primary, fallbackDB, cfg)

	ctx := context.Background()

	fallbackDB.Set(ctx, Pattern{ID: "test", Selector: ".fallback", LastSeen: time.Now()})

	for i := 0; i < 3; i++ {
		fs.Set(ctx, Pattern{ID: fmt.Sprintf("p%d", i), Selector: ".x", LastSeen: time.Now()})
	}

	if fs.State() != CircuitOpen {
		t.Errorf("state = %v, want open", fs.State())
	}

	patterns, err := fs.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := patterns["test"]; !ok {
		t.Error("fallback should have 'test' pattern")
	}

	time.Sleep(60 * time.Millisecond)

	fs.Set(ctx, Pattern{ID: "retry", Selector: ".x", LastSeen: time.Now()})
	if fs.State() != CircuitOpen {
		t.Errorf("state after half-open retry = %v, want open", fs.State())
	}
}

func TestFallbackStoreHalfOpenRecovery(t *testing.T) {
	primary, _ := NewJSONFileStore(":memory:")
	fallback, _ := NewJSONFileStore(":memory:")
	defer primary.Close()
	defer fallback.Close()

	cfg := CircuitBreakerConfig{FailureThreshold: 1, HalfOpenTimeout: 20 * time.Millisecond}
	fs := NewFallbackStore(&failingStore{failAfter: 0}, fallback, cfg)

	ctx := context.Background()
	fs.Set(ctx, Pattern{ID: "trigger", Selector: ".x", LastSeen: time.Now()})
	if fs.State() != CircuitOpen {
		t.Fatal("should be open")
	}

	// Replace with a working primary
	fs2 := NewFallbackStore(primary, fallback, cfg)
	fs2.mu.Lock()
	fs2.state = CircuitHalfOpen
	fs2.mu.Unlock()

	primary.Set(ctx, Pattern{ID: "recovered", Selector: ".ok", LastSeen: time.Now()})

	got, ok := fs2.Get(ctx, "recovered")
	if !ok {
		t.Fatal("half-open get should succeed and close circuit")
	}
	if got.Selector != ".ok" {
		t.Errorf("Selector = %q, want .ok", got.Selector)
	}
	if fs2.State() != CircuitClosed {
		t.Errorf("state = %v, want closed after recovery", fs2.State())
	}
}

func TestFallbackStoreNormalOperation(t *testing.T) {
	primaryDB, _ := NewJSONFileStore(":memory:")
	fallbackDB, _ := NewJSONFileStore(":memory:")
	defer primaryDB.Close()
	defer fallbackDB.Close()

	cfg := DefaultCircuitBreakerConfig()
	fs := NewFallbackStore(primaryDB, fallbackDB, cfg)

	ctx := context.Background()
	p := Pattern{ID: "btn", Selector: ".btn", LastSeen: time.Now(), Confidence: 0.9}
	if err := fs.Set(ctx, p); err != nil {
		t.Fatal(err)
	}

	got, ok := fs.Get(ctx, "btn")
	if !ok {
		t.Fatal("pattern not found")
	}
	if got.Selector != ".btn" {
		t.Errorf("Selector = %q, want .btn", got.Selector)
	}

	if fs.State() != CircuitClosed {
		t.Errorf("state = %v, want closed", fs.State())
	}
}

func TestFallbackStoreWriteThrough(t *testing.T) {
	primary, _ := NewJSONFileStore(":memory:")
	fallback, _ := NewJSONFileStore(":memory:")
	defer primary.Close()
	defer fallback.Close()

	fs := NewFallbackStore(primary, fallback, DefaultCircuitBreakerConfig())
	ctx := context.Background()

	fs.Set(ctx, Pattern{ID: "wt", Selector: ".wt", LastSeen: time.Now()})

	// Should be in both stores
	_, ok := fallback.Get(ctx, "wt")
	if !ok {
		t.Error("write-through to fallback failed")
	}
}

func TestFallbackStoreDecayAndBoost(t *testing.T) {
	primary, _ := NewJSONFileStore(":memory:")
	fallback, _ := NewJSONFileStore(":memory:")
	defer primary.Close()
	defer fallback.Close()

	fs := NewFallbackStore(primary, fallback, DefaultCircuitBreakerConfig())
	ctx := context.Background()

	fs.Set(ctx, Pattern{ID: "d", Selector: ".d", LastSeen: time.Now().Add(-48 * time.Hour), Confidence: 1.0})
	fs.DecayConfidence(ctx, 24*time.Hour, 0.5)
	got, _ := fs.Get(ctx, "d")
	if got.Confidence > 0.6 {
		t.Errorf("decayed confidence = %f, want ~0.5", got.Confidence)
	}

	fs.Set(ctx, Pattern{ID: "b", Selector: ".b", LastSeen: time.Now(), Confidence: 0.5})
	fs.BoostConfidence(ctx, "b", 0.4)
	got, _ = fs.Get(ctx, "b")
	if got.Confidence < 0.89 || got.Confidence > 0.91 {
		t.Errorf("boosted confidence = %f, want ~0.9", got.Confidence)
	}
}

func TestCircuitStateString(t *testing.T) {
	tests := []struct {
		s    CircuitState
		want string
	}{
		{CircuitClosed, "closed"},
		{CircuitOpen, "open"},
		{CircuitHalfOpen, "half-open"},
		{CircuitState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("CircuitState(%d) = %q, want %q", tt.s, got, tt.want)
		}
	}
}
