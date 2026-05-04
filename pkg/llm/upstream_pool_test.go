package llm

import (
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testNodes() []*UpstreamNode {
	return []*UpstreamNode{
		{Name: "node-a", URL: "http://localhost:8001", Tier: "fast", Weight: 3, Models: []string{"model-a"}},
		{Name: "node-b", URL: "http://localhost:8002", Tier: "fast", Weight: 1, Models: []string{"model-b"}},
		{Name: "node-c", URL: "http://localhost:8003", Tier: "agent", Weight: 4, Models: []string{"model-c"}},
	}
}

func noopPool(nodes []*UpstreamNode) *UpstreamPool {
	cfg := HealthCheckConfig{Interval: 0} // no background checks
	doer := &roundTripFunc{fn: func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	}}
	return NewUpstreamPoolWithHTTP(nodes, cfg, doer, slog.Default())
}

func TestUpstreamPool_SelectByTier_ReturnsMatchingTier(t *testing.T) {
	nodes := testNodes()
	pool := noopPool(nodes)
	defer pool.Close()

	node := pool.SelectByTier("agent")
	require.NotNil(t, node)
	assert.Equal(t, "agent", node.Tier)
	assert.Equal(t, "node-c", node.Name)
}

func TestUpstreamPool_SelectByTier_NoMatch(t *testing.T) {
	nodes := testNodes()
	pool := noopPool(nodes)
	defer pool.Close()

	node := pool.SelectByTier("nonexistent")
	assert.Nil(t, node)
}

func TestUpstreamPool_SelectAny_AllHealthy(t *testing.T) {
	nodes := testNodes()
	pool := noopPool(nodes)
	defer pool.Close()

	node := pool.SelectAny()
	require.NotNil(t, node)
	found := false
	for _, n := range nodes {
		if n.Name == node.Name {
			found = true
		}
	}
	assert.True(t, found)
}

func TestUpstreamPool_MarkUnhealthy_RemovesFromSelection(t *testing.T) {
	nodes := []*UpstreamNode{
		{Name: "only-node", URL: "http://localhost:8001", Tier: "fast", Weight: 1},
	}
	pool := noopPool(nodes)
	defer pool.Close()
	pool.hcCfg.UnhealthyThreshold = 1

	pool.MarkUnhealthy(nodes[0])
	assert.Nil(t, pool.SelectByTier("fast"))
	assert.Equal(t, 0, pool.HealthyCount())
}

func TestUpstreamPool_MarkHealthy_Recovers(t *testing.T) {
	nodes := []*UpstreamNode{
		{Name: "only-node", URL: "http://localhost:8001", Tier: "fast", Weight: 1},
	}
	pool := noopPool(nodes)
	defer pool.Close()
	pool.hcCfg.UnhealthyThreshold = 1

	pool.MarkUnhealthy(nodes[0])
	assert.Equal(t, 0, pool.HealthyCount())

	pool.MarkHealthy(nodes[0])
	assert.Equal(t, 1, pool.HealthyCount())
	require.NotNil(t, pool.SelectByTier("fast"))
}

func TestUpstreamPool_HealthyCount(t *testing.T) {
	nodes := testNodes()
	pool := noopPool(nodes)
	defer pool.Close()

	assert.Equal(t, 3, pool.HealthyCount())
}

func TestUpstreamPool_HealthStatus_ReturnsSnapshot(t *testing.T) {
	nodes := testNodes()
	pool := noopPool(nodes)
	defer pool.Close()

	statuses := pool.HealthStatus()
	require.Len(t, statuses, 3)
	for _, s := range statuses {
		assert.True(t, s.Healthy)
		assert.Equal(t, 0, s.FailCount)
	}
}

func TestUpstreamPool_WeightedSelection_Distribution(t *testing.T) {
	nodes := []*UpstreamNode{
		{Name: "heavy", URL: "http://localhost:8001", Tier: "fast", Weight: 9},
		{Name: "light", URL: "http://localhost:8002", Tier: "fast", Weight: 1},
	}
	pool := noopPool(nodes)
	defer pool.Close()

	counts := map[string]int{}
	const trials = 10000
	for range trials {
		n := pool.SelectByTier("fast")
		require.NotNil(t, n)
		counts[n.Name]++
	}
	heavyPct := float64(counts["heavy"]) / trials
	assert.InDelta(t, 0.9, heavyPct, 0.05, "heavy node should get ~90%% of traffic")
}

func TestUpstreamPool_DefaultWeight(t *testing.T) {
	nodes := []*UpstreamNode{
		{Name: "zero-weight", URL: "http://localhost:8001", Tier: "fast", Weight: 0},
	}
	pool := noopPool(nodes)
	defer pool.Close()

	assert.Equal(t, 1, nodes[0].Weight, "zero weight should be normalized to 1")
	node := pool.SelectByTier("fast")
	require.NotNil(t, node)
}

func TestUpstreamPool_HealthCheck_Integration(t *testing.T) {
	callCount := 0
	doer := &roundTripFunc{fn: func(req *http.Request) (*http.Response, error) {
		callCount++
		if strings.Contains(req.URL.Path, "/health") {
			return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
		}
		return &http.Response{StatusCode: 404, Body: http.NoBody}, nil
	}}
	nodes := []*UpstreamNode{
		{Name: "hc-node", URL: "http://localhost:8001", Tier: "fast", Weight: 1},
	}
	cfg := HealthCheckConfig{
		Interval:           50 * time.Millisecond,
		Timeout:            1 * time.Second,
		Path:               "/health",
		UnhealthyThreshold: 3,
		HealthyThreshold:   1,
	}
	pool := NewUpstreamPoolWithHTTP(nodes, cfg, doer, slog.Default())
	time.Sleep(200 * time.Millisecond)
	pool.Close()

	assert.Greater(t, callCount, 0, "health checks should have been called")
	assert.Equal(t, 1, pool.HealthyCount())
}

type roundTripFunc struct {
	fn func(req *http.Request) (*http.Response, error)
}

func (f *roundTripFunc) Do(req *http.Request) (*http.Response, error) {
	return f.fn(req)
}
