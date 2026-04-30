package evolver

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"
)

// PersistentFleetCoordinator extends FleetCoordinator with durable storage.
// It wraps in-memory operations and persists all mutations through the Store interface.
type PersistentFleetCoordinator struct {
	store  Store
	logger *slog.Logger
	fleet  *FleetCoordinator
}

// NewPersistentFleetCoordinator creates a fleet coordinator backed by persistent storage.
func NewPersistentFleetCoordinator(store Store, logger *slog.Logger) (*PersistentFleetCoordinator, error) {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	pfc := &PersistentFleetCoordinator{
		store:  store,
		logger: logger,
		fleet:  NewFleetCoordinator(),
	}
	if err := pfc.loadState(context.Background()); err != nil {
		logger.Warn("loading persisted fleet state", "error", err)
	}
	return pfc, nil
}

func (pfc *PersistentFleetCoordinator) loadState(ctx context.Context) error {
	nodes, err := pfc.store.ListFleetNodes(ctx)
	if err != nil {
		return fmt.Errorf("load fleet nodes: %w", err)
	}
	for i := range nodes {
		pfc.fleet.RegisterNode(nodes[i])
	}
	pfc.logger.Info("fleet state loaded", "nodes", len(nodes))
	return nil
}

// RegisterNode adds a node and persists it.
func (pfc *PersistentFleetCoordinator) RegisterNode(ctx context.Context, node FleetNode) error {
	pfc.fleet.RegisterNode(node)
	node.LastSeen = time.Now()
	node.Status = "online"
	if err := pfc.store.SaveFleetNode(ctx, &node); err != nil {
		pfc.logger.Error("persist fleet node", "error", err)
		return err
	}
	return nil
}

// SharePattern shares a pattern and persists the share record.
func (pfc *PersistentFleetCoordinator) SharePattern(ctx context.Context, share PatternShare) error {
	if err := pfc.fleet.SharePattern(ctx, share); err != nil {
		return err
	}
	share.Timestamp = time.Now()
	if err := pfc.store.SavePatternShare(ctx, &share); err != nil {
		pfc.logger.Error("persist pattern share", "error", err)
		return err
	}
	return nil
}

// DelegateTask delegates a task and persists the delegation.
func (pfc *PersistentFleetCoordinator) DelegateTask(ctx context.Context, delegation TaskDelegation) error {
	if err := pfc.fleet.DelegateTask(ctx, delegation); err != nil {
		return err
	}
	delegation.CreatedAt = time.Now()
	delegation.Status = "pending"
	if err := pfc.store.SaveTaskDelegation(ctx, &delegation); err != nil {
		pfc.logger.Error("persist task delegation", "error", err)
		return err
	}
	return nil
}

// Nodes returns all registered fleet nodes.
func (pfc *PersistentFleetCoordinator) Nodes() []*FleetNode {
	return pfc.fleet.Nodes()
}

// Shares returns all pattern shares.
func (pfc *PersistentFleetCoordinator) Shares() []PatternShare {
	return pfc.fleet.Shares()
}

// Delegations returns all task delegations.
func (pfc *PersistentFleetCoordinator) Delegations() []TaskDelegation {
	return pfc.fleet.Delegations()
}

// PersistentFeedbackLoop extends FeedbackLoop with durable storage.
type PersistentFeedbackLoop struct {
	store  Store
	logger *slog.Logger
	loop   *FeedbackLoop
}

// NewPersistentFeedbackLoop creates a feedback loop backed by persistent storage.
func NewPersistentFeedbackLoop(store Store, logger *slog.Logger) *PersistentFeedbackLoop {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &PersistentFeedbackLoop{
		store:  store,
		logger: logger,
		loop:   NewFeedbackLoop(),
	}
}

// IngestSignal processes a signal and persists it.
func (pfl *PersistentFeedbackLoop) IngestSignal(ctx context.Context, signal FeedbackSignal) error {
	pfl.loop.IngestSignal(signal)
	if signal.Timestamp.IsZero() {
		signal.Timestamp = time.Now()
	}
	if err := pfl.store.SaveFeedbackSignal(ctx, &signal); err != nil {
		pfl.logger.Error("persist feedback signal", "error", err)
		return err
	}
	return nil
}

// Evaluate checks signals and persists generated actions.
func (pfl *PersistentFeedbackLoop) Evaluate(ctx context.Context) ([]FeedbackAction, error) {
	actions := pfl.loop.Evaluate(ctx)
	for i := range actions {
		if err := pfl.store.SaveFeedbackAction(ctx, &actions[i]); err != nil {
			pfl.logger.Error("persist feedback action", "error", err)
		}
	}
	return actions, nil
}

// ApplyAction marks an action as applied.
func (pfl *PersistentFeedbackLoop) ApplyAction(idx int) {
	pfl.loop.ApplyAction(idx)
}

// Signals returns all ingested signals.
func (pfl *PersistentFeedbackLoop) Signals() []FeedbackSignal {
	return pfl.loop.Signals()
}

// Actions returns all generated actions.
func (pfl *PersistentFeedbackLoop) Actions() []FeedbackAction {
	return pfl.loop.Actions()
}
