package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// FairShareConfig controls the per-user fair-share scheduler.
type FairShareConfig struct {
	MaxQueueDepth  int           `yaml:"max_queue_depth"  json:"max_queue_depth"`
	MaxConcurrency int           `yaml:"max_concurrency"  json:"max_concurrency"`
	RequestTimeout time.Duration `yaml:"request_timeout"  json:"request_timeout"`
}

// DefaultFairShareConfig returns conservative defaults.
func DefaultFairShareConfig() FairShareConfig {
	return FairShareConfig{
		MaxQueueDepth:  10,
		MaxConcurrency: 2,
		RequestTimeout: 5 * time.Minute,
	}
}

var (
	// ErrUserQueueFull is returned when a user's queue has reached capacity.
	ErrUserQueueFull = errors.New("user queue full")
	// ErrSchedulerClosed is returned when the scheduler has been shut down.
	ErrSchedulerClosed = errors.New("scheduler closed")
)

type fairShareRequest struct {
	ctx    context.Context
	req    CompletionRequest
	respCh chan *CompletionResponse
	errCh  chan error
	userID string
}

type userQueue struct {
	requests []*fairShareRequest
	mu       sync.Mutex
}

// FairShareScheduler implements per-user FIFO queues with round-robin
// dequeuing, preventing any single user from starving others.
type FairShareScheduler struct {
	cfg      FairShareConfig
	pool     *UpstreamPool
	provider func(node *UpstreamNode) Provider
	logger   *slog.Logger

	queues   map[string]*userQueue
	queueMu  sync.Mutex
	incoming chan *fairShareRequest
	inflight atomic.Int64

	done chan struct{}
	once sync.Once
	wg   sync.WaitGroup
}

// NewFairShareScheduler creates a scheduler backed by an upstream pool.
// providerFactory creates an LLM Provider from a selected upstream node.
func NewFairShareScheduler(cfg FairShareConfig, pool *UpstreamPool,
	providerFactory func(node *UpstreamNode) Provider, logger *slog.Logger,
) *FairShareScheduler {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.MaxQueueDepth <= 0 {
		cfg.MaxQueueDepth = 10
	}
	if cfg.MaxConcurrency <= 0 {
		cfg.MaxConcurrency = 2
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 5 * time.Minute
	}

	s := &FairShareScheduler{
		cfg:      cfg,
		pool:     pool,
		provider: providerFactory,
		logger:   logger,
		queues:   make(map[string]*userQueue),
		incoming: make(chan *fairShareRequest, cfg.MaxQueueDepth*10),
		done:     make(chan struct{}),
	}

	s.wg.Add(1)
	go s.schedulerLoop()

	return s
}

// Submit enqueues a request for fair-share scheduling.
func (s *FairShareScheduler) Submit(ctx context.Context, userID string, req CompletionRequest) (*CompletionResponse, error) {
	select {
	case <-s.done:
		return nil, ErrSchedulerClosed
	default:
	}

	if userID == "" {
		userID = "_anonymous"
	}

	s.queueMu.Lock()
	q, ok := s.queues[userID]
	if !ok {
		q = &userQueue{}
		s.queues[userID] = q
	}
	s.queueMu.Unlock()

	q.mu.Lock()
	if len(q.requests) >= s.cfg.MaxQueueDepth {
		q.mu.Unlock()
		return nil, fmt.Errorf("%w: user %s has %d pending", ErrUserQueueFull, userID, s.cfg.MaxQueueDepth)
	}

	fsr := &fairShareRequest{
		ctx:    ctx,
		req:    req,
		respCh: make(chan *CompletionResponse, 1),
		errCh:  make(chan error, 1),
		userID: userID,
	}
	q.requests = append(q.requests, fsr)
	q.mu.Unlock()

	select {
	case s.incoming <- fsr:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.done:
		return nil, ErrSchedulerClosed
	}

	select {
	case resp := <-fsr.respCh:
		return resp, nil
	case err := <-fsr.errCh:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.done:
		return nil, ErrSchedulerClosed
	}
}

func (s *FairShareScheduler) schedulerLoop() {
	defer s.wg.Done()

	sem := make(chan struct{}, s.cfg.MaxConcurrency)

	for {
		select {
		case <-s.done:
			return
		case fsr, ok := <-s.incoming:
			if !ok {
				return
			}
			select {
			case sem <- struct{}{}:
			case <-s.done:
				fsr.errCh <- ErrSchedulerClosed
				return
			case <-fsr.ctx.Done():
				fsr.errCh <- fsr.ctx.Err()
				continue
			}

			s.inflight.Add(1)
			go func(r *fairShareRequest) {
				defer func() {
					<-sem
					s.inflight.Add(-1)
					s.removeFromQueue(r)
				}()
				s.processRequest(r)
			}(fsr)
		}
	}
}

func (s *FairShareScheduler) processRequest(fsr *fairShareRequest) {
	ctx := fsr.ctx
	if s.cfg.RequestTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.cfg.RequestTimeout)
		defer cancel()
	}

	tier := extractTierFromRequest(fsr.req)
	var node *UpstreamNode
	if tier != "" {
		node = s.pool.SelectByTier(tier)
	}
	if node == nil {
		node = s.pool.SelectAny()
	}
	if node == nil {
		fsr.errCh <- ErrNoHealthyUpstream
		return
	}

	provider := s.provider(node)
	resp, err := provider.Complete(ctx, fsr.req)
	if err != nil {
		s.pool.MarkUnhealthy(node)
		fsr.errCh <- fmt.Errorf("upstream %s: %w", node.Name, err)
		return
	}

	s.pool.MarkHealthy(node)
	fsr.respCh <- resp
}

func (s *FairShareScheduler) removeFromQueue(fsr *fairShareRequest) {
	s.queueMu.Lock()
	q, ok := s.queues[fsr.userID]
	s.queueMu.Unlock()
	if !ok {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, r := range q.requests {
		if r == fsr {
			q.requests = append(q.requests[:i], q.requests[i+1:]...)
			return
		}
	}
}

// Inflight returns the number of currently processing requests.
func (s *FairShareScheduler) Inflight() int64 {
	return s.inflight.Load()
}

// QueueDepth returns the total number of pending requests across all users.
func (s *FairShareScheduler) QueueDepth() int {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	total := 0
	for _, q := range s.queues {
		q.mu.Lock()
		total += len(q.requests)
		q.mu.Unlock()
	}
	return total
}

// Close shuts down the scheduler.
func (s *FairShareScheduler) Close() {
	s.once.Do(func() {
		close(s.done)
	})
	s.wg.Wait()
}

func extractTierFromRequest(req CompletionRequest) string {
	for _, m := range req.Messages {
		if m.Role == "system" {
			lower := m.Content
			for _, tier := range []string{"agent", "fast", "utility", "heavy", "powerful"} {
				if containsAny(lower, "tier:"+tier, "x-tier:"+tier) {
					return tier
				}
			}
		}
	}
	return ""
}
