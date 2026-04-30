package signal

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Handler receives emitted signals. Implementations might log, send HTTP
// webhooks, write to a channel, or store for later retrieval.
type Handler func(Signal)

// Emitter is the central signal dispatcher with per-category debouncing.
type Emitter struct {
	handlers []Handler
	mu       sync.RWMutex
	logger   *slog.Logger

	// Debounce tracking: last emit time per category.
	debounce     time.Duration
	lastEmit     map[Category]time.Time
	debounceLock sync.Mutex

	// Buffered signals suppressed by debounce.
	suppressed int64
}

// EmitterOption configures an Emitter.
type EmitterOption func(*Emitter)

// WithDebounce sets the minimum interval between signals of the same category.
func WithDebounce(d time.Duration) EmitterOption {
	return func(e *Emitter) { e.debounce = d }
}

// WithLogger sets a structured logger for the emitter.
func WithLogger(l *slog.Logger) EmitterOption {
	return func(e *Emitter) { e.logger = l }
}

// NewEmitter creates a new signal emitter with optional configuration.
func NewEmitter(opts ...EmitterOption) *Emitter {
	e := &Emitter{
		logger:   slog.Default(),
		lastEmit: make(map[Category]time.Time),
		debounce: 0, // no debounce by default
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// On registers a handler for all signals.
func (e *Emitter) On(h Handler) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.handlers = append(e.handlers, h)
}

// Emit sends a signal to all handlers, subject to debounce.
// Returns true if the signal was delivered, false if suppressed.
func (e *Emitter) Emit(s Signal) bool {
	if s.Timestamp.IsZero() {
		s.Timestamp = time.Now()
	}

	if e.debounce > 0 {
		e.debounceLock.Lock()
		last, ok := e.lastEmit[s.Category]
		if ok && time.Since(last) < e.debounce {
			e.suppressed++
			e.debounceLock.Unlock()
			return false
		}
		e.lastEmit[s.Category] = time.Now()
		e.debounceLock.Unlock()
	}

	e.mu.RLock()
	handlers := make([]Handler, len(e.handlers))
	copy(handlers, e.handlers)
	e.mu.RUnlock()

	for _, h := range handlers {
		h(s)
	}
	return true
}

// Suppressed returns the count of signals suppressed by debounce.
func (e *Emitter) Suppressed() int64 {
	e.debounceLock.Lock()
	defer e.debounceLock.Unlock()
	return e.suppressed
}

// Drain blocks until ctx is cancelled, then returns.
// Useful for keeping a background signal listener alive.
func (e *Emitter) Drain(ctx context.Context) {
	<-ctx.Done()
}

// LogHandler returns a Handler that logs signals via slog.
func LogHandler(logger *slog.Logger, brief bool) Handler {
	return func(s Signal) {
		logger.Info(s.Format(brief),
			slog.String("signal_id", s.ID),
			slog.String("severity", s.Severity.String()),
			slog.String("category", string(s.Category)),
		)
	}
}

// CollectorHandler returns a Handler that appends signals to a slice.
// Thread-safe for concurrent emit.
func CollectorHandler() (Handler, func() []Signal) {
	var mu sync.Mutex
	var collected []Signal
	handler := func(s Signal) {
		mu.Lock()
		collected = append(collected, s)
		mu.Unlock()
	}
	getter := func() []Signal {
		mu.Lock()
		defer mu.Unlock()
		out := make([]Signal, len(collected))
		copy(out, collected)
		return out
	}
	return handler, getter
}
