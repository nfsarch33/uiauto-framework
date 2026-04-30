package evolver

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// ReviewDecision represents a human-in-the-loop decision on a mutation.
type ReviewDecision string

// ReviewDecision values.
const (
	ReviewApproved ReviewDecision = "approved"
	ReviewRejected ReviewDecision = "rejected"
	ReviewDeferred ReviewDecision = "deferred"
)

// ReviewRequest is presented to a human reviewer.
type ReviewRequest struct {
	MutationID string    `json:"mutation_id"`
	SignalID   string    `json:"signal_id"`
	Reasoning  string    `json:"reasoning"`
	RiskLevel  RiskLevel `json:"risk_level"`
	Strategy   string    `json:"strategy"`
	CreatedAt  time.Time `json:"created_at"`
}

// ReviewResponse captures the human's decision.
type ReviewResponse struct {
	MutationID string         `json:"mutation_id"`
	Decision   ReviewDecision `json:"decision"`
	Comment    string         `json:"comment,omitempty"`
	ReviewedAt time.Time      `json:"reviewed_at"`
	ReviewerID string         `json:"reviewer_id,omitempty"`
}

// Reviewer abstracts the human-in-the-loop approval channel.
type Reviewer interface {
	RequestReview(ctx context.Context, req ReviewRequest) (*ReviewResponse, error)
}

// AutoApproveReviewer automatically approves low-risk mutations and
// defers high-risk ones. Useful for testing and autonomous modes.
type AutoApproveReviewer struct {
	MaxAutoApproveRisk RiskLevel
}

// RequestReview auto-decides based on risk level.
func (r *AutoApproveReviewer) RequestReview(_ context.Context, req ReviewRequest) (*ReviewResponse, error) {
	decision := ReviewApproved
	if riskOrd(req.RiskLevel) > riskOrd(r.MaxAutoApproveRisk) {
		decision = ReviewDeferred
	}
	return &ReviewResponse{
		MutationID: req.MutationID,
		Decision:   decision,
		Comment:    fmt.Sprintf("auto-review: risk=%s threshold=%s", req.RiskLevel, r.MaxAutoApproveRisk),
		ReviewedAt: time.Now(),
		ReviewerID: "auto-reviewer",
	}, nil
}

func riskOrd(r RiskLevel) int {
	switch r {
	case RiskLow:
		return 0
	case RiskMedium:
		return 1
	case RiskHigh:
		return 2
	case RiskCritical:
		return 3
	default:
		return 4
	}
}

// ReviewGateway coordinates mutation review and applies approved mutations.
type ReviewGateway struct {
	reviewer Reviewer
	store    *CapsuleStore
}

// NewReviewGateway creates a gateway with the given reviewer and store.
func NewReviewGateway(reviewer Reviewer, store *CapsuleStore) *ReviewGateway {
	return &ReviewGateway{reviewer: reviewer, store: store}
}

// ProcessMutations sends each mutation through review and records decisions
// as evolution events.
func (g *ReviewGateway) ProcessMutations(ctx context.Context, mutations []Mutation) ([]ReviewResponse, error) {
	var responses []ReviewResponse
	for _, mut := range mutations {
		req := ReviewRequest{
			MutationID: mut.ID,
			SignalID:   mut.SignalID,
			Reasoning:  mut.Reasoning,
			RiskLevel:  mut.RiskEstimate,
			Strategy:   string(mut.Strategy),
			CreatedAt:  mut.CreatedAt,
		}
		resp, err := g.reviewer.RequestReview(ctx, req)
		if err != nil {
			return responses, fmt.Errorf("review mutation %s: %w", mut.ID, err)
		}

		eventType := EventMutationProposed
		mutStatus := MutationStatusRejected
		switch resp.Decision {
		case ReviewApproved:
			eventType = EventGeneApplied
			mutStatus = MutationStatusApplied
		case ReviewDeferred:
			eventType = EventMutationProposed
			mutStatus = MutationStatusPending
		}

		payload, _ := json.Marshal(map[string]string{
			"mutation_id": mut.ID,
			"decision":    string(resp.Decision),
			"comment":     resp.Comment,
			"reviewer":    resp.ReviewerID,
		})

		event := &EvolutionEvent{
			ID:        fmt.Sprintf("rev-%s-%d", mut.ID, time.Now().UnixMilli()),
			Type:      eventType,
			Timestamp: time.Now(),
			Payload:   payload,
			Outcome:   EventOutcome{Success: resp.Decision == ReviewApproved},
		}

		if g.store != nil {
			_ = g.store.SaveEvent(ctx, event)
		}

		mut.Status = mutStatus
		responses = append(responses, *resp)
	}
	return responses, nil
}
