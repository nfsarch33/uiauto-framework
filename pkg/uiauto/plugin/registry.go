// Package plugin defines extension seams that turn the uiauto framework into
// a generic library suitable for extraction into a public repo. Each interface
// has a default no-op or built-in implementation so callers that don't need
// the seam keep working unchanged.
//
// Four seams live here:
//
//   - ActionRegistry: register custom action types beyond the built-in
//     click/type/evaluate/read/wait/verify/frame set.
//   - ScenarioLoader: parse NL scenarios from any source (JSON, YAML, ZBT
//     spec, Playwright TC).
//   - AuthProvider: perform target-specific auth (OAuth, API keys, password
//     manager autofill via CDP, SSO redirects, etc.) before the demo loop runs.
//   - VisualVerifier: pluggable visual scoring (OmniParser, GPT-4V, custom).
package plugin

import (
	"context"
	"fmt"
	"sync"
)

// ActionHandler executes a single user-defined action type. The selector and
// value mirror the built-in semantics; implementations may ignore them.
type ActionHandler func(ctx context.Context, selector, value string) error

// ActionRegistry maps action type names to their handlers.
type ActionRegistry struct {
	mu       sync.RWMutex
	handlers map[string]ActionHandler
}

// NewActionRegistry returns an empty registry. Built-in action types
// (click/type/evaluate/read/wait/verify/frame) are NOT pre-registered here
// because they're implemented inside LightExecutor; this registry is only
// for custom extensions added by downstream consumers.
func NewActionRegistry() *ActionRegistry {
	return &ActionRegistry{handlers: map[string]ActionHandler{}}
}

// Register adds or replaces a handler for the named action type.
func (r *ActionRegistry) Register(name string, handler ActionHandler) error {
	if name == "" {
		return fmt.Errorf("action name must not be empty")
	}
	if handler == nil {
		return fmt.Errorf("action %q: handler must not be nil", name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[name] = handler
	return nil
}

// Get returns the handler for name and whether it was found.
func (r *ActionRegistry) Get(name string) (ActionHandler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[name]
	return h, ok
}

// Names returns the registered action type names sorted alphabetically.
func (r *ActionRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.handlers))
	for k := range r.handlers {
		out = append(out, k)
	}
	return out
}
