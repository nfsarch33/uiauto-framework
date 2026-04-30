package parallel

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Task represents a unit of work to execute in the pool.
type Task struct {
	ID   string
	Name string
	Fn   func(ctx context.Context) error
}

// TaskResult captures the outcome of a task execution.
type TaskResult struct {
	TaskID   string        `json:"task_id"`
	Name     string        `json:"name"`
	Success  bool          `json:"success"`
	Error    string        `json:"error,omitempty"`
	Duration time.Duration `json:"duration"`
}

// Pool manages a fixed-size goroutine pool for parallel task execution.
// Each task gets an isolated context derived from the pool's parent context.
type Pool struct {
	workers int
	logger  *slog.Logger
}

// PoolOption configures the pool.
type PoolOption func(*Pool)

// WithWorkers sets the number of concurrent workers.
func WithWorkers(n int) PoolOption {
	return func(p *Pool) {
		if n > 0 {
			p.workers = n
		}
	}
}

// WithPoolLogger sets a structured logger for the pool.
func WithPoolLogger(l *slog.Logger) PoolOption {
	return func(p *Pool) { p.logger = l }
}

// NewPool creates a new goroutine pool.
func NewPool(opts ...PoolOption) *Pool {
	p := &Pool{
		workers: 4,
		logger:  slog.Default(),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Run executes tasks in parallel with at most p.workers concurrent goroutines.
// Returns results in completion order. Cancelling ctx cancels all pending tasks.
func (p *Pool) Run(ctx context.Context, tasks []Task) []TaskResult {
	if len(tasks) == 0 {
		return nil
	}

	taskCh := make(chan Task, len(tasks))
	resultCh := make(chan TaskResult, len(tasks))

	var wg sync.WaitGroup
	for i := 0; i < p.workers && i < len(tasks); i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for task := range taskCh {
				// Isolated context per task
				taskCtx, cancel := context.WithCancel(ctx)
				start := time.Now()

				p.logger.Debug("task starting",
					slog.Int("worker", workerID),
					slog.String("task", task.ID),
				)

				err := task.Fn(taskCtx)
				elapsed := time.Since(start)
				cancel()

				result := TaskResult{
					TaskID:   task.ID,
					Name:     task.Name,
					Success:  err == nil,
					Duration: elapsed,
				}
				if err != nil {
					result.Error = err.Error()
					p.logger.Warn("task failed",
						slog.String("task", task.ID),
						slog.Duration("duration", elapsed),
						slog.String("error", err.Error()),
					)
				} else {
					p.logger.Debug("task completed",
						slog.String("task", task.ID),
						slog.Duration("duration", elapsed),
					)
				}
				resultCh <- result
			}
		}(i)
	}

	for _, t := range tasks {
		taskCh <- t
	}
	close(taskCh)

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	var results []TaskResult
	for r := range resultCh {
		results = append(results, r)
	}
	return results
}

// Summary returns a human-readable summary of task results.
func Summary(results []TaskResult) string {
	passed, failed := 0, 0
	var totalDur time.Duration
	for _, r := range results {
		if r.Success {
			passed++
		} else {
			failed++
		}
		totalDur += r.Duration
	}
	return fmt.Sprintf("%d tasks: %d passed, %d failed (total wall time: %s)",
		len(results), passed, failed, totalDur)
}
