package frame

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// SelectorCandidate is a proposed replacement selector within a frame.
type SelectorCandidate struct {
	Selector   string
	Confidence float64
	Method     string
	FrameID    string
}

// HealResult records a cross-frame healing attempt.
type HealResult struct {
	OriginalSelector string
	FramePath        []string
	BestCandidate    *SelectorCandidate
	Candidates       []SelectorCandidate
	Duration         time.Duration
	Success          bool
}

// SelectorResolver can find element replacements within a frame context.
type SelectorResolver interface {
	ResolveCandidates(ctx context.Context, frameHTML string, oldSelector string) ([]SelectorCandidate, error)
}

// FrameNavigator abstracts browser iframe navigation.
type FrameNavigator interface {
	EnterFrame(ctx context.Context, path []string) error
	ExitToTop(ctx context.Context) error
	CaptureFrameDOM(ctx context.Context) (string, error)
}

// FrameHealer performs self-healing across frame boundaries.
type FrameHealer struct {
	tree     *FrameTree
	nav      FrameNavigator
	resolver SelectorResolver
	logger   *slog.Logger
}

// NewFrameHealer creates a healer for the given frame tree.
func NewFrameHealer(tree *FrameTree, nav FrameNavigator, resolver SelectorResolver) *FrameHealer {
	return &FrameHealer{
		tree:     tree,
		nav:      nav,
		resolver: resolver,
		logger:   slog.Default(),
	}
}

// Heal attempts to find a replacement selector within the specified frame.
func (fh *FrameHealer) Heal(ctx context.Context, frameID, oldSelector string) (*HealResult, error) {
	start := time.Now()

	frame, ok := fh.tree.Frames[frameID]
	if !ok {
		return nil, fmt.Errorf("frame healer: frame %q not found", frameID)
	}

	path := frame.Path()

	if err := fh.nav.EnterFrame(ctx, path); err != nil {
		return nil, fmt.Errorf("frame healer: enter frame %q: %w", frameID, err)
	}
	defer fh.nav.ExitToTop(ctx)

	html, err := fh.nav.CaptureFrameDOM(ctx)
	if err != nil {
		return nil, fmt.Errorf("frame healer: capture DOM in %q: %w", frameID, err)
	}

	candidates, err := fh.resolver.ResolveCandidates(ctx, html, oldSelector)
	if err != nil {
		return nil, fmt.Errorf("frame healer: resolve in %q: %w", frameID, err)
	}

	result := &HealResult{
		OriginalSelector: oldSelector,
		FramePath:        path,
		Candidates:       candidates,
		Duration:         time.Since(start),
	}

	if len(candidates) > 0 {
		best := candidates[0]
		for _, c := range candidates[1:] {
			if c.Confidence > best.Confidence {
				best = c
			}
		}
		result.BestCandidate = &best
		result.Success = best.Confidence >= 0.5
	}

	fh.logger.Info("frame heal",
		slog.String("frame", frameID),
		slog.String("selector", oldSelector),
		slog.Int("candidates", len(candidates)),
		slog.Bool("success", result.Success),
		slog.Duration("duration", result.Duration),
	)

	return result, nil
}
