package uiauto

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/nfsarch33/uiauto-framework/pkg/domheal"
)

func testFingerprint(html string) domheal.DOMFingerprint {
	return domheal.ParseDOMFingerprint(html)
}

// Sprint 8: PostgreSQL live integration tests for PostgresPatternStore via testcontainers.

func setupPostgresContainer(t *testing.T) (pool *pgxpool.Pool, cleanup func()) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping postgres integration test in short mode")
	}

	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("uiauto_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err, "failed to start postgres container")

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	pool, err = pgxpool.New(ctx, connStr)
	require.NoError(t, err)

	cleanup = func() {
		pool.Close()
		pgContainer.Terminate(ctx)
	}

	return pool, cleanup
}

func TestPostgresStore_MigrateCreatesAllTables(t *testing.T) {
	pool, cleanup := setupPostgresContainer(t)
	defer cleanup()

	ctx := context.Background()
	store := NewPostgresPatternStore(pool)

	err := store.Migrate(ctx)
	require.NoError(t, err)

	// Verify tables exist by querying them
	var count int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM ui_patterns").Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 0, count)

	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM ui_drift_alerts").Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 0, count)

	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM ui_model_handoffs").Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestPostgresStore_MigrateIdempotent(t *testing.T) {
	pool, cleanup := setupPostgresContainer(t)
	defer cleanup()

	ctx := context.Background()
	store := NewPostgresPatternStore(pool)

	// Run migrate twice -- should not error
	require.NoError(t, store.Migrate(ctx))
	require.NoError(t, store.Migrate(ctx))
}

func TestPostgresStore_SaveAndGet(t *testing.T) {
	pool, cleanup := setupPostgresContainer(t)
	defer cleanup()

	ctx := context.Background()
	store := NewPostgresPatternStore(pool)
	require.NoError(t, store.Migrate(ctx))

	pattern := UIPattern{
		ID:          "login_button",
		Selector:    "#login-btn",
		Description: "Login button on the homepage",
		Confidence:  0.85,
		LastSeen:    time.Now().UTC().Truncate(time.Microsecond),
		Fingerprint: testFingerprint(`<html><body><div><div><div><button id="login-btn">Login</button></div></div></div></body></html>`),
		Metadata:    map[string]string{"page": "home", "cms": "d2l"},
	}

	err := store.SavePattern(ctx, pattern)
	require.NoError(t, err)

	got, ok := store.Get(ctx, "login_button")
	require.True(t, ok, "pattern should exist after save")
	assert.Equal(t, pattern.ID, got.ID)
	assert.Equal(t, pattern.Selector, got.Selector)
	assert.Equal(t, pattern.Description, got.Description)
	assert.InDelta(t, pattern.Confidence, got.Confidence, 0.001)
}

func TestPostgresStore_UpsertOverwrite(t *testing.T) {
	pool, cleanup := setupPostgresContainer(t)
	defer cleanup()

	ctx := context.Background()
	store := NewPostgresPatternStore(pool)
	require.NoError(t, store.Migrate(ctx))

	original := UIPattern{
		ID:          "nav_menu",
		Selector:    "#nav",
		Description: "Navigation menu",
		Confidence:  0.5,
		LastSeen:    time.Now().UTC().Truncate(time.Microsecond),
		Fingerprint: testFingerprint(`<html><body><nav id="nav">Home</nav></body></html>`),
	}
	require.NoError(t, store.SavePattern(ctx, original))

	updated := original
	updated.Selector = ".main-nav"
	updated.Confidence = 0.9
	require.NoError(t, store.SavePattern(ctx, updated))

	got, ok := store.Get(ctx, "nav_menu")
	require.True(t, ok)
	assert.Equal(t, ".main-nav", got.Selector)
	assert.InDelta(t, 0.9, got.Confidence, 0.001)
}

func TestPostgresStore_LoadAll(t *testing.T) {
	pool, cleanup := setupPostgresContainer(t)
	defer cleanup()

	ctx := context.Background()
	store := NewPostgresPatternStore(pool)
	require.NoError(t, store.Migrate(ctx))

	for i := 0; i < 5; i++ {
		p := UIPattern{
			ID:          fmt.Sprintf("pattern_%d", i),
			Selector:    fmt.Sprintf("#el-%d", i),
			Description: fmt.Sprintf("Element %d", i),
			Confidence:  float64(i) * 0.2,
			LastSeen:    time.Now().UTC().Truncate(time.Microsecond),
			Fingerprint: testFingerprint(fmt.Sprintf(`<html><body><div id="el-%d">content %d</div></body></html>`, i, i)),
		}
		require.NoError(t, store.SavePattern(ctx, p))
	}

	all, err := store.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, 5, len(all))
	assert.Contains(t, all, "pattern_0")
	assert.Contains(t, all, "pattern_4")
}

func TestPostgresStore_SetImplementsInterface(t *testing.T) {
	pool, cleanup := setupPostgresContainer(t)
	defer cleanup()

	ctx := context.Background()
	store := NewPostgresPatternStore(pool)
	require.NoError(t, store.Migrate(ctx))

	p := UIPattern{
		ID:          "via_set",
		Selector:    "#set-test",
		Description: "Test via Set interface",
		Confidence:  0.7,
		LastSeen:    time.Now().UTC().Truncate(time.Microsecond),
		Fingerprint: testFingerprint(`<html><body><span id="set-test">test</span></body></html>`),
	}
	err := store.Set(ctx, p)
	require.NoError(t, err)

	got, ok := store.Get(ctx, "via_set")
	assert.True(t, ok)
	assert.Equal(t, "#set-test", got.Selector)
}

func TestPostgresStore_BoostConfidence(t *testing.T) {
	pool, cleanup := setupPostgresContainer(t)
	defer cleanup()

	ctx := context.Background()
	store := NewPostgresPatternStore(pool)
	require.NoError(t, store.Migrate(ctx))

	p := UIPattern{
		ID:          "boost_test",
		Selector:    "#boosted",
		Confidence:  0.5,
		LastSeen:    time.Now().UTC().Truncate(time.Microsecond),
		Fingerprint: testFingerprint(`<html><body><a id="boosted" href="#">link</a></body></html>`),
	}
	require.NoError(t, store.SavePattern(ctx, p))

	err := store.BoostConfidence(ctx, "boost_test", 0.3)
	require.NoError(t, err)

	got, ok := store.Get(ctx, "boost_test")
	require.True(t, ok)
	assert.InDelta(t, 0.8, got.Confidence, 0.01)

	// Boost again -- should cap at 1.0
	require.NoError(t, store.BoostConfidence(ctx, "boost_test", 0.5))
	got, _ = store.Get(ctx, "boost_test")
	assert.InDelta(t, 1.0, got.Confidence, 0.01)
}

func TestPostgresStore_BoostConfidenceNotFound(t *testing.T) {
	pool, cleanup := setupPostgresContainer(t)
	defer cleanup()

	ctx := context.Background()
	store := NewPostgresPatternStore(pool)
	require.NoError(t, store.Migrate(ctx))

	err := store.BoostConfidence(ctx, "nonexistent", 0.1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pattern not found")
}

func TestPostgresStore_DecayConfidence(t *testing.T) {
	pool, cleanup := setupPostgresContainer(t)
	defer cleanup()

	ctx := context.Background()
	store := NewPostgresPatternStore(pool)
	require.NoError(t, store.Migrate(ctx))

	old := UIPattern{
		ID:          "old_pattern",
		Selector:    "#old",
		Description: "Old pattern",
		Confidence:  0.8,
		LastSeen:    time.Now().Add(-48 * time.Hour).UTC().Truncate(time.Microsecond),
		Fingerprint: testFingerprint(`<html><body><p id="old">old content</p></body></html>`),
	}
	recent := UIPattern{
		ID:          "recent_pattern",
		Selector:    "#recent",
		Description: "Recent pattern",
		Confidence:  0.8,
		LastSeen:    time.Now().UTC().Truncate(time.Microsecond),
		Fingerprint: testFingerprint(`<html><body><p id="recent">recent content</p></body></html>`),
	}

	require.NoError(t, store.SavePattern(ctx, old))
	require.NoError(t, store.SavePattern(ctx, recent))

	err := store.DecayConfidence(ctx, 24*time.Hour, 0.5)
	require.NoError(t, err)

	gotOld, _ := store.Get(ctx, "old_pattern")
	assert.InDelta(t, 0.4, gotOld.Confidence, 0.01, "old pattern should be decayed")

	gotRecent, _ := store.Get(ctx, "recent_pattern")
	assert.InDelta(t, 0.8, gotRecent.Confidence, 0.01, "recent pattern should be unchanged")
}

func TestPostgresStore_DriftAlertLifecycle(t *testing.T) {
	pool, cleanup := setupPostgresContainer(t)
	defer cleanup()

	ctx := context.Background()
	store := NewPostgresPatternStore(pool)
	require.NoError(t, store.Migrate(ctx))

	alert := DriftAlert{
		PageID:      "login_page",
		PatternID:   "submit_btn",
		Severity:    DriftSeverityHigh,
		OldSelector: "#submit",
		NewSelector: ".new-submit",
		Similarity:  0.35,
	}
	require.NoError(t, store.InsertDriftAlert(ctx, alert))

	unresolved, err := store.UnresolvedAlerts(ctx)
	require.NoError(t, err)
	require.Len(t, unresolved, 1)
	assert.Equal(t, "login_page", unresolved[0].PageID)
	assert.Equal(t, DriftSeverityHigh, unresolved[0].Severity)
	assert.False(t, unresolved[0].Resolved)

	require.NoError(t, store.ResolveDriftAlert(ctx, unresolved[0].ID))

	remaining, err := store.UnresolvedAlerts(ctx)
	require.NoError(t, err)
	assert.Empty(t, remaining)
}

func TestPostgresStore_ModelHandoffRoundTrip(t *testing.T) {
	pool, cleanup := setupPostgresContainer(t)
	defer cleanup()

	ctx := context.Background()
	store := NewPostgresPatternStore(pool)
	require.NoError(t, store.Migrate(ctx))

	handoffs := []ModelHandoff{
		{PatternID: "btn_a", FromTier: "light", ToTier: "smart", Reason: "cache_miss", Success: true},
		{PatternID: "form_b", FromTier: "smart", ToTier: "vlm", Reason: "llm_fail", Success: false},
		{PatternID: "nav_c", FromTier: "light", ToTier: "smart", Reason: "drift", Success: true},
	}

	for _, h := range handoffs {
		require.NoError(t, store.InsertModelHandoff(ctx, h))
	}

	recent, err := store.RecentHandoffs(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, recent, 3)
	// Most recent first
	assert.Equal(t, "nav_c", recent[0].PatternID)
	assert.Equal(t, "form_b", recent[1].PatternID)
	assert.Equal(t, "btn_a", recent[2].PatternID)
}

func TestPostgresStore_GetNonexistent(t *testing.T) {
	pool, cleanup := setupPostgresContainer(t)
	defer cleanup()

	ctx := context.Background()
	store := NewPostgresPatternStore(pool)
	require.NoError(t, store.Migrate(ctx))

	_, ok := store.Get(ctx, "does_not_exist")
	assert.False(t, ok)
}

func TestPostgresStore_EmptyLoad(t *testing.T) {
	pool, cleanup := setupPostgresContainer(t)
	defer cleanup()

	ctx := context.Background()
	store := NewPostgresPatternStore(pool)
	require.NoError(t, store.Migrate(ctx))

	all, err := store.Load(ctx)
	require.NoError(t, err)
	assert.Empty(t, all)
}
