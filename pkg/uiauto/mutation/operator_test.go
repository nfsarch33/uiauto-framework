package mutation

import (
	"strings"
	"testing"
)

const testHTML = `<html><body>
<div id="main" class="container primary">
  <button id="login-btn" class="btn primary" data-testid="login-button">Login</button>
  <input id="username" class="form-input" data-testid="username-field" />
  <ul id="items">
    <li class="item">First</li>
    <li class="item">Second</li>
    <li class="item">Third</li>
  </ul>
  <span class="label">Status</span>
</div>
</body></html>`

func TestRenameClass(t *testing.T) {
	tests := []struct {
		name     string
		selector string
		suffix   string
		wantMut  int
		contains string
	}{
		{"rename button classes", "button", "-v2", 1, `class="btn-v2 primary-v2"`},
		{"rename all items", ".item", "-new", 3, `class="item-new"`},
		{"no match", ".nonexistent", "-x", 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op := RenameClass(tt.suffix)
			runner := NewRunner(DefaultConfig(), op)
			out, result, err := runner.Run(testHTML, tt.selector)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.TotalMutated != tt.wantMut {
				t.Errorf("mutated = %d, want %d", result.TotalMutated, tt.wantMut)
			}
			if tt.contains != "" && !strings.Contains(out, tt.contains) {
				t.Errorf("output missing %q", tt.contains)
			}
			if op.Tier != TierA {
				t.Errorf("tier = %s, want A", op.Tier)
			}
		})
	}
}

func TestRemoveID(t *testing.T) {
	tests := []struct {
		name     string
		selector string
		wantMut  int
		absent   string
	}{
		{"remove button id", "#login-btn", 1, `id="login-btn"`},
		{"remove input id", "#username", 1, `id="username"`},
		{"no id to remove", ".item", 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op := RemoveID()
			runner := NewRunner(DefaultConfig(), op)
			out, result, err := runner.Run(testHTML, tt.selector)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.TotalMutated != tt.wantMut {
				t.Errorf("mutated = %d, want %d", result.TotalMutated, tt.wantMut)
			}
			if tt.absent != "" && strings.Contains(out, tt.absent) {
				t.Errorf("output should not contain %q", tt.absent)
			}
		})
	}
}

func TestChangeTestID(t *testing.T) {
	tests := []struct {
		name     string
		selector string
		prefix   string
		wantMut  int
		contains string
	}{
		{"prefix login button", "[data-testid='login-button']", "new-", 1, `data-testid="new-login-button"`},
		{"no testid", ".item", "", 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op := ChangeTestID(tt.prefix)
			runner := NewRunner(DefaultConfig(), op)
			out, result, err := runner.Run(testHTML, tt.selector)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.TotalMutated != tt.wantMut {
				t.Errorf("mutated = %d, want %d", result.TotalMutated, tt.wantMut)
			}
			if tt.contains != "" && !strings.Contains(out, tt.contains) {
				t.Errorf("output missing %q", tt.contains)
			}
		})
	}
}

func TestWrapElement(t *testing.T) {
	tests := []struct {
		name     string
		selector string
		wantMut  int
		contains string
	}{
		{"wrap button", "#login-btn", 1, `class="mutation-wrapper"`},
		{"wrap all items", ".item", 3, `class="mutation-wrapper"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op := WrapElement("mutation-wrapper")
			runner := NewRunner(DefaultConfig(), op)
			out, result, err := runner.Run(testHTML, tt.selector)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.TotalMutated != tt.wantMut {
				t.Errorf("mutated = %d, want %d", result.TotalMutated, tt.wantMut)
			}
			if !strings.Contains(out, tt.contains) {
				t.Errorf("output missing %q", tt.contains)
			}
		})
	}
}

func TestReorderSiblings(t *testing.T) {
	op := ReorderSiblings()
	runner := NewRunner(DefaultConfig(), op)
	out, result, err := runner.Run(testHTML, "#items")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TotalMutated != 1 {
		t.Errorf("mutated = %d, want 1", result.TotalMutated)
	}
	// After reversal, "Third" should appear before "First"
	thirdIdx := strings.Index(out, "Third")
	firstIdx := strings.Index(out, "First")
	if thirdIdx >= firstIdx {
		t.Error("siblings not reversed: Third should appear before First")
	}
}

func TestReorderSiblings_SingleChild(t *testing.T) {
	html := `<html><body><div id="single"><span>Only</span></div></body></html>`
	op := ReorderSiblings()
	runner := NewRunner(DefaultConfig(), op)
	_, result, err := runner.Run(html, "#single")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TotalMutated != 0 {
		t.Errorf("should not mutate single-child parent, got %d", result.TotalMutated)
	}
}

func TestChangeTag(t *testing.T) {
	tests := []struct {
		name     string
		selector string
		newTag   string
		wantMut  int
		contains string
	}{
		{"span to div", ".label", "div", 1, "<div"},
		{"button to a", "#login-btn", "a", 1, "<a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op := ChangeTag(tt.newTag)
			runner := NewRunner(DefaultConfig(), op)
			out, result, err := runner.Run(testHTML, tt.selector)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.TotalMutated != tt.wantMut {
				t.Errorf("mutated = %d, want %d", result.TotalMutated, tt.wantMut)
			}
			if !strings.Contains(out, tt.contains) {
				t.Errorf("output missing %q", tt.contains)
			}
		})
	}
}

func TestRunnerMulti(t *testing.T) {
	runner := NewRunner(DefaultConfig(), RenameClass("-v2"), RemoveID())
	out, result, err := runner.RunMulti(testHTML, []string{"button", "#username"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TotalMutated < 2 {
		t.Errorf("expected at least 2 mutations, got %d", result.TotalMutated)
	}
	if !strings.Contains(out, "btn-v2") {
		t.Error("output missing renamed class")
	}
	if result.Errors != 0 {
		t.Errorf("unexpected errors: %d", result.Errors)
	}
}

func TestRunnerMaxMutations(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxMutations = 1
	runner := NewRunner(cfg, RenameClass("-v2"), RemoveID(), ChangeTag("div"))
	_, result, err := runner.Run(testHTML, "button")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TotalMutated > 1 {
		t.Errorf("expected max 1 mutation, got %d", result.TotalMutated)
	}
}

func TestConfigIntensityRate(t *testing.T) {
	tests := []struct {
		intensity Intensity
		want      float64
	}{
		{IntensityLow, 0.10},
		{IntensityMedium, 0.30},
		{IntensityHigh, 0.60},
		{"unknown", 0.30},
	}

	for _, tt := range tests {
		t.Run(string(tt.intensity), func(t *testing.T) {
			cfg := Config{Intensity: tt.intensity}
			if got := cfg.IntensityRate(); got != tt.want {
				t.Errorf("IntensityRate() = %f, want %f", got, tt.want)
			}
		})
	}
}

func TestConfigRng(t *testing.T) {
	cfg := Config{Seed: 123}
	rng1 := cfg.Rng()
	rng2 := cfg.Rng()
	if rng1.Int63() != rng2.Int63() {
		t.Error("same seed should produce same sequence")
	}
}

func TestOperatorMetadata(t *testing.T) {
	ops := []*Operator{
		RenameClass("-v2"),
		RemoveID(),
		ChangeTestID("new-"),
		WrapElement("wrap"),
		ReorderSiblings(),
		ChangeTag("div"),
	}

	for _, op := range ops {
		if op.Type == "" {
			t.Errorf("operator missing type")
		}
		if op.Tier != TierA {
			t.Errorf("operator %s: tier = %s, want A", op.Type, op.Tier)
		}
		if op.Description == "" {
			t.Errorf("operator %s: missing description", op.Type)
		}
	}
}
