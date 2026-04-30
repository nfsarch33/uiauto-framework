package visual

import (
	"context"
	"time"
)

// DiffLevel classifies the severity of visual differences.
type DiffLevel int

const (
	DiffNone     DiffLevel = iota // identical
	DiffMinor                     // < 1% pixel change
	DiffModerate                  // 1-5% pixel change
	DiffMajor                     // 5-20% pixel change
	DiffCritical                  // > 20% pixel change
)

func (d DiffLevel) String() string {
	switch d {
	case DiffNone:
		return "none"
	case DiffMinor:
		return "minor"
	case DiffModerate:
		return "moderate"
	case DiffMajor:
		return "major"
	case DiffCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// PixelDiffResult contains pixel-level comparison metrics.
type PixelDiffResult struct {
	TotalPixels    int
	DiffPixels     int
	DiffPercentage float64
	Level          DiffLevel
	MaxRegionDiff  BBox
	Duration       time.Duration
}

// BBox is a bounding rectangle for the largest diff region.
type BBox struct {
	X, Y, W, H int
}

// SemanticDiffResult captures VLM-based semantic analysis.
type SemanticDiffResult struct {
	Similarity  float64
	Description string
	Level       DiffLevel
	Duration    time.Duration
}

// CompositeResult combines pixel and semantic analysis.
type CompositeResult struct {
	Pixel    *PixelDiffResult
	Semantic *SemanticDiffResult
	Score    float64
	Level    DiffLevel
	Pass     bool
}

// Verifier compares two screenshots for visual regression.
type Verifier interface {
	ComparePixels(ctx context.Context, baseline, current []byte) (*PixelDiffResult, error)
	CompareSemantic(ctx context.Context, baseline, current []byte) (*SemanticDiffResult, error)
	Compare(ctx context.Context, baseline, current []byte) (*CompositeResult, error)
}

// Config controls verification thresholds.
type Config struct {
	PixelThreshold    float64 // max allowed pixel diff percentage (default 1.0)
	SemanticThreshold float64 // min semantic similarity (default 0.85)
	PixelWeight       float64 // weight for pixel score in composite (default 0.6)
	SemanticWeight    float64 // weight for semantic score in composite (default 0.4)
}

// DefaultConfig returns sensible verification defaults.
func DefaultConfig() Config {
	return Config{
		PixelThreshold:    1.0,
		SemanticThreshold: 0.85,
		PixelWeight:       0.6,
		SemanticWeight:    0.4,
	}
}
