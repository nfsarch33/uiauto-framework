package parallel

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func BenchmarkPoolUnderLoad(b *testing.B) {
	for _, workers := range []int{4, 8, 16} {
		b.Run(fmt.Sprintf("workers=%d", workers), func(b *testing.B) {
			pool := NewPool(WithWorkers(workers))
			tasks := make([]Task, 1000)
			for i := range tasks {
				idx := i
				tasks[i] = Task{
					ID:   fmt.Sprintf("t%d", idx),
					Name: fmt.Sprintf("task-%d", idx),
					Fn: func(ctx context.Context) error {
						time.Sleep(100 * time.Microsecond)
						return nil
					},
				}
			}

			b.ResetTimer()
			for range b.N {
				results := pool.Run(context.Background(), tasks)
				if len(results) != 1000 {
					b.Fatalf("got %d results, want 1000", len(results))
				}
			}
		})
	}
}

func BenchmarkPoolSmallBatch(b *testing.B) {
	pool := NewPool(WithWorkers(4))
	tasks := make([]Task, 10)
	for i := range tasks {
		idx := i
		tasks[i] = Task{
			ID:   fmt.Sprintf("t%d", idx),
			Name: fmt.Sprintf("small-%d", idx),
			Fn:   func(ctx context.Context) error { return nil },
		}
	}

	b.ResetTimer()
	for range b.N {
		pool.Run(context.Background(), tasks)
	}
}

func BenchmarkPoolSingleWorker(b *testing.B) {
	pool := NewPool(WithWorkers(1))
	tasks := make([]Task, 100)
	for i := range tasks {
		idx := i
		tasks[i] = Task{
			ID:   fmt.Sprintf("t%d", idx),
			Name: fmt.Sprintf("serial-%d", idx),
			Fn: func(ctx context.Context) error {
				time.Sleep(10 * time.Microsecond)
				return nil
			},
		}
	}

	b.ResetTimer()
	for range b.N {
		pool.Run(context.Background(), tasks)
	}
}
