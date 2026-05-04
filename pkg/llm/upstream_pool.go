package llm

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"
)

// UpstreamNode represents a single LLM backend.
type UpstreamNode struct {
	Name   string   `yaml:"name"    json:"name"`
	URL    string   `yaml:"url"     json:"url"`
	Tier   string   `yaml:"tier"    json:"tier"`
	Weight int      `yaml:"weight"  json:"weight"`
	Models []string `yaml:"models"  json:"models"`
	APIKey string   `yaml:"-"       json:"-"`

	mu          sync.Mutex
	healthy     bool
	failCount   int
	lastCheck   time.Time
	lastFailure time.Time
}

// HealthCheckConfig controls upstream health monitoring.
type HealthCheckConfig struct {
	Interval           time.Duration `yaml:"interval"            json:"interval"`
	Timeout            time.Duration `yaml:"timeout"             json:"timeout"`
	Path               string        `yaml:"path"                json:"path"`
	UnhealthyThreshold int           `yaml:"unhealthy_threshold" json:"unhealthy_threshold"`
	HealthyThreshold   int           `yaml:"healthy_threshold"   json:"healthy_threshold"`
}

// DefaultHealthCheckConfig returns sensible defaults.
func DefaultHealthCheckConfig() HealthCheckConfig {
	return HealthCheckConfig{
		Interval:           15 * time.Second,
		Timeout:            5 * time.Second,
		Path:               "/health",
		UnhealthyThreshold: 3,
		HealthyThreshold:   1,
	}
}

// UpstreamPool manages a set of upstream nodes with health checks
// and tier-based weighted selection.
type UpstreamPool struct {
	nodes  []*UpstreamNode
	hcCfg  HealthCheckConfig
	client HTTPDoer
	logger *slog.Logger
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewUpstreamPool creates a pool and starts background health checks.
func NewUpstreamPool(nodes []*UpstreamNode, cfg HealthCheckConfig, logger *slog.Logger) *UpstreamPool {
	if logger == nil {
		logger = slog.Default()
	}
	for _, n := range nodes {
		n.healthy = true
		if n.Weight <= 0 {
			n.Weight = 1
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &UpstreamPool{
		nodes:  nodes,
		hcCfg:  cfg,
		client: &http.Client{Timeout: cfg.Timeout},
		logger: logger,
		cancel: cancel,
	}
	p.wg.Add(1)
	go p.healthCheckLoop(ctx)
	return p
}

// NewUpstreamPoolWithHTTP creates a pool with an injected HTTP doer (for testing).
func NewUpstreamPoolWithHTTP(nodes []*UpstreamNode, cfg HealthCheckConfig, doer HTTPDoer, logger *slog.Logger) *UpstreamPool {
	if logger == nil {
		logger = slog.Default()
	}
	for _, n := range nodes {
		n.healthy = true
		if n.Weight <= 0 {
			n.Weight = 1
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &UpstreamPool{
		nodes:  nodes,
		hcCfg:  cfg,
		client: doer,
		logger: logger,
		cancel: cancel,
	}
	p.wg.Add(1)
	go p.healthCheckLoop(ctx)
	return p
}

// SelectByTier picks a healthy node matching the given tier using weighted
// random selection. Returns nil if no healthy node matches.
func (p *UpstreamPool) SelectByTier(tier string) *UpstreamNode {
	var candidates []*UpstreamNode
	var totalWeight int
	for _, n := range p.nodes {
		if n.Tier != tier {
			continue
		}
		n.mu.Lock()
		h := n.healthy
		n.mu.Unlock()
		if !h {
			continue
		}
		candidates = append(candidates, n)
		totalWeight += n.Weight
	}
	if len(candidates) == 0 {
		return nil
	}
	r := rand.IntN(totalWeight)
	for _, n := range candidates {
		r -= n.Weight
		if r < 0 {
			return n
		}
	}
	return candidates[len(candidates)-1]
}

// SelectAny picks any healthy node using weighted random selection.
func (p *UpstreamPool) SelectAny() *UpstreamNode {
	var candidates []*UpstreamNode
	var totalWeight int
	for _, n := range p.nodes {
		n.mu.Lock()
		h := n.healthy
		n.mu.Unlock()
		if !h {
			continue
		}
		candidates = append(candidates, n)
		totalWeight += n.Weight
	}
	if len(candidates) == 0 {
		return nil
	}
	r := rand.IntN(totalWeight)
	for _, n := range candidates {
		r -= n.Weight
		if r < 0 {
			return n
		}
	}
	return candidates[len(candidates)-1]
}

// MarkUnhealthy forces a node into unhealthy state (e.g. after a request failure).
func (p *UpstreamPool) MarkUnhealthy(node *UpstreamNode) {
	node.mu.Lock()
	defer node.mu.Unlock()
	node.failCount++
	node.lastFailure = time.Now()
	if node.failCount >= p.hcCfg.UnhealthyThreshold {
		node.healthy = false
		p.logger.Info("upstream marked unhealthy",
			"node", node.Name, "fail_count", node.failCount)
	}
}

// MarkHealthy resets a node to healthy state.
func (p *UpstreamPool) MarkHealthy(node *UpstreamNode) {
	node.mu.Lock()
	defer node.mu.Unlock()
	node.healthy = true
	node.failCount = 0
}

// HealthyCount returns the number of currently healthy nodes.
func (p *UpstreamPool) HealthyCount() int {
	count := 0
	for _, n := range p.nodes {
		n.mu.Lock()
		if n.healthy {
			count++
		}
		n.mu.Unlock()
	}
	return count
}

// Nodes returns the underlying node list for inspection.
func (p *UpstreamPool) Nodes() []*UpstreamNode {
	return p.nodes
}

// Close stops health checks.
func (p *UpstreamPool) Close() {
	p.cancel()
	p.wg.Wait()
}

func (p *UpstreamPool) healthCheckLoop(ctx context.Context) {
	defer p.wg.Done()
	if p.hcCfg.Interval <= 0 {
		return
	}
	ticker := time.NewTicker(p.hcCfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.checkAll(ctx)
		}
	}
}

func (p *UpstreamPool) checkAll(ctx context.Context) {
	for _, node := range p.nodes {
		if ctx.Err() != nil {
			return
		}
		healthy := p.probe(ctx, node)
		node.mu.Lock()
		node.lastCheck = time.Now()
		if healthy {
			if !node.healthy {
				p.logger.Info("upstream recovered", "node", node.Name)
			}
			node.healthy = true
			node.failCount = 0
		} else {
			node.failCount++
			node.lastFailure = time.Now()
			if node.failCount >= p.hcCfg.UnhealthyThreshold {
				if node.healthy {
					p.logger.Warn("upstream marked unhealthy by health check",
						"node", node.Name, "fail_count", node.failCount)
				}
				node.healthy = false
			}
		}
		node.mu.Unlock()
	}
}

func (p *UpstreamPool) probe(ctx context.Context, node *UpstreamNode) bool {
	ctx, cancel := context.WithTimeout(ctx, p.hcCfg.Timeout)
	defer cancel()
	url := node.URL + p.hcCfg.Path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// NodeHealthStatus returns a snapshot for observability.
type NodeHealthStatus struct {
	Name        string    `json:"name"`
	Healthy     bool      `json:"healthy"`
	FailCount   int       `json:"fail_count"`
	LastCheck   time.Time `json:"last_check"`
	LastFailure time.Time `json:"last_failure,omitempty"`
}

// HealthStatus returns per-node health snapshots.
func (p *UpstreamPool) HealthStatus() []NodeHealthStatus {
	result := make([]NodeHealthStatus, len(p.nodes))
	for i, n := range p.nodes {
		n.mu.Lock()
		result[i] = NodeHealthStatus{
			Name:        n.Name,
			Healthy:     n.healthy,
			FailCount:   n.failCount,
			LastCheck:   n.lastCheck,
			LastFailure: n.lastFailure,
		}
		n.mu.Unlock()
	}
	return result
}

// ErrNoHealthyUpstream is returned when all upstream nodes are unhealthy.
var ErrNoHealthyUpstream = fmt.Errorf("no healthy upstream available")
