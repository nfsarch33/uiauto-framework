package visual

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// CompositeVerifier implements Verifier using pixel + semantic analysis.
type CompositeVerifier struct {
	pixel    *PixelDiffer
	semantic *SemanticDiffer
	config   Config
	logger   *slog.Logger
}

// NewCompositeVerifier creates a verifier with pixel and semantic backends.
func NewCompositeVerifier(pixel *PixelDiffer, semantic *SemanticDiffer, cfg Config) *CompositeVerifier {
	return &CompositeVerifier{
		pixel:    pixel,
		semantic: semantic,
		config:   cfg,
		logger:   slog.Default(),
	}
}

// ComparePixels delegates to the pixel differ.
func (v *CompositeVerifier) ComparePixels(ctx context.Context, baseline, current []byte) (*PixelDiffResult, error) {
	return v.pixel.Compare(ctx, baseline, current)
}

// CompareSemantic delegates to the semantic differ.
func (v *CompositeVerifier) CompareSemantic(ctx context.Context, baseline, current []byte) (*SemanticDiffResult, error) {
	if v.semantic == nil {
		return nil, fmt.Errorf("scorer: no semantic provider configured")
	}
	return v.semantic.Compare(ctx, baseline, current)
}

// Compare runs both pixel and semantic analysis, producing a weighted composite score.
func (v *CompositeVerifier) Compare(ctx context.Context, baseline, current []byte) (*CompositeResult, error) {
	start := time.Now()

	pixelResult, err := v.pixel.Compare(ctx, baseline, current)
	if err != nil {
		return nil, fmt.Errorf("scorer: pixel: %w", err)
	}

	result := &CompositeResult{Pixel: pixelResult}

	pixelScore := 1.0 - (pixelResult.DiffPercentage / 100.0)
	if pixelScore < 0 {
		pixelScore = 0
	}

	if v.semantic != nil {
		semResult, err := v.semantic.Compare(ctx, baseline, current)
		if err != nil {
			v.logger.Warn("semantic diff failed, using pixel-only score", slog.String("error", err.Error()))
			result.Score = pixelScore
		} else {
			result.Semantic = semResult
			result.Score = v.config.PixelWeight*pixelScore + v.config.SemanticWeight*semResult.Similarity
		}
	} else {
		result.Score = pixelScore
	}

	result.Level = classifyCompositeScore(result.Score)
	result.Pass = pixelResult.DiffPercentage <= v.config.PixelThreshold &&
		(result.Semantic == nil || result.Semantic.Similarity >= v.config.SemanticThreshold)

	v.logger.Info("composite verify",
		slog.Float64("score", result.Score),
		slog.Bool("pass", result.Pass),
		slog.Duration("duration", time.Since(start)),
	)

	return result, nil
}

func classifyCompositeScore(score float64) DiffLevel {
	switch {
	case score >= 0.99:
		return DiffNone
	case score >= 0.95:
		return DiffMinor
	case score >= 0.85:
		return DiffModerate
	case score >= 0.70:
		return DiffMajor
	default:
		return DiffCritical
	}
}
