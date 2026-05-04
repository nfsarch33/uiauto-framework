package llm

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMQConfig_Valid(t *testing.T) {
	yaml := `
max_queue_depth: 10
max_concurrency: 2
request_timeout: "5m"
nodes:
  - name: local-vllm
    url: http://127.0.0.1:8001
    tier: agent
    weight: 4
    models: ["qwen3.5-27b"]
  - name: openai
    url: https://api.openai.com/v1
    tier: powerful
    weight: 3
    models: ["gpt-4o"]
    api_key_env: OPENAI_API_KEY
health_check:
  interval: 15s
  timeout: 5s
  path: /health
  unhealthy_threshold: 3
  healthy_threshold: 1
tiers:
  agent:
    description: "Reliable multi-step tool calling"
    models: ["qwen3.5-27b"]
  powerful:
    description: "Large models for complex tasks"
    models: ["gpt-4o"]
`
	cfg, err := ParseMQConfig([]byte(yaml))
	require.NoError(t, err)
	assert.Equal(t, 10, cfg.MaxQueueDepth)
	assert.Equal(t, 2, cfg.MaxConcurrency)
	assert.Equal(t, "5m", cfg.RequestTimeout)
	require.Len(t, cfg.Nodes, 2)
	assert.Equal(t, "local-vllm", cfg.Nodes[0].Name)
	assert.Equal(t, "agent", cfg.Nodes[0].Tier)
	assert.Equal(t, 4, cfg.Nodes[0].Weight)
	assert.Equal(t, "OPENAI_API_KEY", cfg.Nodes[1].APIKeyEnv)
	require.Len(t, cfg.Tiers, 2)
	assert.Equal(t, "Reliable multi-step tool calling", cfg.Tiers["agent"].Description)
	assert.Equal(t, 15*time.Second, cfg.HealthCheck.Interval)
}

func TestParseMQConfig_Empty(t *testing.T) {
	cfg, err := ParseMQConfig([]byte("{}"))
	require.NoError(t, err)
	assert.Empty(t, cfg.Nodes)
}

func TestParseMQConfig_InvalidYAML(t *testing.T) {
	_, err := ParseMQConfig([]byte("not: [valid: yaml"))
	require.Error(t, err)
}

func TestNewMQRouter_NoNodes(t *testing.T) {
	_, err := NewMQRouter(MQConfig{}, slog.Default())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one node")
}

func TestMQRouter_Complete_Success(t *testing.T) {
	cfg := MQConfig{
		MaxQueueDepth:  5,
		MaxConcurrency: 2,
		RequestTimeout: "5s",
		Nodes: []MQNodeConfig{
			{Name: "mock-node", URL: "http://localhost:19999", Tier: "fast", Weight: 1, Models: []string{"test-model"}},
		},
		HealthCheck: HealthCheckConfig{Interval: 0},
	}

	router, err := NewMQRouter(cfg, slog.Default())
	require.NoError(t, err)
	defer router.Close()

	assert.Equal(t, 1, router.Pool().HealthyCount())
}

func TestMQRouter_WithUserID(t *testing.T) {
	ctx := WithUserID(context.Background(), "test-user")
	assert.Equal(t, "test-user", extractUserID(ctx))
}

func TestMQRouter_WithUserID_Empty(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, "", extractUserID(ctx))
}

func TestMQRouter_PoolAccess(t *testing.T) {
	cfg := MQConfig{
		Nodes: []MQNodeConfig{
			{Name: "n1", URL: "http://localhost:8001", Tier: "fast", Weight: 1},
			{Name: "n2", URL: "http://localhost:8002", Tier: "agent", Weight: 2},
		},
		HealthCheck: HealthCheckConfig{Interval: 0},
	}

	router, err := NewMQRouter(cfg, slog.Default())
	require.NoError(t, err)
	defer router.Close()

	assert.Equal(t, 2, router.Pool().HealthyCount())
	statuses := router.Pool().HealthStatus()
	require.Len(t, statuses, 2)
}

func TestFirstModel(t *testing.T) {
	assert.Equal(t, "a", firstModel([]string{"a", "b"}))
	assert.Equal(t, "", firstModel(nil))
	assert.Equal(t, "", firstModel([]string{}))
}

func TestExtractTierFromRequest(t *testing.T) {
	tests := []struct {
		name   string
		system string
		want   string
	}{
		{"no tier", "normal request", ""},
		{"agent tier", "tier:agent", "agent"},
		{"fast tier", "x-tier:fast routing", "fast"},
		{"empty messages", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := CompletionRequest{
				Messages: []Message{{Role: "system", Content: tt.system}},
			}
			got := extractTierFromRequest(req)
			assert.Equal(t, tt.want, got)
		})
	}
}
