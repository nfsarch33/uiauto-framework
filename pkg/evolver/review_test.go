package evolver

import (
	"context"
	"testing"
	"time"
)

func TestAutoApproveReviewer_LowRisk(t *testing.T) {
	r := &AutoApproveReviewer{MaxAutoApproveRisk: RiskMedium}
	resp, err := r.RequestReview(context.Background(), ReviewRequest{
		MutationID: "m1",
		RiskLevel:  RiskLow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Decision != ReviewApproved {
		t.Errorf("decision=%s want approved", resp.Decision)
	}
}

func TestAutoApproveReviewer_HighRisk(t *testing.T) {
	r := &AutoApproveReviewer{MaxAutoApproveRisk: RiskLow}
	resp, err := r.RequestReview(context.Background(), ReviewRequest{
		MutationID: "m2",
		RiskLevel:  RiskHigh,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Decision != ReviewDeferred {
		t.Errorf("decision=%s want deferred", resp.Decision)
	}
}

func TestAutoApproveReviewer_EqualRisk(t *testing.T) {
	r := &AutoApproveReviewer{MaxAutoApproveRisk: RiskMedium}
	resp, err := r.RequestReview(context.Background(), ReviewRequest{
		MutationID: "m3",
		RiskLevel:  RiskMedium,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Decision != ReviewApproved {
		t.Errorf("decision=%s want approved for equal risk", resp.Decision)
	}
}

func TestReviewGateway_ProcessMutations(t *testing.T) {
	store := newTestStore(t)
	reviewer := &AutoApproveReviewer{MaxAutoApproveRisk: RiskMedium}
	gw := NewReviewGateway(reviewer, store)
	ctx := context.Background()

	mutations := []Mutation{
		{
			ID:           "mut-low",
			Reasoning:    "safe tweak",
			RiskEstimate: RiskLow,
			Status:       MutationStatusPending,
			CreatedAt:    time.Now(),
		},
		{
			ID:           "mut-high",
			Reasoning:    "risky change",
			RiskEstimate: RiskHigh,
			Status:       MutationStatusPending,
			CreatedAt:    time.Now(),
		},
	}

	responses, err := gw.ProcessMutations(ctx, mutations)
	if err != nil {
		t.Fatal(err)
	}
	if len(responses) != 2 {
		t.Fatalf("len=%d want 2", len(responses))
	}
	if responses[0].Decision != ReviewApproved {
		t.Errorf("low-risk decision=%s want approved", responses[0].Decision)
	}
	if responses[1].Decision != ReviewDeferred {
		t.Errorf("high-risk decision=%s want deferred", responses[1].Decision)
	}

	events, err := store.ListEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Errorf("events=%d want 2", len(events))
	}
}

func TestReviewGateway_NilStore(t *testing.T) {
	reviewer := &AutoApproveReviewer{MaxAutoApproveRisk: RiskCritical}
	gw := NewReviewGateway(reviewer, nil)
	ctx := context.Background()

	mutations := []Mutation{{
		ID: "m1", RiskEstimate: RiskLow, Status: MutationStatusPending,
		CreatedAt: time.Now(),
	}}

	responses, err := gw.ProcessMutations(ctx, mutations)
	if err != nil {
		t.Fatal(err)
	}
	if len(responses) != 1 {
		t.Fatalf("len=%d want 1", len(responses))
	}
}

type failingReviewer struct{}

func (f *failingReviewer) RequestReview(_ context.Context, _ ReviewRequest) (*ReviewResponse, error) {
	return nil, context.DeadlineExceeded
}

func TestReviewGateway_ReviewerError(t *testing.T) {
	gw := NewReviewGateway(&failingReviewer{}, nil)
	ctx := context.Background()

	mutations := []Mutation{{
		ID: "m1", RiskEstimate: RiskLow, Status: MutationStatusPending,
		CreatedAt: time.Now(),
	}}

	_, err := gw.ProcessMutations(ctx, mutations)
	if err == nil {
		t.Error("expected error from failing reviewer")
	}
}
