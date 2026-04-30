package uiauto

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/nfsarch33/uiauto-framework/pkg/domheal"
)

// ClusterID identifies a pattern cluster.
type ClusterID string

// PatternCluster groups similar UI patterns for aggregate analysis.
type PatternCluster struct {
	ID            ClusterID              `json:"id"`
	Label         string                 `json:"label"`
	Centroid      domheal.DOMFingerprint `json:"centroid"`
	Members       []string               `json:"members"` // Pattern IDs
	AvgConfidence float64                `json:"avg_confidence"`
	DriftScore    float64                `json:"drift_score"` // 0 = stable, 1 = high drift
	LastUpdated   time.Time              `json:"last_updated"`
}

// ClusterEngine performs single-linkage-like clustering on UI patterns.
type ClusterEngine struct {
	threshold float64 // similarity threshold to merge into a cluster
}

// NewClusterEngine creates a clustering engine. Threshold is the minimum
// fingerprint similarity (0..1) required to place two patterns in the same cluster.
func NewClusterEngine(threshold float64) *ClusterEngine {
	if threshold <= 0 || threshold > 1 {
		threshold = 0.6
	}
	return &ClusterEngine{threshold: threshold}
}

// Cluster groups the provided patterns based on fingerprint similarity.
func (ce *ClusterEngine) Cluster(patterns map[string]UIPattern) []PatternCluster {
	ids := make([]string, 0, len(patterns))
	for id := range patterns {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	assigned := make(map[string]int)
	var clusters []PatternCluster

	for _, id := range ids {
		if _, ok := assigned[id]; ok {
			continue
		}

		p := patterns[id]
		clusterIdx := len(clusters)
		cluster := PatternCluster{
			ID:          ClusterID("cluster-" + id),
			Label:       p.Description,
			Centroid:    p.Fingerprint,
			Members:     []string{id},
			LastUpdated: p.LastSeen,
		}
		assigned[id] = clusterIdx

		for _, othID := range ids {
			if _, ok := assigned[othID]; ok {
				continue
			}
			oth := patterns[othID]
			sim := domheal.DOMFingerprintSimilarity(p.Fingerprint, oth.Fingerprint)
			if sim >= ce.threshold {
				cluster.Members = append(cluster.Members, othID)
				assigned[othID] = clusterIdx
				if oth.LastSeen.After(cluster.LastUpdated) {
					cluster.LastUpdated = oth.LastSeen
				}
			}
		}

		cluster.AvgConfidence = ce.avgConfidence(cluster.Members, patterns)
		clusters = append(clusters, cluster)
	}

	return clusters
}

func (ce *ClusterEngine) avgConfidence(memberIDs []string, patterns map[string]UIPattern) float64 {
	if len(memberIDs) == 0 {
		return 0
	}
	var sum float64
	for _, id := range memberIDs {
		sum += patterns[id].Confidence
	}
	return sum / float64(len(memberIDs))
}

// DriftScorer computes a drift score for patterns based on their change history.
type DriftScorer struct {
	decayHalfLife time.Duration
}

// NewDriftScorer creates a scorer. decayHalfLife controls how fast old drift
// events lose weight (e.g., 7*24h means half-weight after 7 days).
func NewDriftScorer(decayHalfLife time.Duration) *DriftScorer {
	if decayHalfLife <= 0 {
		decayHalfLife = 7 * 24 * time.Hour
	}
	return &DriftScorer{decayHalfLife: decayHalfLife}
}

// DriftEvent records a single observed change at a point in time.
type DriftEvent struct {
	PageID    string    `json:"page_id"`
	Timestamp time.Time `json:"timestamp"`
	Magnitude float64   `json:"magnitude"` // 0..1 severity
}

// Score computes a drift score from a list of events. Each event's magnitude
// is scaled by an exponential decay weight based on its age, then summed. The
// result is capped at 1.0. This means a single old event produces a near-zero
// score, while recent high-magnitude events push the score toward 1.
func (ds *DriftScorer) Score(events []DriftEvent) float64 {
	if len(events) == 0 {
		return 0
	}
	now := time.Now()
	lambda := math.Ln2 / ds.decayHalfLife.Seconds()

	var score float64
	for _, e := range events {
		age := now.Sub(e.Timestamp).Seconds()
		if age < 0 {
			age = 0
		}
		w := math.Exp(-lambda * age)
		score += w * e.Magnitude
	}
	if score > 1 {
		return 1
	}
	return score
}

// ConfidenceScorer applies adaptive confidence based on usage history.
type ConfidenceScorer struct {
	boostPerSuccess float64
	decayPerFailure float64
	minConfidence   float64
	maxConfidence   float64
}

// NewConfidenceScorer creates a scorer with the given boost/decay parameters.
func NewConfidenceScorer(boostPerSuccess, decayPerFailure float64) *ConfidenceScorer {
	if boostPerSuccess <= 0 {
		boostPerSuccess = 0.05
	}
	if decayPerFailure <= 0 {
		decayPerFailure = 0.15
	}
	return &ConfidenceScorer{
		boostPerSuccess: boostPerSuccess,
		decayPerFailure: decayPerFailure,
		minConfidence:   0.05,
		maxConfidence:   1.0,
	}
}

// Update adjusts the confidence of a pattern after a selector evaluation.
func (cs *ConfidenceScorer) Update(current float64, success bool) float64 {
	if success {
		current += cs.boostPerSuccess
	} else {
		current -= cs.decayPerFailure
	}
	if current < cs.minConfidence {
		return cs.minConfidence
	}
	if current > cs.maxConfidence {
		return cs.maxConfidence
	}
	return current
}

// AdaptiveThreshold returns the acceptance threshold for a given confidence.
// High-confidence patterns require a lower match score; low-confidence
// patterns require a higher score to be accepted.
func (cs *ConfidenceScorer) AdaptiveThreshold(confidence float64) float64 {
	base := 0.6
	adjustment := 0.1 * (confidence - 0.5)
	threshold := base - adjustment
	if threshold < 0.40 {
		return 0.40
	}
	if threshold > 0.80 {
		return 0.80
	}
	return threshold
}

// PatternMLPipeline orchestrates clustering, drift scoring, and confidence
// updates across all tracked patterns.
type PatternMLPipeline struct {
	cluster    *ClusterEngine
	drift      *DriftScorer
	confidence *ConfidenceScorer
	store      PatternStorage
}

// NewPatternMLPipeline builds the ML pipeline from components.
func NewPatternMLPipeline(store PatternStorage, clusterThreshold float64, driftHalfLife time.Duration) *PatternMLPipeline {
	return &PatternMLPipeline{
		cluster:    NewClusterEngine(clusterThreshold),
		drift:      NewDriftScorer(driftHalfLife),
		confidence: NewConfidenceScorer(0.05, 0.15),
		store:      store,
	}
}

// ClusterResult holds a clustering run output.
type ClusterResult struct {
	Clusters      []PatternCluster `json:"clusters"`
	TotalPatterns int              `json:"total_patterns"`
	NumClusters   int              `json:"num_clusters"`
}

// RunClustering loads all patterns and clusters them.
func (p *PatternMLPipeline) RunClustering(ctx context.Context) (ClusterResult, error) {
	patterns, err := p.store.Load(ctx)
	if err != nil {
		return ClusterResult{}, err
	}
	clusters := p.cluster.Cluster(patterns)
	return ClusterResult{
		Clusters:      clusters,
		TotalPatterns: len(patterns),
		NumClusters:   len(clusters),
	}, nil
}

// ScoreDrift computes the weighted drift score from recent events.
func (p *PatternMLPipeline) ScoreDrift(events []DriftEvent) float64 {
	return p.drift.Score(events)
}

// UpdateConfidence adjusts a pattern's confidence and persists it.
func (p *PatternMLPipeline) UpdateConfidence(ctx context.Context, patternID string, success bool) error {
	pat, ok := p.store.Get(ctx, patternID)
	if !ok {
		return nil
	}
	pat.Confidence = p.confidence.Update(pat.Confidence, success)
	pat.LastSeen = time.Now()
	return p.store.Set(ctx, pat)
}

// AcceptableMatch returns whether a match similarity is above the adaptive
// threshold for the given pattern confidence.
func (p *PatternMLPipeline) AcceptableMatch(similarity, confidence float64) bool {
	return similarity >= p.confidence.AdaptiveThreshold(confidence)
}
