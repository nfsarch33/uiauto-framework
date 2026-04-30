package uiauto

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockBrowser satisfies Browser for pool / interface testing.
type mockBrowser struct {
	closed   bool
	closeMu  sync.Mutex
	navURL   string
	domHTML  string
	clickErr error
}

func (m *mockBrowser) Navigate(url string) error { m.navURL = url; return nil }
func (m *mockBrowser) NavigateWithConfig(url string, _ WaitConfig) error {
	m.navURL = url
	return nil
}
func (m *mockBrowser) CaptureDOM() (string, error)                 { return m.domHTML, nil }
func (m *mockBrowser) CaptureScreenshot() ([]byte, error)          { return []byte("png"), nil }
func (m *mockBrowser) Click(sel string) error                      { return m.clickErr }
func (m *mockBrowser) Type(sel, text string) error                 { return nil }
func (m *mockBrowser) Evaluate(expr string, res interface{}) error { return nil }
func (m *mockBrowser) IsVisible(sel string) (bool, error)          { return true, nil }
func (m *mockBrowser) SwitchToFrame(sel string) (func(), error)    { return func() {}, nil }
func (m *mockBrowser) Close() {
	m.closeMu.Lock()
	defer m.closeMu.Unlock()
	m.closed = true
}

func (m *mockBrowser) isClosed() bool {
	m.closeMu.Lock()
	defer m.closeMu.Unlock()
	return m.closed
}

func mockFactory() BrowserFactory {
	return func() (Browser, error) {
		return &mockBrowser{domHTML: "<html></html>"}, nil
	}
}

func failingFactory() BrowserFactory {
	return func() (Browser, error) {
		return nil, fmt.Errorf("chrome not found")
	}
}

// --- Browser interface compliance ---

func TestBrowserInterface_MockSatisfies(t *testing.T) {
	var b Browser = &mockBrowser{}
	assert.NotNil(t, b)
}

func TestBrowserInterface_ChromeDPAdapterSatisfies(t *testing.T) {
	// Compile-time check: ChromeDPBrowserAdapter implements Browser
	var _ Browser = (*ChromeDPBrowserAdapter)(nil)
}

// --- BrowserPool tests ---

func TestBrowserPool_AcquireRelease(t *testing.T) {
	pool := NewBrowserPool(BrowserPoolConfig{
		MaxSize: 2,
		Factory: mockFactory(),
	})
	defer pool.CloseAll()

	ctx := context.Background()
	b1, err := pool.Acquire(ctx)
	require.NoError(t, err)
	assert.NotNil(t, b1)

	stats := pool.Stats()
	assert.Equal(t, 1, stats.Active)
	assert.Equal(t, 0, stats.Idle)
	assert.Equal(t, int64(1), stats.TotalLeased)

	pool.Release(b1)

	stats = pool.Stats()
	assert.Equal(t, 0, stats.Active)
	assert.Equal(t, 1, stats.Idle)
}

func TestBrowserPool_ReuseIdleSessions(t *testing.T) {
	pool := NewBrowserPool(BrowserPoolConfig{
		MaxSize: 2,
		Factory: mockFactory(),
	})
	defer pool.CloseAll()

	ctx := context.Background()
	b1, _ := pool.Acquire(ctx)
	pool.Release(b1)

	b2, err := pool.Acquire(ctx)
	require.NoError(t, err)
	// Should reuse the same instance
	assert.Same(t, b1, b2)
	pool.Release(b2)
}

func TestBrowserPool_MaxConcurrency(t *testing.T) {
	pool := NewBrowserPool(BrowserPoolConfig{
		MaxSize: 2,
		Factory: mockFactory(),
	})
	defer pool.CloseAll()

	ctx := context.Background()
	b1, _ := pool.Acquire(ctx)
	b2, _ := pool.Acquire(ctx)

	stats := pool.Stats()
	assert.Equal(t, 2, stats.Active)

	// Third acquire should block until timeout
	ctxTimeout, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	_, err := pool.Acquire(ctxTimeout)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	pool.Release(b1)
	pool.Release(b2)
}

func TestBrowserPool_ConcurrentAcquireRelease(t *testing.T) {
	pool := NewBrowserPool(BrowserPoolConfig{
		MaxSize: 4,
		Factory: mockFactory(),
	})
	defer pool.CloseAll()

	var wg sync.WaitGroup
	var acquired int32
	ctx := context.Background()

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, err := pool.Acquire(ctx)
			if err != nil {
				return
			}
			atomic.AddInt32(&acquired, 1)
			time.Sleep(5 * time.Millisecond)
			pool.Release(b)
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(20), acquired)
	stats := pool.Stats()
	assert.Equal(t, 0, stats.Active)
	assert.Equal(t, int64(20), stats.TotalLeased)
}

func TestBrowserPool_FactoryError(t *testing.T) {
	pool := NewBrowserPool(BrowserPoolConfig{
		MaxSize: 2,
		Factory: failingFactory(),
	})
	defer pool.CloseAll()

	_, err := pool.Acquire(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "chrome not found")
}

func TestBrowserPool_CloseAll(t *testing.T) {
	pool := NewBrowserPool(BrowserPoolConfig{
		MaxSize: 3,
		Factory: mockFactory(),
	})

	ctx := context.Background()
	b1, _ := pool.Acquire(ctx)
	b2, _ := pool.Acquire(ctx)
	pool.Release(b1)
	pool.Release(b2)

	pool.CloseAll()

	stats := pool.Stats()
	assert.True(t, stats.Closed)
	assert.Equal(t, 0, stats.Idle)

	// Verify both browsers were closed
	assert.True(t, b1.(*mockBrowser).isClosed())
	assert.True(t, b2.(*mockBrowser).isClosed())

	// Acquire after close should fail
	_, err := pool.Acquire(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pool is closed")
}

func TestBrowserPool_ReleaseAfterClose(t *testing.T) {
	pool := NewBrowserPool(BrowserPoolConfig{
		MaxSize: 2,
		Factory: mockFactory(),
	})

	ctx := context.Background()
	b, _ := pool.Acquire(ctx)
	pool.CloseAll()

	// Release after close should close the browser, not panic
	pool.Release(b)
	assert.True(t, b.(*mockBrowser).isClosed())
}

func TestBrowserPool_DefaultMaxSize(t *testing.T) {
	pool := NewBrowserPool(BrowserPoolConfig{
		Factory: mockFactory(),
	})
	defer pool.CloseAll()

	stats := pool.Stats()
	assert.Equal(t, 4, stats.MaxSize)
}

func TestBrowserPool_ReuseWithinMaxSize(t *testing.T) {
	pool := NewBrowserPool(BrowserPoolConfig{
		MaxSize: 1,
		Factory: mockFactory(),
	})
	defer pool.CloseAll()

	ctx := context.Background()
	b1, _ := pool.Acquire(ctx)
	pool.Release(b1) // idle=[b1]

	b2, _ := pool.Acquire(ctx) // takes b1 from idle
	assert.Same(t, b1, b2)
	pool.Release(b2) // idle=[b2]

	stats := pool.Stats()
	assert.Equal(t, 1, stats.Idle)
	assert.Equal(t, 0, stats.Active)
}

// --- ChromeDPBrowserAdapter tests (no real browser) ---

func TestChromeDPAdapter_UnwrapReturnsAgent(t *testing.T) {
	agent := &BrowserAgent{}
	adapter := NewChromeDPBrowserAdapter(agent)
	assert.Same(t, agent, adapter.Unwrap())
}

func TestChromeDPFactory_ReturnsFactory(t *testing.T) {
	f := ChromeDPFactory(true)
	assert.NotNil(t, f)
}

func TestChromeDPRemoteFactory_ReturnsFactory(t *testing.T) {
	f := ChromeDPRemoteFactory("ws://localhost:9222")
	assert.NotNil(t, f)
}

// --- mockBrowser usage tests ---

func TestMockBrowser_Navigate(t *testing.T) {
	b := &mockBrowser{}
	err := b.Navigate("https://example.com")
	assert.NoError(t, err)
	assert.Equal(t, "https://example.com", b.navURL)
}

func TestMockBrowser_CaptureDOM(t *testing.T) {
	b := &mockBrowser{domHTML: "<div>test</div>"}
	html, err := b.CaptureDOM()
	assert.NoError(t, err)
	assert.Equal(t, "<div>test</div>", html)
}

func TestMockBrowser_CaptureScreenshot(t *testing.T) {
	b := &mockBrowser{}
	data, err := b.CaptureScreenshot()
	assert.NoError(t, err)
	assert.Equal(t, []byte("png"), data)
}

func TestMockBrowser_ClickError(t *testing.T) {
	b := &mockBrowser{clickErr: fmt.Errorf("element not found")}
	err := b.Click("#submit")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "element not found")
}
