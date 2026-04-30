package plugin

import "context"

// AuthProvider performs target-specific authentication before the demo loop
// starts. Implementations may navigate via CDP, set cookies, autofill from a
// password manager, or hit a backend SSO API.
type AuthProvider interface {
	// Authenticate runs the auth flow. The implementation may use the
	// provided ctx for cancellation. Returns an error if auth fails so the
	// demo can abort early with a clear message.
	Authenticate(ctx context.Context) error
}

// NoopAuthProvider is the default; it does nothing and never errors. Use it
// for public targets (example.com smoke tests) and CI where the test page
// requires no login.
type NoopAuthProvider struct{}

// NewNoopAuthProvider returns the default no-op provider.
func NewNoopAuthProvider() *NoopAuthProvider { return &NoopAuthProvider{} }

// Authenticate is a no-op.
func (NoopAuthProvider) Authenticate(_ context.Context) error { return nil }
