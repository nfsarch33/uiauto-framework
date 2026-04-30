package uiauto

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresPatternStore implements PatternStorage backed by PostgreSQL.
type PostgresPatternStore struct {
	pool *pgxpool.Pool
}

// NewPostgresPatternStore creates a new pattern store using a pgxpool.
func NewPostgresPatternStore(pool *pgxpool.Pool) *PostgresPatternStore {
	return &PostgresPatternStore{pool: pool}
}

// Migrate creates the required tables for patterns, drift alerts, and handoff log.
func (s *PostgresPatternStore) Migrate(ctx context.Context) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS ui_patterns (
			id TEXT PRIMARY KEY,
			selector TEXT NOT NULL,
			description TEXT NOT NULL,
			confidence DOUBLE PRECISION NOT NULL,
			last_seen TIMESTAMP WITH TIME ZONE NOT NULL,
			fingerprint JSONB NOT NULL,
			metadata JSONB
		)`,
		`CREATE TABLE IF NOT EXISTS ui_drift_alerts (
			id BIGSERIAL PRIMARY KEY,
			page_id TEXT NOT NULL,
			pattern_id TEXT NOT NULL,
			severity TEXT NOT NULL,
			old_selector TEXT,
			new_selector TEXT,
			similarity DOUBLE PRECISION,
			resolved BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_drift_alerts_unresolved ON ui_drift_alerts (resolved, created_at DESC) WHERE NOT resolved`,
		`CREATE TABLE IF NOT EXISTS ui_model_handoffs (
			id BIGSERIAL PRIMARY KEY,
			pattern_id TEXT NOT NULL,
			from_tier TEXT NOT NULL,
			to_tier TEXT NOT NULL,
			reason TEXT,
			success BOOLEAN,
			created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
		)`,
	}
	for _, q := range queries {
		if _, err := s.pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("migrate %q: %w", q[:40], err)
		}
	}
	return nil
}

// Get retrieves a single pattern by ID.
func (s *PostgresPatternStore) Get(ctx context.Context, id string) (UIPattern, bool) {
	query := `SELECT id, selector, description, confidence, last_seen, fingerprint, metadata FROM ui_patterns WHERE id = $1`
	row := s.pool.QueryRow(ctx, query, id)

	var p UIPattern
	var fpData, metaData []byte
	if err := row.Scan(&p.ID, &p.Selector, &p.Description, &p.Confidence, &p.LastSeen, &fpData, &metaData); err != nil {
		return UIPattern{}, false
	}
	_ = json.Unmarshal(fpData, &p.Fingerprint)
	if len(metaData) > 0 {
		_ = json.Unmarshal(metaData, &p.Metadata)
	}
	return p, true
}

// Set upserts a single pattern (implements PatternStorage).
func (s *PostgresPatternStore) Set(ctx context.Context, pattern UIPattern) error {
	return s.SavePattern(ctx, pattern)
}

// Load returns all stored patterns.
func (s *PostgresPatternStore) Load(ctx context.Context) (map[string]UIPattern, error) {
	query := `SELECT id, selector, description, confidence, last_seen, fingerprint, metadata FROM ui_patterns`
	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	patterns := make(map[string]UIPattern)
	for rows.Next() {
		var p UIPattern
		var fpData, metaData []byte
		if err := rows.Scan(&p.ID, &p.Selector, &p.Description, &p.Confidence, &p.LastSeen, &fpData, &metaData); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(fpData, &p.Fingerprint)
		if len(metaData) > 0 {
			_ = json.Unmarshal(metaData, &p.Metadata)
		}
		patterns[p.ID] = p
	}
	return patterns, nil
}

// SavePattern upserts a single pattern.
func (s *PostgresPatternStore) SavePattern(ctx context.Context, p UIPattern) error {
	fpData, err := json.Marshal(p.Fingerprint)
	if err != nil {
		return err
	}
	metaData, err := json.Marshal(p.Metadata)
	if err != nil {
		return err
	}

	query := `INSERT INTO ui_patterns (id, selector, description, confidence, last_seen, fingerprint, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE SET
			selector = EXCLUDED.selector, description = EXCLUDED.description,
			confidence = EXCLUDED.confidence, last_seen = EXCLUDED.last_seen,
			fingerprint = EXCLUDED.fingerprint, metadata = EXCLUDED.metadata`
	_, err = s.pool.Exec(ctx, query, p.ID, p.Selector, p.Description, p.Confidence, p.LastSeen, fpData, metaData)
	return err
}

// DecayConfidence reduces the confidence of patterns that haven't been seen recently.
func (s *PostgresPatternStore) DecayConfidence(ctx context.Context, olderThan time.Duration, decayFactor float64) error {
	cutoff := time.Now().Add(-olderThan)
	query := `UPDATE ui_patterns SET confidence = confidence * $1 WHERE last_seen < $2 AND confidence > 0.1`
	_, err := s.pool.Exec(ctx, query, decayFactor, cutoff)
	return err
}

// BoostConfidence increases a pattern's confidence after successful use.
func (s *PostgresPatternStore) BoostConfidence(ctx context.Context, id string, boost float64) error {
	query := `UPDATE ui_patterns SET confidence = LEAST(confidence + $1, 1.0), last_seen = NOW() WHERE id = $2`
	tag, err := s.pool.Exec(ctx, query, boost, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("pattern not found: %s", id)
	}
	return nil
}

// --- Drift Alert System ---

// DriftSeverity classifies the impact of detected DOM drift.
type DriftSeverity string

// Drift severity levels based on fingerprint similarity score.
const (
	DriftSeverityLow      DriftSeverity = "low"
	DriftSeverityMedium   DriftSeverity = "medium"
	DriftSeverityHigh     DriftSeverity = "high"
	DriftSeverityCritical DriftSeverity = "critical"
)

// DriftAlert represents a detected DOM drift event requiring attention.
type DriftAlert struct {
	ID          int64         `json:"id"`
	PageID      string        `json:"page_id"`
	PatternID   string        `json:"pattern_id"`
	Severity    DriftSeverity `json:"severity"`
	OldSelector string        `json:"old_selector"`
	NewSelector string        `json:"new_selector"`
	Similarity  float64       `json:"similarity"`
	Resolved    bool          `json:"resolved"`
	CreatedAt   time.Time     `json:"created_at"`
}

// ClassifyDriftSeverity determines alert severity from similarity score.
func ClassifyDriftSeverity(similarity float64) DriftSeverity {
	switch {
	case similarity >= 0.8:
		return DriftSeverityLow
	case similarity >= 0.5:
		return DriftSeverityMedium
	case similarity >= 0.2:
		return DriftSeverityHigh
	default:
		return DriftSeverityCritical
	}
}

// InsertDriftAlert records a drift alert in PostgreSQL.
func (s *PostgresPatternStore) InsertDriftAlert(ctx context.Context, alert DriftAlert) error {
	query := `INSERT INTO ui_drift_alerts (page_id, pattern_id, severity, old_selector, new_selector, similarity)
		VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := s.pool.Exec(ctx, query, alert.PageID, alert.PatternID, string(alert.Severity),
		alert.OldSelector, alert.NewSelector, alert.Similarity)
	return err
}

// UnresolvedAlerts retrieves all unresolved drift alerts.
func (s *PostgresPatternStore) UnresolvedAlerts(ctx context.Context) ([]DriftAlert, error) {
	query := `SELECT id, page_id, pattern_id, severity, old_selector, new_selector, similarity, resolved, created_at
		FROM ui_drift_alerts WHERE NOT resolved ORDER BY created_at DESC`
	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var alerts []DriftAlert
	for rows.Next() {
		var a DriftAlert
		var sev string
		if err := rows.Scan(&a.ID, &a.PageID, &a.PatternID, &sev, &a.OldSelector, &a.NewSelector, &a.Similarity, &a.Resolved, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Severity = DriftSeverity(sev)
		alerts = append(alerts, a)
	}
	return alerts, nil
}

// ResolveDriftAlert marks a drift alert as resolved.
func (s *PostgresPatternStore) ResolveDriftAlert(ctx context.Context, alertID int64) error {
	query := `UPDATE ui_drift_alerts SET resolved = TRUE WHERE id = $1`
	_, err := s.pool.Exec(ctx, query, alertID)
	return err
}

// --- Model Handoff Tracking ---

// ModelHandoff records a tier escalation or demotion event.
type ModelHandoff struct {
	ID        int64     `json:"id"`
	PatternID string    `json:"pattern_id"`
	FromTier  string    `json:"from_tier"`
	ToTier    string    `json:"to_tier"`
	Reason    string    `json:"reason"`
	Success   bool      `json:"success"`
	CreatedAt time.Time `json:"created_at"`
}

// InsertModelHandoff records a model handoff in PostgreSQL.
func (s *PostgresPatternStore) InsertModelHandoff(ctx context.Context, h ModelHandoff) error {
	query := `INSERT INTO ui_model_handoffs (pattern_id, from_tier, to_tier, reason, success)
		VALUES ($1, $2, $3, $4, $5)`
	_, err := s.pool.Exec(ctx, query, h.PatternID, h.FromTier, h.ToTier, h.Reason, h.Success)
	return err
}

// RecentHandoffs returns recent model handoffs ordered by creation time.
func (s *PostgresPatternStore) RecentHandoffs(ctx context.Context, limit int) ([]ModelHandoff, error) {
	query := `SELECT id, pattern_id, from_tier, to_tier, reason, success, created_at
		FROM ui_model_handoffs ORDER BY created_at DESC LIMIT $1`
	rows, err := s.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var handoffs []ModelHandoff
	for rows.Next() {
		var h ModelHandoff
		if err := rows.Scan(&h.ID, &h.PatternID, &h.FromTier, &h.ToTier, &h.Reason, &h.Success, &h.CreatedAt); err != nil {
			return nil, err
		}
		handoffs = append(handoffs, h)
	}
	return handoffs, nil
}

// --- In-Memory Implementations for Testing ---

// InMemoryDriftAlertStore provides an in-memory drift alert store for testing.
type InMemoryDriftAlertStore struct {
	mu      sync.Mutex
	alerts  []DriftAlert
	nextID  int64
	handler DriftAlertHandler
}

// DriftAlertHandler is called when a new drift alert is created.
type DriftAlertHandler func(alert DriftAlert)

// NewInMemoryDriftAlertStore creates an in-memory drift alert store.
func NewInMemoryDriftAlertStore(handler DriftAlertHandler) *InMemoryDriftAlertStore {
	return &InMemoryDriftAlertStore{handler: handler}
}

// Insert adds a drift alert.
func (s *InMemoryDriftAlertStore) Insert(alert DriftAlert) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	alert.ID = s.nextID
	alert.CreatedAt = time.Now()
	s.alerts = append(s.alerts, alert)
	if s.handler != nil {
		s.handler(alert)
	}
}

// Unresolved returns alerts that haven't been resolved.
func (s *InMemoryDriftAlertStore) Unresolved() []DriftAlert {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []DriftAlert
	for _, a := range s.alerts {
		if !a.Resolved {
			out = append(out, a)
		}
	}
	return out
}

// Resolve marks an alert as resolved.
func (s *InMemoryDriftAlertStore) Resolve(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.alerts {
		if s.alerts[i].ID == id {
			s.alerts[i].Resolved = true
			return
		}
	}
}

// InMemoryHandoffStore provides an in-memory model handoff store for testing.
type InMemoryHandoffStore struct {
	mu       sync.Mutex
	handoffs []ModelHandoff
	nextID   int64
}

// NewInMemoryHandoffStore creates an in-memory handoff store.
func NewInMemoryHandoffStore() *InMemoryHandoffStore {
	return &InMemoryHandoffStore{}
}

// Insert adds a handoff record.
func (s *InMemoryHandoffStore) Insert(h ModelHandoff) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	h.ID = s.nextID
	h.CreatedAt = time.Now()
	s.handoffs = append(s.handoffs, h)
}

// Recent returns the last N handoff records.
func (s *InMemoryHandoffStore) Recent(limit int) []ModelHandoff {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit > len(s.handoffs) {
		limit = len(s.handoffs)
	}
	out := make([]ModelHandoff, limit)
	for i := 0; i < limit; i++ {
		out[i] = s.handoffs[len(s.handoffs)-1-i]
	}
	return out
}

// PatternPipeline orchestrates pattern tracking with drift alerts and model handoff.
type PatternPipeline struct {
	tracker  *PatternTracker
	alerts   *InMemoryDriftAlertStore
	handoffs *InMemoryHandoffStore
	logger   *slog.Logger
}

// NewPatternPipeline creates a production-ready pattern tracking pipeline.
func NewPatternPipeline(tracker *PatternTracker, logger *slog.Logger) *PatternPipeline {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &PatternPipeline{
		tracker:  tracker,
		alerts:   NewInMemoryDriftAlertStore(nil),
		handoffs: NewInMemoryHandoffStore(),
		logger:   logger,
	}
}

// WithAlertHandler sets a callback for new drift alerts.
func (pp *PatternPipeline) WithAlertHandler(handler DriftAlertHandler) {
	pp.alerts.handler = handler
}

// CheckAndAlert checks for drift and creates alerts if detected.
func (pp *PatternPipeline) CheckAndAlert(ctx context.Context, pageID, patternID, html string) (bool, error) {
	drifted, err := pp.tracker.CheckDrift(pageID, html)
	if err != nil {
		return false, err
	}

	if drifted {
		pattern, ok := pp.tracker.store.Get(ctx, patternID)
		similarity := 0.0
		oldSel := ""
		if ok {
			similarity = pattern.Confidence
			oldSel = pattern.Selector
		}

		alert := DriftAlert{
			PageID:      pageID,
			PatternID:   patternID,
			Severity:    ClassifyDriftSeverity(similarity),
			OldSelector: oldSel,
			Similarity:  similarity,
		}
		pp.alerts.Insert(alert)

		pp.logger.Warn("drift detected",
			"page", pageID,
			"pattern", patternID,
			"severity", string(alert.Severity),
		)
	}

	return drifted, nil
}

// RecordHandoff logs a model tier transition.
func (pp *PatternPipeline) RecordHandoff(patternID, fromTier, toTier, reason string, success bool) {
	pp.handoffs.Insert(ModelHandoff{
		PatternID: patternID,
		FromTier:  fromTier,
		ToTier:    toTier,
		Reason:    reason,
		Success:   success,
	})
}

// UnresolvedAlerts returns current unresolved drift alerts.
func (pp *PatternPipeline) UnresolvedAlerts() []DriftAlert {
	return pp.alerts.Unresolved()
}

// RecentHandoffs returns recent model handoff records.
func (pp *PatternPipeline) RecentHandoffs(limit int) []ModelHandoff {
	return pp.handoffs.Recent(limit)
}

// ResolveAlert marks a drift alert as resolved.
func (pp *PatternPipeline) ResolveAlert(id int64) {
	pp.alerts.Resolve(id)
}
