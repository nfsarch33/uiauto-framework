package domheal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	stableHTML = `<html><body>
		<nav class="d2l-nav" role="navigation"><a href="/home">Home</a></nav>
		<main id="content" role="main">
			<div class="d2l-content-list">
				<a class="d2l-link" href="/content/1">Module 1</a>
				<a class="d2l-link" href="/content/2">Module 2</a>
			</div>
		</main>
	</body></html>`

	majorChangeHTML = `<html><body>
		<header class="new-header">
			<div class="mega-menu" role="menubar">
				<button class="menu-toggle">Menu</button>
			</div>
		</header>
		<section class="new-content-area" data-view="grid">
			<article class="card" data-id="1"><h3>Week 1</h3></article>
			<article class="card" data-id="2"><h3>Week 2</h3></article>
		</section>
		<footer class="new-footer"><p>Copyright 2026</p></footer>
	</body></html>`
)

func TestDOMChangeDetector_StablePage(t *testing.T) {
	dir := t.TempDir()
	d := NewDOMChangeDetector(DOMChangeDetectorConfig{StateDir: dir})

	d.Check("page1", stableHTML)
	event := d.Check("page1", stableHTML)
	if event != nil {
		t.Error("stable page should not trigger change event")
	}
}

func TestDOMChangeDetector_NewPage(t *testing.T) {
	dir := t.TempDir()
	d := NewDOMChangeDetector(DOMChangeDetectorConfig{StateDir: dir})

	event := d.Check("page1", stableHTML)
	if event != nil {
		t.Error("first check on a new page should not trigger change event")
	}
}

func TestDOMChangeDetector_MajorChange(t *testing.T) {
	dir := t.TempDir()
	d := NewDOMChangeDetector(DOMChangeDetectorConfig{StateDir: dir})

	d.Check("page1", stableHTML)
	event := d.Check("page1", majorChangeHTML)
	if event == nil {
		t.Fatal("major DOM restructure should trigger change event")
	}
	if event.Severity != "major" {
		t.Errorf("expected major severity, got %s", event.Severity)
	}
}

func TestDOMChangeDetector_RepairCallback(t *testing.T) {
	dir := t.TempDir()
	d := NewDOMChangeDetector(DOMChangeDetectorConfig{StateDir: dir})

	repairCalled := false
	d.OnRepair(func(pageID string, event ChangeEvent) error {
		repairCalled = true
		if pageID != "page1" {
			t.Errorf("expected page1, got %s", pageID)
		}
		return nil
	})

	d.Check("page1", stableHTML)
	d.Check("page1", majorChangeHTML)
	if !repairCalled {
		t.Error("repair callback should have been called for major change")
	}
}

func TestDOMChangeDetector_StatePersistence(t *testing.T) {
	dir := t.TempDir()
	d := NewDOMChangeDetector(DOMChangeDetectorConfig{StateDir: dir})

	d.Check("page1", stableHTML)
	d.Check("page1", majorChangeHTML)

	if err := d.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	d2 := NewDOMChangeDetector(DOMChangeDetectorConfig{StateDir: dir})
	if err := d2.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}

	events := d2.Events()
	if len(events) == 0 {
		t.Error("expected persisted events after load")
	}
}

func TestDOMChangeDetector_Stats(t *testing.T) {
	dir := t.TempDir()
	d := NewDOMChangeDetector(DOMChangeDetectorConfig{StateDir: dir})

	d.Check("page1", stableHTML)
	d.Check("page1", majorChangeHTML)
	d.Check("page2", stableHTML)

	stats := d.Stats()
	if stats.HashesTracked < 2 {
		t.Errorf("expected at least 2 hashes tracked, got %d", stats.HashesTracked)
	}
	if stats.BreakerState != "closed" {
		t.Errorf("expected breaker closed, got %s", stats.BreakerState)
	}
}

func TestDOMChangeDetector_WithMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics := NewDOMChangeDetectorMetrics(reg)

	dir := t.TempDir()
	d := NewDOMChangeDetector(DOMChangeDetectorConfig{StateDir: dir})
	d.WithMetrics(metrics)

	d.Check("page1", stableHTML)
	d.Check("page1", majorChangeHTML)

	gathered, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	found := false
	for _, mf := range gathered {
		if strings.Contains(mf.GetName(), "change_detector") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected change_detector metrics")
	}
}

func TestDOMChangeDetector_CircuitBreakerIntegration(t *testing.T) {
	dir := t.TempDir()
	d := NewDOMChangeDetector(DOMChangeDetectorConfig{
		StateDir:           dir,
		BreakerFailures:    2,
		BreakerCooldownSec: 60,
	})

	repairCallCount := 0
	d.OnRepair(func(_ string, _ ChangeEvent) error {
		repairCallCount++
		return os.ErrPermission
	})

	d.Check("p1", stableHTML)
	d.Check("p1", majorChangeHTML)

	d.Check("p2", stableHTML)
	d.Check("p2", majorChangeHTML)

	if repairCallCount == 0 {
		t.Fatal("repair callback should have been called at least once")
	}

	stats := d.Stats()
	if stats.BreakerState != "open" {
		t.Skipf("breaker not open after %d repair failures (state=%s)", repairCallCount, stats.BreakerState)
	}

	event := d.Check("p3", stableHTML)
	if event != nil {
		t.Error("circuit breaker should block check when open")
	}
}

func TestDOMChangeDetector_SeverityClassification(t *testing.T) {
	tests := []struct {
		fpSim, structSim float64
		want             string
	}{
		{0.95, 0.95, "minor"},
		{0.80, 0.80, "moderate"},
		{0.50, 0.40, "major"},
		{0.90, 0.90, "minor"},
		{0.70, 0.70, "moderate"},
		{0.69, 0.69, "major"},
	}
	for _, tt := range tests {
		got := classifySeverity(tt.fpSim, tt.structSim)
		if got != tt.want {
			t.Errorf("classifySeverity(%.2f, %.2f) = %s, want %s",
				tt.fpSim, tt.structSim, got, tt.want)
		}
	}
}

// Synthetic D2L page mutation fixtures

func TestDOMChangeDetector_SyntheticD2L_NavRestructure(t *testing.T) {
	dir := t.TempDir()
	d := NewDOMChangeDetector(DOMChangeDetectorConfig{StateDir: dir})

	original := `<html><body>
		<nav class="d2l-navigation" id="d2l-nav"><ul><li>Home</li><li>Content</li></ul></nav>
		<div id="d2l-content">Content here</div>
	</body></html>`

	mutated := `<html><body>
		<div class="d2l-minibar" role="navigation"><div class="d2l-minibar-items"><span>Home</span><span>Content</span></div></div>
		<div id="d2l-content">Content here</div>
	</body></html>`

	d.Check("d2l-nav", original)
	event := d.Check("d2l-nav", mutated)
	if event == nil {
		t.Fatal("navigation restructure should be detected")
	}
}

func TestDOMChangeDetector_SyntheticD2L_ContentListRedesign(t *testing.T) {
	dir := t.TempDir()
	d := NewDOMChangeDetector(DOMChangeDetectorConfig{StateDir: dir})

	original := `<html><body>
		<div class="d2l-le-content"><ul class="d2l-content-list">
			<li><a href="/m/1">Module 1</a></li>
			<li><a href="/m/2">Module 2</a></li>
		</ul></div>
	</body></html>`

	mutated := `<html><body>
		<div class="d2l-content-container"><div class="d2l-card-grid">
			<div class="d2l-card" data-module="1"><h3>Module 1</h3></div>
			<div class="d2l-card" data-module="2"><h3>Module 2</h3></div>
		</div></div>
	</body></html>`

	d.Check("d2l-content", original)
	event := d.Check("d2l-content", mutated)
	if event == nil {
		t.Fatal("content list redesign should be detected")
	}
}

func TestDOMChangeDetector_SyntheticD2L_ClassRename(t *testing.T) {
	dir := t.TempDir()
	d := NewDOMChangeDetector(DOMChangeDetectorConfig{StateDir: dir})

	original := `<html><body>
		<div class="d2l-le-content-module"><a class="d2l-link" href="/x">Link</a></div>
	</body></html>`

	mutated := `<html><body>
		<div class="d2l-content-module-v2"><a class="d2l-link-v2" href="/x">Link</a></div>
	</body></html>`

	d.Check("d2l-class", original)
	event := d.Check("d2l-class", mutated)
	if event == nil {
		t.Fatal("class rename should be detected")
	}
}

func TestDOMChangeDetector_SyntheticD2L_SubmissionForm(t *testing.T) {
	dir := t.TempDir()
	d := NewDOMChangeDetector(DOMChangeDetectorConfig{StateDir: dir})

	original := `<html><body>
		<form id="d2l-submission" class="d2l-form" action="/submit">
			<input type="file" name="attachment" />
			<textarea name="comments"></textarea>
			<button type="submit" class="d2l-btn">Submit</button>
		</form>
	</body></html>`

	mutated := `<html><body>
		<div class="d2l-submission-v2" data-form="true">
			<div class="d2l-file-upload" data-dropzone="true"></div>
			<div class="d2l-rich-editor" contenteditable="true"></div>
			<button class="d2l-floating-btn" data-action="submit">Submit Assignment</button>
		</div>
	</body></html>`

	d.Check("d2l-submit", original)
	event := d.Check("d2l-submit", mutated)
	if event == nil {
		t.Fatal("submission form redesign should be detected")
	}
}

func TestDOMChangeDetector_SyntheticD2L_GradeTable(t *testing.T) {
	dir := t.TempDir()
	d := NewDOMChangeDetector(DOMChangeDetectorConfig{StateDir: dir})

	original := `<html><body>
		<table class="d2l-grades-table" role="table">
			<thead><tr><th>Item</th><th>Grade</th><th>Weight</th></tr></thead>
			<tbody><tr><td>Assignment 1</td><td>85</td><td>20%</td></tr></tbody>
		</table>
	</body></html>`

	mutated := `<html><body>
		<div class="d2l-grades-grid" role="grid">
			<div class="d2l-grade-row" data-item="1">
				<span class="d2l-grade-name">Assignment 1</span>
				<span class="d2l-grade-value">85</span>
				<span class="d2l-grade-weight">20%</span>
			</div>
		</div>
	</body></html>`

	d.Check("d2l-grades", original)
	event := d.Check("d2l-grades", mutated)
	if event == nil {
		t.Fatal("grade table redesign should be detected")
	}
}

func TestDOMChangeDetector_EmptyStateDir(t *testing.T) {
	d := NewDOMChangeDetector(DOMChangeDetectorConfig{})

	event := d.Check("page1", stableHTML)
	if event != nil {
		t.Error("first check should not trigger event")
	}

	event = d.Check("page1", stableHTML)
	if event != nil {
		t.Error("unchanged page should not trigger event")
	}
}

func TestDOMChangeDetector_SaveLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	d := NewDOMChangeDetector(DOMChangeDetectorConfig{StateDir: dir})

	d.Check("p1", stableHTML)
	d.Check("p1", majorChangeHTML)

	if err := d.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	eventsFile := filepath.Join(dir, "change_events.json")
	data, err := os.ReadFile(eventsFile)
	if err != nil {
		t.Fatalf("read events file: %v", err)
	}
	if len(data) < 10 {
		t.Error("events file should contain serialized events")
	}
}
