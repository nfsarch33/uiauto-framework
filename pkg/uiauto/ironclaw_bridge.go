package uiauto

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// AgentTask represents a UI automation task from IronClaw.
type AgentTask struct {
	ID          string            `json:"id"`
	Type        AgentTaskType     `json:"type"`
	URL         string            `json:"url,omitempty"`
	Actions     []Action          `json:"actions,omitempty"`
	Description string            `json:"description,omitempty"`
	Timeout     time.Duration     `json:"timeout,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// AgentTaskType classifies the kind of task.
type AgentTaskType string

// Agent task types for IronClaw fleet operations.
const (
	TaskTypeNavigate    AgentTaskType = "navigate"
	TaskTypeInteract    AgentTaskType = "interact"
	TaskTypeDiscover    AgentTaskType = "discover"
	TaskTypeRegression  AgentTaskType = "regression"
	TaskTypeHealthCheck AgentTaskType = "health_check"
)

// AgentTaskResult is the response sent back to IronClaw.
type AgentTaskResult struct {
	TaskID    string             `json:"task_id"`
	Success   bool               `json:"success"`
	Status    string             `json:"status"`
	Duration  time.Duration      `json:"duration"`
	Error     string             `json:"error,omitempty"`
	Metrics   *AggregatedMetrics `json:"metrics,omitempty"`
	Patterns  int                `json:"patterns_discovered"`
	Converged bool               `json:"converged"`
	ModelTier string             `json:"model_tier"`
	Details   json.RawMessage    `json:"details,omitempty"`
}

// IronClawBridgeConfig configures the bridge.
type IronClawBridgeConfig struct {
	DefaultTimeout time.Duration
	MaxConcurrent  int
	Logger         *slog.Logger
}

// DefaultBridgeConfig returns sensible defaults.
func DefaultBridgeConfig() IronClawBridgeConfig {
	return IronClawBridgeConfig{
		DefaultTimeout: 60 * time.Second,
		MaxConcurrent:  4,
		Logger:         slog.Default(),
	}
}

// IronClawBridge translates IronClaw agent tasks into MemberAgent operations.
type IronClawBridge struct {
	mu            sync.RWMutex
	agent         *MemberAgent
	config        IronClawBridgeConfig
	logger        *slog.Logger
	taskHistory   []AgentTaskResult
	activeCount   int
	totalExecuted int64
	totalSuccess  int64
	totalFailed   int64
}

// NewIronClawBridge creates a bridge wrapping an existing MemberAgent.
func NewIronClawBridge(agent *MemberAgent, cfg IronClawBridgeConfig) *IronClawBridge {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 4
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 60 * time.Second
	}
	return &IronClawBridge{
		agent:  agent,
		config: cfg,
		logger: cfg.Logger,
	}
}

// Execute runs a task and returns the result. This is the main A2A entry point.
func (b *IronClawBridge) Execute(ctx context.Context, task AgentTask) AgentTaskResult {
	// Check concurrency limits
	b.mu.Lock()
	if b.activeCount >= b.config.MaxConcurrent {
		b.mu.Unlock()
		return AgentTaskResult{
			TaskID:  task.ID,
			Success: false,
			Status:  "rejected",
			Error:   fmt.Sprintf("concurrency limit exceeded (max %d)", b.config.MaxConcurrent),
		}
	}
	b.activeCount++
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		b.activeCount--
		b.mu.Unlock()
	}()

	// Apply timeout from task or default
	timeout := b.config.DefaultTimeout
	if task.Timeout > 0 {
		timeout = task.Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	var result AgentTaskResult

	switch task.Type {
	case TaskTypeNavigate:
		result = b.executeNavigate(ctx, task)
	case TaskTypeInteract:
		result = b.executeInteract(ctx, task)
	case TaskTypeDiscover:
		result = b.executeDiscover(ctx, task)
	case TaskTypeRegression:
		result = b.executeRegression(ctx, task)
	case TaskTypeHealthCheck:
		result = b.HealthCheck()
		result.TaskID = task.ID
	default:
		result = AgentTaskResult{
			TaskID:  task.ID,
			Success: false,
			Status:  "failed",
			Error:   fmt.Sprintf("unknown task type: %s", task.Type),
		}
	}

	result.Duration = time.Since(start)

	// Record result in history and update stats
	b.mu.Lock()
	b.totalExecuted++
	if result.Success {
		b.totalSuccess++
	} else {
		b.totalFailed++
	}
	// Keep last 100 results
	b.taskHistory = append(b.taskHistory, result)
	if len(b.taskHistory) > 100 {
		b.taskHistory = b.taskHistory[len(b.taskHistory)-100:]
	}
	b.mu.Unlock()

	b.logger.Info("task executed",
		slog.String("task_id", task.ID),
		slog.String("type", string(task.Type)),
		slog.Bool("success", result.Success),
		slog.Duration("duration", result.Duration),
	)
	return result
}

func (b *IronClawBridge) executeNavigate(ctx context.Context, task AgentTask) AgentTaskResult {
	if task.URL == "" {
		return AgentTaskResult{
			TaskID:  task.ID,
			Success: false,
			Status:  "failed",
			Error:   "navigate task requires url",
		}
	}
	err := b.agent.Navigate(task.URL)
	if err != nil {
		return AgentTaskResult{
			TaskID:    task.ID,
			Success:   false,
			Status:    "failed",
			Error:     err.Error(),
			Metrics:   ptrCopy(b.agent.Metrics()),
			Patterns:  0,
			Converged: b.agent.IsConverged(),
			ModelTier: b.agent.CurrentTier().String(),
		}
	}
	return AgentTaskResult{
		TaskID:    task.ID,
		Success:   true,
		Status:    "completed",
		Metrics:   ptrCopy(b.agent.Metrics()),
		Converged: b.agent.IsConverged(),
		ModelTier: b.agent.CurrentTier().String(),
	}
}

func (b *IronClawBridge) executeInteract(ctx context.Context, task AgentTask) AgentTaskResult {
	tr := b.agent.RunTask(ctx, task.ID, task.Actions)
	success := tr.Status == TaskCompleted
	errStr := ""
	if tr.Error != nil {
		errStr = tr.Error.Error()
	}
	details, _ := json.Marshal(map[string]any{
		"actions":      tr.Actions,
		"heal_results": tr.HealResults,
	})
	return AgentTaskResult{
		TaskID:    task.ID,
		Success:   success,
		Status:    tr.Status.String(),
		Error:     errStr,
		Metrics:   ptrCopy(b.agent.Metrics()),
		Patterns:  tr.PatternCount,
		Converged: tr.Converged,
		ModelTier: b.agent.CurrentTier().String(),
		Details:   details,
	}
}

func (b *IronClawBridge) executeDiscover(ctx context.Context, task AgentTask) AgentTaskResult {
	var patterns int
	var lastErr error
	for _, action := range task.Actions {
		_, err := b.agent.DiscoverAndRegister(ctx, action.TargetID, action.Description)
		if err != nil {
			lastErr = err
			continue
		}
		patterns++
	}
	if lastErr != nil && patterns == 0 {
		return AgentTaskResult{
			TaskID:    task.ID,
			Success:   false,
			Status:    "failed",
			Error:     lastErr.Error(),
			Metrics:   ptrCopy(b.agent.Metrics()),
			Patterns:  patterns,
			Converged: b.agent.IsConverged(),
			ModelTier: b.agent.CurrentTier().String(),
		}
	}
	return AgentTaskResult{
		TaskID:    task.ID,
		Success:   patterns > 0,
		Status:    "completed",
		Error:     errStr(lastErr),
		Metrics:   ptrCopy(b.agent.Metrics()),
		Patterns:  patterns,
		Converged: b.agent.IsConverged(),
		ModelTier: b.agent.CurrentTier().String(),
	}
}

func (b *IronClawBridge) executeRegression(ctx context.Context, task AgentTask) AgentTaskResult {
	results := b.agent.DetectDriftAndHeal(ctx)
	details, _ := json.Marshal(results)
	success := true
	for _, r := range results {
		if !r.Success {
			success = false
			break
		}
	}
	return AgentTaskResult{
		TaskID:    task.ID,
		Success:   success,
		Status:    "completed",
		Metrics:   ptrCopy(b.agent.Metrics()),
		Patterns:  len(results),
		Converged: b.agent.IsConverged(),
		ModelTier: b.agent.CurrentTier().String(),
		Details:   details,
	}
}

// HealthCheck returns current agent health status.
func (b *IronClawBridge) HealthCheck() AgentTaskResult {
	metrics := b.agent.Metrics()
	return AgentTaskResult{
		TaskID:    "health_check",
		Success:   !b.agent.IsDegraded(),
		Status:    "completed",
		Metrics:   &metrics,
		Converged: b.agent.IsConverged(),
		ModelTier: b.agent.CurrentTier().String(),
	}
}

// History returns recent task results.
func (b *IronClawBridge) History() []AgentTaskResult {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]AgentTaskResult, len(b.taskHistory))
	copy(out, b.taskHistory)
	return out
}

// Stats returns bridge-level statistics.
func (b *IronClawBridge) Stats() BridgeStats {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return BridgeStats{
		TotalExecuted: b.totalExecuted,
		TotalSuccess:  b.totalSuccess,
		TotalFailed:   b.totalFailed,
		ActiveTasks:   b.activeCount,
		HistorySize:   len(b.taskHistory),
	}
}

// BridgeStats holds bridge-level counters.
type BridgeStats struct {
	TotalExecuted int64
	TotalSuccess  int64
	TotalFailed   int64
	ActiveTasks   int
	HistorySize   int
}

func ptrCopy(m AggregatedMetrics) *AggregatedMetrics {
	return &m
}

func errStr(e error) string {
	if e == nil {
		return ""
	}
	return e.Error()
}
