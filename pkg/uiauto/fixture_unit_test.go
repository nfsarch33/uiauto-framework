package uiauto

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests exercise the FixtureServer and BrowserPool together without
// a real browser, achieving coverage of the fixture infrastructure and
// the abstract Browser interface paths.

func TestFixtureServer_LoginClean_ContainsExpectedElements(t *testing.T) {
	fs := NewFixtureServer()
	defer fs.Close()

	body := getFixturePage(t, fs, "/login")
	assert.Contains(t, body, `id="username"`)
	assert.Contains(t, body, `id="password"`)
	assert.Contains(t, body, `id="submit-btn"`)
	assert.Contains(t, body, "Sign In")
}

func TestFixtureServer_LoginDrifted_SelectorsChanged(t *testing.T) {
	fs := NewFixtureServer()
	defer fs.Close()
	fs.SetScenario(ScenarioDrifted)

	body := getFixturePage(t, fs, "/login")
	assert.NotContains(t, body, `id="username"`, "drifted should not have #username")
	assert.Contains(t, body, `id="email"`, "drifted should have #email")
	assert.Contains(t, body, `id="login-btn"`, "drifted should have #login-btn")
	assert.Contains(t, body, "Log In")
}

func TestFixtureServer_LoginBroken_MaintenancePage(t *testing.T) {
	fs := NewFixtureServer()
	defer fs.Close()
	fs.SetScenario(ScenarioBroken)

	body := getFixturePage(t, fs, "/login")
	assert.Contains(t, body, "maintenance-banner")
	assert.Contains(t, body, "Under Maintenance")
	assert.NotContains(t, body, `id="submit-btn"`)
}

func TestFixtureServer_LoginUnknown_DataTestIds(t *testing.T) {
	fs := NewFixtureServer()
	defer fs.Close()
	fs.SetScenario(ScenarioUnknown)

	body := getFixturePage(t, fs, "/login")
	assert.Contains(t, body, `data-testid="user-input"`)
	assert.Contains(t, body, `data-testid="pass-input"`)
	assert.Contains(t, body, `data-testid="auth-submit"`)
}

func TestFixtureServer_DashboardClean_HasNav(t *testing.T) {
	fs := NewFixtureServer()
	defer fs.Close()

	body := getFixturePage(t, fs, "/dashboard")
	assert.Contains(t, body, `id="main-nav"`)
	assert.Contains(t, body, `id="logout-btn"`)
	assert.Contains(t, body, `id="welcome-msg"`)
}

func TestFixtureServer_DashboardDrifted_ClassBased(t *testing.T) {
	fs := NewFixtureServer()
	defer fs.Close()
	fs.SetScenario(ScenarioDrifted)

	body := getFixturePage(t, fs, "/dashboard")
	assert.Contains(t, body, `class="top-bar"`)
	assert.Contains(t, body, `class="greeting"`)
	assert.NotContains(t, body, `id="main-nav"`, "drifted removes id-based nav")
}

func TestFixtureServer_RootRedirectsToLogin(t *testing.T) {
	fs := NewFixtureServer()
	defer fs.Close()

	body := getFixturePage(t, fs, "/")
	assert.Contains(t, body, `id="submit-btn"`, "root should serve login page")
}

func TestFixtureServer_ContentTypeHTML(t *testing.T) {
	fs := NewFixtureServer()
	defer fs.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, fs.URL()+"/login", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	assert.True(t, strings.HasPrefix(ct, "text/html"), "expected text/html, got %s", ct)
}

func TestFixtureServer_ConcurrentAccess(t *testing.T) {
	fs := NewFixtureServer()
	defer fs.Close()

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(scenario FixtureScenario) {
			fs.SetScenario(scenario)
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, fs.URL()+"/login", nil)
			if err == nil {
				resp, err := http.DefaultClient.Do(req)
				if err == nil {
					resp.Body.Close()
				}
			}
			done <- true
		}([]FixtureScenario{ScenarioClean, ScenarioDrifted, ScenarioBroken, ScenarioUnknown}[i%4])
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestBrowserPool_WithFixtureServer exercises the pool + fixture server together.
func TestBrowserPool_WithFixtureServer(t *testing.T) {
	fs := NewFixtureServer()
	defer fs.Close()

	pool := NewBrowserPool(BrowserPoolConfig{
		MaxSize: 2,
		Factory: func() (Browser, error) {
			return &mockBrowser{domHTML: "<html>mock</html>"}, nil
		},
	})
	defer pool.CloseAll()

	b, err := pool.Acquire(t.Context())
	require.NoError(t, err)

	err = b.Navigate(fs.URL() + "/login")
	assert.NoError(t, err)

	html, err := b.CaptureDOM()
	assert.NoError(t, err)
	assert.Contains(t, html, "mock")

	pool.Release(b)

	stats := pool.Stats()
	assert.Equal(t, 1, stats.Idle)
	assert.Equal(t, 0, stats.Active)
}

// TestSPAFixture adds an SPA page variant for single-page app testing.
func TestFixtureServer_AddCustomPage(t *testing.T) {
	fs := NewFixtureServer()
	defer fs.Close()

	// Add a custom SPA page
	fs.mu.Lock()
	fs.pages["/spa"] = map[FixtureScenario]string{
		ScenarioClean: `<!DOCTYPE html>
<html><body>
  <div id="app"></div>
  <script>document.getElementById('app').innerHTML = '<h1>SPA Loaded</h1>';</script>
</body></html>`,
	}
	fs.mu.Unlock()

	body := getFixturePage(t, fs, "/spa")
	assert.Contains(t, body, `id="app"`)
	assert.Contains(t, body, "SPA Loaded")
}

// --- helpers ---

func getFixturePage(t *testing.T, fs *FixtureServer, path string) string {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, fs.URL()+path, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, 200, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(body)
}
