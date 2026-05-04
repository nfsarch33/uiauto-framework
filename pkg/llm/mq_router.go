package llm

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// MQConfig holds configuration for the MQ-style LLM router.
type MQConfig struct {
	MaxQueueDepth  int               `yaml:"max_queue_depth"  json:"max_queue_depth"`
	MaxConcurrency int               `yaml:"max_concurrency"  json:"max_concurrency"`
	RequestTimeout string            `yaml:"request_timeout"  json:"request_timeout"`
	Nodes          []MQNodeConfig    `yaml:"nodes"            json:"nodes"`
	Tiers          map[string]MQTier `yaml:"tiers"            json:"tiers"`
	HealthCheck    HealthCheckConfig `yaml:"health_check"     json:"health_check"`
}

// MQNodeConfig represents a node in the YAML config.
type MQNodeConfig struct {
	Name      string   `yaml:"name"        json:"name"`
	URL       string   `yaml:"url"         json:"url"`
	Tier      string   `yaml:"tier"        json:"tier"`
	Weight    int      `yaml:"weight"      json:"weight"`
	Models    []string `yaml:"models"      json:"models"`
	APIKeyEnv string   `yaml:"api_key_env" json:"api_key_env"`
}

// MQTier describes a tier's purpose and preferred models.
type MQTier struct {
	Description string   `yaml:"description"  json:"description"`
	Models      []string `yaml:"models"       json:"models"`
	PreferNodes []string `yaml:"prefer_nodes" json:"prefer_nodes"`
}

// MQRouter is an MQ-style LLM router that provides fair-share scheduling
// across multiple upstream nodes with health checks and tier-based routing.
// Implements Provider.
type MQRouter struct {
	pool      *UpstreamPool
	scheduler *FairShareScheduler
	config    MQConfig
	logger    *slog.Logger
}

// NewMQRouter creates a router from MQConfig, starts health checks and
// the fair-share scheduler.
func NewMQRouter(cfg MQConfig, logger *slog.Logger) (*MQRouter, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if len(cfg.Nodes) == 0 {
		return nil, fmt.Errorf("mq router: at least one node is required")
	}

	nodes := make([]*UpstreamNode, len(cfg.Nodes))
	for i, nc := range cfg.Nodes {
		apiKey := ""
		if nc.APIKeyEnv != "" {
			apiKey = os.Getenv(nc.APIKeyEnv)
		}
		nodes[i] = &UpstreamNode{
			Name:   nc.Name,
			URL:    strings.TrimRight(nc.URL, "/"),
			Tier:   nc.Tier,
			Weight: nc.Weight,
			Models: nc.Models,
			APIKey: apiKey,
		}
	}

	hcCfg := cfg.HealthCheck
	if hcCfg.Interval == 0 {
		hcCfg = DefaultHealthCheckConfig()
	}

	pool := NewUpstreamPool(nodes, hcCfg, logger)

	timeout := 5 * time.Minute
	if cfg.RequestTimeout != "" {
		d, err := time.ParseDuration(cfg.RequestTimeout)
		if err == nil {
			timeout = d
		}
	}

	fsCfg := FairShareConfig{
		MaxQueueDepth:  cfg.MaxQueueDepth,
		MaxConcurrency: cfg.MaxConcurrency,
		RequestTimeout: timeout,
	}
	if fsCfg.MaxQueueDepth <= 0 {
		fsCfg.MaxQueueDepth = DefaultFairShareConfig().MaxQueueDepth
	}
	if fsCfg.MaxConcurrency <= 0 {
		fsCfg.MaxConcurrency = DefaultFairShareConfig().MaxConcurrency
	}

	providerFactory := func(node *UpstreamNode) Provider {
		if IsBedrockEndpoint(node.URL) {
			return NewBedrockClient(BedrockConfig{
				BaseURL: node.URL,
				APIKey:  node.APIKey,
				ModelID: firstModel(node.Models),
				Timeout: timeout,
			})
		}
		return NewClient(Config{
			BaseURL: node.URL,
			APIKey:  node.APIKey,
			Model:   firstModel(node.Models),
			Timeout: timeout,
		})
	}

	scheduler := NewFairShareScheduler(fsCfg, pool, providerFactory, logger)

	return &MQRouter{
		pool:      pool,
		scheduler: scheduler,
		config:    cfg,
		logger:    logger,
	}, nil
}

// Complete implements Provider by routing through the fair-share scheduler.
func (r *MQRouter) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	userID := extractUserID(ctx)
	return r.scheduler.Submit(ctx, userID, req)
}

// Pool returns the underlying upstream pool for health inspection.
func (r *MQRouter) Pool() *UpstreamPool {
	return r.pool
}

// Close shuts down the scheduler and health checks.
func (r *MQRouter) Close() {
	r.scheduler.Close()
	r.pool.Close()
}

// LoadMQConfig reads an MQ config from a YAML file.
func LoadMQConfig(path string) (*MQConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read mq config: %w", err)
	}
	return ParseMQConfig(data)
}

// ParseMQConfig parses MQ config from YAML bytes.
func ParseMQConfig(data []byte) (*MQConfig, error) {
	var cfg MQConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse mq config: %w", err)
	}
	return &cfg, nil
}

func firstModel(models []string) string {
	if len(models) > 0 {
		return models[0]
	}
	return ""
}

type userIDKey struct{}

// WithUserID adds a user ID to the context for fair-share scheduling.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey{}, userID)
}

func extractUserID(ctx context.Context) string {
	if v, ok := ctx.Value(userIDKey{}).(string); ok {
		return v
	}
	return ""
}
