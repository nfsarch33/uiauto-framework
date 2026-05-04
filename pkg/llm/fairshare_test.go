package llm

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// funcProvider adapts a function into the Provider interface for tests.
type funcProvider struct {
	fn func(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
}

func (f *funcProvider) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	return f.fn(ctx, req)
}

func mockProviderFactory(latency time.Duration) func(*UpstreamNode) Provider {
	return func(_ *UpstreamNode) Provider {
		return &funcProvider{fn: func(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
			if latency > 0 {
				select {
				case <-time.After(latency):
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			return &CompletionResponse{
				Choices: []Choice{{Message: Message{Role: "assistant", Content: "ok"}}},
			}, nil
		}}
	}
}

func testScheduler(nodes []*UpstreamNode, factory func(*UpstreamNode) Provider) *FairShareScheduler {
	pool := noopPool(nodes)
	return NewFairShareScheduler(DefaultFairShareConfig(), pool, factory, slog.Default())
}

func TestFairShare_SingleUser_Success(t *testing.T) {
	nodes := testNodes()
	s := testScheduler(nodes, mockProviderFactory(0))
	defer s.Close()

	resp, err := s.Submit(context.Background(), "user-1", CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "ok", resp.Choices[0].Message.Content)
}

func TestFairShare_AnonymousUser(t *testing.T) {
	nodes := testNodes()
	s := testScheduler(nodes, mockProviderFactory(0))
	defer s.Close()

	resp, err := s.Submit(context.Background(), "", CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestFairShare_QueueFull(t *testing.T) {
	nodes := testNodes()
	slow := mockProviderFactory(5 * time.Second)
	cfg := FairShareConfig{MaxQueueDepth: 1, MaxConcurrency: 1, RequestTimeout: 10 * time.Second}
	pool := noopPool(nodes)
	s := NewFairShareScheduler(cfg, pool, slow, slog.Default())
	defer s.Close()

	ctx := context.Background()
	go func() {
		_, _ = s.Submit(ctx, "user-flood", CompletionRequest{
			Messages: []Message{{Role: "user", Content: "slow-1"}},
		})
	}()
	time.Sleep(50 * time.Millisecond)

	_, err := s.Submit(ctx, "user-flood", CompletionRequest{
		Messages: []Message{{Role: "user", Content: "slow-2"}},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUserQueueFull)
}

func TestFairShare_ConcurrentUsers_Fair(t *testing.T) {
	nodes := testNodes()
	var completed atomic.Int64
	factory := func(_ *UpstreamNode) Provider {
		return &funcProvider{fn: func(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
			completed.Add(1)
			return &CompletionResponse{
				Choices: []Choice{{Message: Message{Role: "assistant", Content: "done"}}},
			}, nil
		}}
	}

	cfg := FairShareConfig{MaxQueueDepth: 10, MaxConcurrency: 4, RequestTimeout: 5 * time.Second}
	pool := noopPool(nodes)
	s := NewFairShareScheduler(cfg, pool, factory, slog.Default())
	defer s.Close()

	const users = 5
	const reqsPerUser = 4
	var wg sync.WaitGroup
	for u := range users {
		for range reqsPerUser {
			wg.Add(1)
			go func(userID string) {
				defer wg.Done()
				_, err := s.Submit(context.Background(), userID, CompletionRequest{
					Messages: []Message{{Role: "user", Content: "test"}},
				})
				assert.NoError(t, err)
			}(string(rune('A' + u)))
		}
	}
	wg.Wait()
	assert.Equal(t, int64(users*reqsPerUser), completed.Load())
}

func TestFairShare_ContextCancellation(t *testing.T) {
	nodes := testNodes()
	slow := mockProviderFactory(10 * time.Second)
	s := testScheduler(nodes, slow)
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := s.Submit(ctx, "user-cancel", CompletionRequest{
		Messages: []Message{{Role: "user", Content: "slow"}},
	})
	require.Error(t, err)
}

func TestFairShare_ClosedScheduler(t *testing.T) {
	nodes := testNodes()
	s := testScheduler(nodes, mockProviderFactory(0))
	s.Close()

	_, err := s.Submit(context.Background(), "user-1", CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSchedulerClosed)
}

func TestFairShare_Inflight(t *testing.T) {
	nodes := testNodes()
	block := make(chan struct{})
	factory := func(_ *UpstreamNode) Provider {
		return &funcProvider{fn: func(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
			<-block
			return &CompletionResponse{
				Choices: []Choice{{Message: Message{Role: "assistant", Content: "ok"}}},
			}, nil
		}}
	}

	cfg := FairShareConfig{MaxQueueDepth: 10, MaxConcurrency: 2, RequestTimeout: 5 * time.Second}
	pool := noopPool(nodes)
	s := NewFairShareScheduler(cfg, pool, factory, slog.Default())
	defer s.Close()

	go func() {
		_, _ = s.Submit(context.Background(), "u1", CompletionRequest{Messages: []Message{{Role: "user", Content: "a"}}})
	}()
	go func() {
		_, _ = s.Submit(context.Background(), "u2", CompletionRequest{Messages: []Message{{Role: "user", Content: "b"}}})
	}()
	time.Sleep(100 * time.Millisecond)

	assert.LessOrEqual(t, s.Inflight(), int64(2))
	close(block)
}
