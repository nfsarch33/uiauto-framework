package visual

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"time"
)

// VLMProvider abstracts the VLM service for semantic comparison.
type VLMProvider interface {
	CompareImages(ctx context.Context, imageA, imageB string) (float64, string, error)
}

// SemanticDiffer uses a VLM to assess semantic visual similarity.
type SemanticDiffer struct {
	vlm    VLMProvider
	logger *slog.Logger
}

// NewSemanticDiffer creates a VLM-backed semantic comparator.
func NewSemanticDiffer(vlm VLMProvider, opts ...func(*SemanticDiffer)) *SemanticDiffer {
	d := &SemanticDiffer{vlm: vlm, logger: slog.Default()}
	for _, o := range opts {
		o(d)
	}
	return d
}

// Compare sends both images to the VLM and returns a semantic diff result.
func (d *SemanticDiffer) Compare(ctx context.Context, baseline, current []byte) (*SemanticDiffResult, error) {
	start := time.Now()

	b64A := base64.StdEncoding.EncodeToString(baseline)
	b64B := base64.StdEncoding.EncodeToString(current)

	similarity, desc, err := d.vlm.CompareImages(ctx, b64A, b64B)
	if err != nil {
		return nil, fmt.Errorf("semantic: vlm compare: %w", err)
	}

	level := classifySemanticDiff(similarity)
	result := &SemanticDiffResult{
		Similarity:  similarity,
		Description: desc,
		Level:       level,
		Duration:    time.Since(start),
	}

	d.logger.Info("semantic diff",
		slog.Float64("similarity", similarity),
		slog.String("level", level.String()),
		slog.Duration("duration", result.Duration),
	)

	return result, nil
}

func classifySemanticDiff(similarity float64) DiffLevel {
	switch {
	case similarity >= 0.99:
		return DiffNone
	case similarity >= 0.95:
		return DiffMinor
	case similarity >= 0.85:
		return DiffModerate
	case similarity >= 0.70:
		return DiffMajor
	default:
		return DiffCritical
	}
}

// StubVLMProvider returns deterministic responses for testing.
type StubVLMProvider struct {
	Similarity  float64
	Description string
	Err         error
}

// CompareImages returns the stub's predetermined response.
func (s *StubVLMProvider) CompareImages(_ context.Context, _, _ string) (float64, string, error) {
	return s.Similarity, s.Description, s.Err
}
