package uiauto

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
)

// FixtureScenario names the E2E fixture page variants.
type FixtureScenario string

const (
	ScenarioClean   FixtureScenario = "clean"
	ScenarioDrifted FixtureScenario = "drifted"
	ScenarioBroken  FixtureScenario = "broken"
	ScenarioUnknown FixtureScenario = "unknown"
)

// FixtureServer hosts deterministic HTML pages for E2E tests.
// Switch the active scenario at runtime to simulate UI changes mid-test.
type FixtureServer struct {
	Server *httptest.Server

	mu       sync.Mutex
	scenario FixtureScenario
	pages    map[string]map[FixtureScenario]string
}

// NewFixtureServer builds a test server with pre-loaded page variants.
func NewFixtureServer() *FixtureServer {
	fs := &FixtureServer{
		scenario: ScenarioClean,
		pages:    defaultFixturePages(),
	}
	fs.Server = httptest.NewServer(http.HandlerFunc(fs.handler))
	return fs
}

func (f *FixtureServer) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	scenario := f.scenario
	f.mu.Unlock()

	path := r.URL.Path
	if path == "" || path == "/" {
		path = "/login"
	}

	variants, ok := f.pages[path]
	if !ok {
		http.NotFound(w, r)
		return
	}

	html, ok := variants[scenario]
	if !ok {
		html = variants[ScenarioClean]
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, html)
}

// SetScenario changes the active fixture scenario.
func (f *FixtureServer) SetScenario(s FixtureScenario) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scenario = s
}

// URL returns the base URL of the fixture server.
func (f *FixtureServer) URL() string {
	return f.Server.URL
}

// Close shuts down the server.
func (f *FixtureServer) Close() {
	f.Server.Close()
}

func defaultFixturePages() map[string]map[FixtureScenario]string {
	return map[string]map[FixtureScenario]string{
		"/login": {
			ScenarioClean: `<!DOCTYPE html>
<html lang="en">
<head><title>Login</title></head>
<body>
  <form id="login-form" action="/submit" method="post">
    <label for="username">Username</label>
    <input id="username" name="username" type="text" placeholder="Enter username" />
    <label for="password">Password</label>
    <input id="password" name="password" type="password" placeholder="Enter password" />
    <button id="submit-btn" type="submit" class="btn primary">Sign In</button>
  </form>
  <a id="forgot-link" href="/forgot">Forgot password?</a>
</body>
</html>`,
			ScenarioDrifted: `<!DOCTYPE html>
<html lang="en">
<head><title>Login</title></head>
<body>
  <form id="login-form" action="/submit" method="post">
    <label for="email">Email</label>
    <input id="email" name="email" type="email" placeholder="Enter email" />
    <label for="pwd">Password</label>
    <input id="pwd" name="pwd" type="password" placeholder="Enter password" />
    <button id="login-btn" type="submit" class="btn primary">Log In</button>
  </form>
  <a id="reset-link" href="/reset">Reset password?</a>
</body>
</html>`,
			ScenarioBroken: `<!DOCTYPE html>
<html lang="en">
<head><title>Maintenance</title></head>
<body>
  <div id="maintenance-banner">
    <h1>Site Under Maintenance</h1>
    <p>Please try again later.</p>
  </div>
</body>
</html>`,
			ScenarioUnknown: `<!DOCTYPE html>
<html lang="en">
<head><title>Login v3</title></head>
<body>
  <div class="auth-container">
    <div class="card">
      <h2>Welcome Back</h2>
      <div class="field-group">
        <input data-testid="user-input" type="text" aria-label="Username" />
      </div>
      <div class="field-group">
        <input data-testid="pass-input" type="password" aria-label="Password" />
      </div>
      <div class="actions">
        <button data-testid="auth-submit" class="cta">Continue</button>
      </div>
    </div>
  </div>
</body>
</html>`,
		},
		"/dashboard": {
			ScenarioClean: `<!DOCTYPE html>
<html lang="en">
<head><title>Dashboard</title></head>
<body>
  <nav id="main-nav">
    <a id="nav-home" href="/">Home</a>
    <a id="nav-profile" href="/profile">Profile</a>
    <a id="nav-settings" href="/settings">Settings</a>
    <button id="logout-btn">Logout</button>
  </nav>
  <main id="content">
    <h1 id="welcome-msg">Welcome, User</h1>
    <div id="stats-panel">
      <div class="stat" data-metric="orders">42</div>
      <div class="stat" data-metric="revenue">$1,234</div>
    </div>
  </main>
</body>
</html>`,
			ScenarioDrifted: `<!DOCTYPE html>
<html lang="en">
<head><title>Dashboard</title></head>
<body>
  <header>
    <nav class="top-bar">
      <a class="nav-link" href="/">Home</a>
      <a class="nav-link" href="/profile">Profile</a>
      <a class="nav-link" href="/settings">Settings</a>
      <a class="nav-link sign-out" href="/logout">Sign Out</a>
    </nav>
  </header>
  <main>
    <h1 class="greeting">Hello, User</h1>
    <section class="metrics">
      <div class="metric-card">Orders: 42</div>
      <div class="metric-card">Revenue: $1,234</div>
    </section>
  </main>
</body>
</html>`,
		},
	}
}
