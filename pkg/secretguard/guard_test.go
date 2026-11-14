package secretguard

import (
	"errors"
	"strings"
	"testing"
)

func probeWith(f Foreground) func() (Foreground, error) {
	return func() (Foreground, error) { return f, nil }
}

func okFG() Foreground {
	return Foreground{Process: "ZCode.exe", WindowTitle: "work", Readable: true}
}

type memHolds struct {
	open map[string]bool
}

func (m *memHolds) Open(id string) error {
	if m.open == nil {
		m.open = map[string]bool{}
	}
	m.open[id] = true
	return nil
}
func (m *memHolds) Lift(id string) error { delete(m.open, id); return nil }
func (m *memHolds) AnyOpen() bool {
	for _, v := range m.open {
		if v {
			return true
		}
	}
	return false
}

var lastCard CardEvent

func cardSink() func(CardEvent) error {
	return func(c CardEvent) error { lastCard = c; return nil }
}

// Scenario: a password field blocks capture - refused, and the card names
// the window title only.
func TestPasswordFieldBlocksCapture(t *testing.T) {
	g := &Guard{Probe: probeWith(Foreground{Process: "ZCode.exe", WindowTitle: "checkout", FocusedIsPassword: true, Readable: true}), RaiseCard: cardSink()}
	if err := g.CheckCapture(); !Refused(err) {
		t.Fatalf("capture must be refused on a password field: %v", err)
	}
	if lastCard.Window != "checkout" {
		t.Fatalf("the card names the window title: %+v", lastCard)
	}
	if strings.Contains(lastCard.Window, "password") == false && lastCard.Reason == "" {
		t.Fatal("the card carries a reason")
	}
}

// Scenario: 1Password blocks capture the same way.
func TestOnePasswordBlocksCapture(t *testing.T) {
	for _, proc := range []string{"1Password.exe", "1password.app", "op.exe"} {
		g := &Guard{Probe: probeWith(Foreground{Process: proc, WindowTitle: "vault", Readable: true}), RaiseCard: cardSink()}
		if err := g.CheckCapture(); !Refused(err) {
			t.Fatalf("capture must be refused with foreground %s: %v", proc, err)
		}
	}
}

// Scenario: a secret hold blocks capture until the card resolves.
func TestSecretHoldBlocksCapture(t *testing.T) {
	h := &memHolds{}
	g := &Guard{Probe: probeWith(okFG()), Holds: h, RaiseCard: cardSink()}
	if err := g.CheckCapture(); err != nil {
		t.Fatalf("no hold open: capture is allowed, got %v", err)
	}
	_ = h.Open("card-7")
	if err := g.CheckCapture(); !Refused(err) {
		t.Fatal("an open hold refuses every capture, even with nothing secret in focus")
	}
	_ = h.Lift("card-7")
	if err := g.CheckCapture(); err != nil {
		t.Fatalf("a resolved card lifts the hold: %v", err)
	}
}

// Scenario: input outside the workflow's windows is refused; a Windows-key
// shortcut is refused on EVERY window.
func TestInputAllowlistAndWindowsKey(t *testing.T) {
	g := &Guard{Probe: probeWith(okFG()), InputAllowlist: []string{"ZCode.exe"}}
	for _, hostile := range []string{"cmd.exe", "powershell.exe", "WindowsTerminal.exe", "regedit.exe", "ApplicationFrameHost.exe"} {
		g2 := &Guard{Probe: probeWith(Foreground{Process: hostile, WindowTitle: "run", Readable: true}), InputAllowlist: []string{"ZCode.exe"}}
		if err := g2.CheckInput("Click"); !Refused(err) {
			t.Fatalf("input on %s must be refused", hostile)
		}
	}
	if err := g.CheckInput("Type"); err != nil {
		t.Fatalf("input on the allowlisted process is allowed: %v", err)
	}
	for _, sc := range []string{"Win+R", "Win+X", "Meta+L"} {
		if err := g.CheckInput("Shortcut:" + sc); !Refused(err) {
			t.Fatalf("%s must be refused on every window", sc)
		}
	}
}

// Scenario: fail closed - an unreadable foreground refuses capture AND input.
func TestFailClosed(t *testing.T) {
	g := &Guard{Probe: probeWith(Foreground{Readable: false}), InputAllowlist: []string{"ZCode.exe"}}
	if err := g.CheckCapture(); !Refused(err) {
		t.Fatal("an unreadable foreground refuses capture")
	}
	if err := g.CheckInput("Click"); !Refused(err) {
		t.Fatal("an unreadable foreground refuses input")
	}
	g2 := &Guard{Probe: func() (Foreground, error) { return Foreground{}, errors.New("uia: timeout") }}
	if err := g2.CheckCapture(); !Refused(err) {
		t.Fatal("a probe error refuses capture")
	}
}

// Scenario: the hold survives a restart - the file-backed store reopens
// with its holds intact.
func TestFileHoldsSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	h1, err := OpenFileHolds(dir + "/holds.json")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := h1.Open("card-9"); err != nil {
		t.Fatalf("open hold: %v", err)
	}
	h2, err := OpenFileHolds(dir + "/holds.json")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if !h2.AnyOpen() {
		t.Fatal("the hold must survive the reopen (a restart must not lift it)")
	}
	if err := h2.Lift("card-9"); err != nil {
		t.Fatalf("lift: %v", err)
	}
	if h2.AnyOpen() {
		t.Fatal("only resolving the card lifts it")
	}
}
