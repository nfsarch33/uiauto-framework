package uiauto

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// verifyMockBrowser implements just enough of the Browser interface to test
// the verify and frame action paths without spinning up Chrome.
type verifyMockBrowser struct {
	verifyResult        bool
	verifyErr           error
	verifyCalls         int32
	switchCalls         int32
	switchSelectorSeen  string
	switchReleaseCalled int32
	clickCalls          int32
}

func (m *verifyMockBrowser) Navigate(url string) error                         { return nil }
func (m *verifyMockBrowser) NavigateWithConfig(url string, _ WaitConfig) error { return nil }
func (m *verifyMockBrowser) CaptureDOM() (string, error)                       { return "", nil }
func (m *verifyMockBrowser) CaptureScreenshot() ([]byte, error)                { return []byte("png"), nil }
func (m *verifyMockBrowser) Click(sel string) error {
	atomic.AddInt32(&m.clickCalls, 1)
	return nil
}
func (m *verifyMockBrowser) Type(sel, text string) error                 { return nil }
func (m *verifyMockBrowser) Evaluate(expr string, res interface{}) error { return nil }
func (m *verifyMockBrowser) IsVisible(selector string) (bool, error) {
	atomic.AddInt32(&m.verifyCalls, 1)
	return m.verifyResult, m.verifyErr
}
func (m *verifyMockBrowser) SwitchToFrame(selector string) (func(), error) {
	atomic.AddInt32(&m.switchCalls, 1)
	m.switchSelectorSeen = selector
	return func() { atomic.AddInt32(&m.switchReleaseCalled, 1) }, nil
}
func (m *verifyMockBrowser) Close() {}

// seedTracker creates a PatternTracker with a single pattern for testing.
func seedTracker(t *testing.T, id, selector string) *PatternTracker {
	t.Helper()
	dir := t.TempDir()
	tracker, err := NewPatternTracker(filepath.Join(dir, "patterns.json"), dir)
	if err != nil {
		t.Fatalf("create tracker: %v", err)
	}
	if err := tracker.store.Set(context.Background(), UIPattern{
		ID:         id,
		Selector:   selector,
		Confidence: 0.95,
		LastSeen:   time.Now(),
	}); err != nil {
		t.Fatalf("seed pattern: %v", err)
	}
	return tracker
}

// --- TDD: verify action ---

func TestExecute_VerifyAction_PassesWhenElementVisible(t *testing.T) {
	tracker := seedTracker(t, "home", "#home")
	mb := &verifyMockBrowser{verifyResult: true}
	exec := NewLightExecutor(tracker, mb)

	err := exec.Execute(context.Background(), Action{Type: "verify", TargetID: "home"})
	if err != nil {
		t.Fatalf("verify should pass when element visible: %v", err)
	}
	if atomic.LoadInt32(&mb.verifyCalls) == 0 {
		t.Error("expected IsVisible to be called")
	}
}

func TestExecute_VerifyAction_FailsWhenElementMissing(t *testing.T) {
	tracker := seedTracker(t, "ghost", "#ghost")
	mb := &verifyMockBrowser{verifyResult: false}
	exec := NewLightExecutor(tracker, mb)

	err := exec.Execute(context.Background(), Action{Type: "verify", TargetID: "ghost"})
	if err == nil {
		t.Fatal("verify should fail when element is not visible")
	}
}

func TestExecute_VerifyAction_PropagatesBrowserError(t *testing.T) {
	tracker := seedTracker(t, "oops", "#x")
	mb := &verifyMockBrowser{verifyErr: errors.New("network down")}
	exec := NewLightExecutor(tracker, mb)

	err := exec.Execute(context.Background(), Action{Type: "verify", TargetID: "oops"})
	if err == nil {
		t.Fatal("verify should propagate browser error")
	}
}

// --- TDD: frame action ---

func TestExecute_FrameSwitchAction_DelegatesToBrowserAgent(t *testing.T) {
	tracker := seedTracker(t, "embedded", "iframe#embedded-widget")
	mb := &verifyMockBrowser{}
	exec := NewLightExecutor(tracker, mb)

	err := exec.Execute(context.Background(), Action{Type: "frame", TargetID: "embedded"})
	if err != nil {
		t.Fatalf("frame should not error on success: %v", err)
	}
	if atomic.LoadInt32(&mb.switchCalls) != 1 {
		t.Errorf("expected SwitchToFrame to be called once, got %d", atomic.LoadInt32(&mb.switchCalls))
	}
	if mb.switchSelectorSeen != "iframe#embedded-widget" {
		t.Errorf("expected selector iframe#embedded-widget, got %q", mb.switchSelectorSeen)
	}
	if atomic.LoadInt32(&mb.switchReleaseCalled) != 1 {
		t.Errorf("expected release to be called once via defer, got %d", atomic.LoadInt32(&mb.switchReleaseCalled))
	}
}

// --- TDD: deadline propagation ---

func TestExecute_VerifyAction_RespectsContextDeadline(t *testing.T) {
	tracker := seedTracker(t, "slow", "#slow")
	mb := &verifyMockBrowser{verifyResult: true}
	exec := NewLightExecutor(tracker, mb)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := exec.Execute(ctx, Action{Type: "verify", TargetID: "slow"})
	// Even with a short deadline, the mock returns immediately, so the result
	// should be PASS. This guards against context plumbing regressions.
	if err != nil {
		t.Fatalf("expected pass with mock under tight deadline: %v", err)
	}
}
