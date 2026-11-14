// Package secretguard is the desk lane's capture and input guard: no
// Screenshot or Snapshot while 1Password or a password field has focus, a
// secret hold that survives restarts, an input allowlist for the workflow's
// own processes, and fail-closed everywhere - a guard that fails open is
// worse than none, so an unreadable foreground also refuses.
package secretguard

import "strings"

// Foreground is one probe of the desktop: the foreground window's process
// and title, and whether the focused element is a password field (UIA
// IsPassword). Readable is false when ANY part could not be read.
type Foreground struct {
	Process           string
	WindowTitle       string
	FocusedIsPassword bool
	Readable          bool
}

// CardEvent is the board card the guard raises on a refusal: the window
// title ONLY - never process arguments, never screen content.
type CardEvent struct {
	Reason string
	Window string
	CardID string
}

// HoldStore is the durable secret-hold state: open(card) arms it, Lift only
// a resolved card disarms it, and AnyOpen reports whether any hold stands.
// Two sources back it in production (a local file and the board card);
// EITHER one saying open keeps the hold closed for capture.
type HoldStore interface {
	Open(cardID string) error
	Lift(cardID string) error
	AnyOpen() bool
}

// Guard decides captures and inputs against the probe and the holds.
type Guard struct {
	// Probe reads the desktop. An error is a refusal (fail closed).
	Probe func() (Foreground, error)
	// Holds is the durable hold state; nil means no holds ever open.
	Holds HoldStore
	// RaiseCard tells the board about a refusal; best effort, never blocks.
	RaiseCard func(CardEvent) error
	// InputAllowlist is the workflow's own process names (Click, Type,
	// Scroll, Shortcut land only on these).
	InputAllowlist []string
	// LogInput is the seam the refusal log rides (nil: no log).
	LogInput func(format string, args ...any)
}

// ErrRefused marks every refusal; the reason names the rule.
type refusalError struct{ reason string }

func (e *refusalError) Error() string { return e.reason }

// Refused reports whether err is a guard refusal.
func Refused(err error) bool {
	_, ok := err.(*refusalError)
	return ok
}

func refuse(reason string) error { return &refusalError{reason: reason} }

// CheckCapture decides one Screenshot or Snapshot request: refused when the
// foreground is unreadable (fail closed), when 1Password is in the
// foreground, when the focused element is a password field, or when any
// secret hold stands. A refusal raises one board card naming the window
// title only.
func (g *Guard) CheckCapture() error {
	f, err := g.Probe()
	if err != nil || !f.Readable {
		return refuse("foreground unreadable: capture refused (fail closed)")
	}
	if isOnePassword(f.Process) {
		g.card("1Password in the foreground", f.WindowTitle)
		return refuse("1Password in the foreground: capture refused")
	}
	if f.FocusedIsPassword {
		g.card("password field focused", f.WindowTitle)
		return refuse("password field focused: capture refused")
	}
	if g.Holds != nil && g.Holds.AnyOpen() {
		return refuse("a secret hold is open: capture refused until the card resolves")
	}
	return nil
}

// CheckInput decides one Click, Type, Scroll or Shortcut: refused when the
// foreground is unreadable, when the process is not on the workflow's
// allowlist, or - on every window - when the shortcut carries the Windows
// key.
func (g *Guard) CheckInput(action string) error {
	f, err := g.Probe()
	if err != nil || !f.Readable {
		return refuse("foreground unreadable: input refused (fail closed)")
	}
	if isWindowsKeyShortcut(action) {
		g.logf("input refused (Windows-key shortcut): %s on %s", action, f.Process)
		return refuse("Windows-key shortcuts are refused on every window")
	}
	if !allowed(g.InputAllowlist, f.Process) {
		g.logf("input refused (process not on the workflow allowlist): %s on %s", action, f.Process)
		return refuse("process " + f.Process + " is not on the workflow allowlist")
	}
	return nil
}

func isOnePassword(proc string) bool {
	p := strings.ToLower(proc)
	return strings.Contains(p, "1password") || p == "op.exe"
}

func isWindowsKeyShortcut(action string) bool {
	a := strings.ToLower(action)
	return strings.Contains(a, "win+") || strings.Contains(a, "meta+") || strings.Contains(a, "super+")
}

func allowed(list []string, proc string) bool {
	for _, p := range list {
		if strings.EqualFold(p, proc) {
			return true
		}
	}
	return false
}

func (g *Guard) card(reason, window string) {
	if g.RaiseCard != nil {
		_ = g.RaiseCard(CardEvent{Reason: reason, Window: window})
	}
}

func (g *Guard) logf(format string, args ...any) {
	if g.LogInput != nil {
		g.LogInput(format, args...)
	}
}
