package parallel

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestPoolBasicExecution(t *testing.T) {
	pool := NewPool(WithWorkers(2))

	tasks := []Task{
		{ID: "t1", Name: "task-1", Fn: func(ctx context.Context) error { return nil }},
		{ID: "t2", Name: "task-2", Fn: func(ctx context.Context) error { return nil }},
		{ID: "t3", Name: "task-3", Fn: func(ctx context.Context) error { return nil }},
	}

	results := pool.Run(context.Background(), tasks)
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}

	for _, r := range results {
		if !r.Success {
			t.Errorf("task %s failed: %s", r.TaskID, r.Error)
		}
	}
}

func TestPoolEmptyTasks(t *testing.T) {
	pool := NewPool()
	results := pool.Run(context.Background(), nil)
	if results != nil {
		t.Errorf("empty tasks should return nil, got %v", results)
	}
}

func TestPoolErrorHandling(t *testing.T) {
	pool := NewPool(WithWorkers(2))

	tasks := []Task{
		{ID: "ok", Name: "ok-task", Fn: func(ctx context.Context) error { return nil }},
		{ID: "fail", Name: "fail-task", Fn: func(ctx context.Context) error { return fmt.Errorf("boom") }},
	}

	results := pool.Run(context.Background(), tasks)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	failCount := 0
	for _, r := range results {
		if !r.Success {
			failCount++
			if r.Error != "boom" {
				t.Errorf("Error = %q, want boom", r.Error)
			}
		}
	}
	if failCount != 1 {
		t.Errorf("expected 1 failure, got %d", failCount)
	}
}

func TestPoolContextCancellation(t *testing.T) {
	pool := NewPool(WithWorkers(2))

	ctx, cancel := context.WithCancel(context.Background())
	var started atomic.Int32

	tasks := []Task{
		{ID: "long", Name: "long-task", Fn: func(ctx context.Context) error {
			started.Add(1)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				return nil
			}
		}},
		{ID: "quick", Name: "quick-task", Fn: func(ctx context.Context) error {
			started.Add(1)
			cancel()
			return nil
		}},
	}

	results := pool.Run(ctx, tasks)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
}

func TestPoolConcurrency(t *testing.T) {
	pool := NewPool(WithWorkers(3))
	var maxConcurrent atomic.Int32
	var current atomic.Int32

	tasks := make([]Task, 9)
	for i := range tasks {
		tasks[i] = Task{
			ID:   fmt.Sprintf("c%d", i),
			Name: fmt.Sprintf("concurrent-%d", i),
			Fn: func(ctx context.Context) error {
				c := current.Add(1)
				for {
					old := maxConcurrent.Load()
					if c <= old || maxConcurrent.CompareAndSwap(old, c) {
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
				current.Add(-1)
				return nil
			},
		}
	}

	results := pool.Run(context.Background(), tasks)
	if len(results) != 9 {
		t.Fatalf("got %d results, want 9", len(results))
	}

	mc := maxConcurrent.Load()
	if mc > 3 {
		t.Errorf("max concurrent = %d, want <= 3", mc)
	}
	if mc < 2 {
		t.Logf("max concurrent = %d (possible on slow CI)", mc)
	}
}

func TestPoolContextIsolation(t *testing.T) {
	pool := NewPool(WithWorkers(2))
	var cancelledCount atomic.Int32

	tasks := []Task{
		{ID: "a", Name: "a-task", Fn: func(ctx context.Context) error {
			<-ctx.Done()
			cancelledCount.Add(1)
			return ctx.Err()
		}},
		{ID: "b", Name: "b-task", Fn: func(ctx context.Context) error {
			time.Sleep(5 * time.Millisecond)
			return nil
		}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	results := pool.Run(ctx, tasks)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
}

func TestPoolDurationTracking(t *testing.T) {
	pool := NewPool(WithWorkers(1))

	tasks := []Task{
		{ID: "slow", Name: "slow-task", Fn: func(ctx context.Context) error {
			time.Sleep(20 * time.Millisecond)
			return nil
		}},
	}

	results := pool.Run(context.Background(), tasks)
	if results[0].Duration < 15*time.Millisecond {
		t.Errorf("Duration = %s, want >= 15ms", results[0].Duration)
	}
}

func TestSummary(t *testing.T) {
	results := []TaskResult{
		{TaskID: "1", Success: true, Duration: 100 * time.Millisecond},
		{TaskID: "2", Success: true, Duration: 200 * time.Millisecond},
		{TaskID: "3", Success: false, Duration: 50 * time.Millisecond},
	}

	s := Summary(results)
	if s == "" {
		t.Error("empty summary")
	}
	t.Log(s)
}
