package evolver

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// LLMProvider is the interface the evolution engine uses to synthesise
// mutations from signals. It intentionally mirrors the llm.Provider shape.
type LLMProvider interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

// EvolutionEngineConfig controls the behaviour of the evolution engine.
type EvolutionEngineConfig struct {
	Mode               EvolutionMode
	MaxMutationsPerRun int
	AutoApplyLowRisk   bool
	DryRun             bool
}

// DefaultEngineConfig returns a balanced configuration with DryRun disabled.
// Safety is enforced at the IronEvolver level via SafetyChecks and MaxMutations.
func DefaultEngineConfig() EvolutionEngineConfig {
	return EvolutionEngineConfig{
		Mode:               ModeBalanced,
		MaxMutationsPerRun: 5,
		AutoApplyLowRisk:   false,
		DryRun:             false,
	}
}

// EvolutionEngine is the core meta-agent that processes signals, synthesises
// mutations, validates them, and manages the gene/capsule lifecycle.
type EvolutionEngine struct {
	mu           sync.Mutex
	cfg          EvolutionEngineConfig
	llm          LLMProvider
	genes        map[string]*Gene
	capsules     map[string]*Capsule
	mutations    []Mutation
	events       []EvolutionEvent
	eventCounter int
	mutCounter   int
}

// NewEvolutionEngine creates an engine with the given config and LLM provider.
func NewEvolutionEngine(cfg EvolutionEngineConfig, llm LLMProvider) *EvolutionEngine {
	return &EvolutionEngine{
		cfg:      cfg,
		llm:      llm,
		genes:    make(map[string]*Gene),
		capsules: make(map[string]*Capsule),
	}
}

// RegisterGene adds a gene to the engine's registry.
func (e *EvolutionEngine) RegisterGene(g Gene) error {
	if err := g.Validate(); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.genes[g.ID] = &g
	return nil
}

// RegisterCapsule adds a capsule to the engine's registry.
func (e *EvolutionEngine) RegisterCapsule(c Capsule) error {
	if err := c.Validate(); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.capsules[c.ID] = &c
	return nil
}

// Evolve processes a batch of signals and produces mutations.
func (e *EvolutionEngine) Evolve(ctx context.Context, signals []Signal) ([]Mutation, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	var produced []Mutation
	for _, sig := range signals {
		if len(produced) >= e.cfg.MaxMutationsPerRun {
			break
		}

		if !e.shouldProcess(sig) {
			continue
		}

		e.recordEvent(EventSignalDetected, "engine", "", sig)

		mut, err := e.synthesiseMutation(ctx, sig)
		if err != nil {
			e.recordEvent(EventMutationProposed, "engine", "", EventOutcome{Success: false, Reason: err.Error()})
			continue
		}

		e.mutations = append(e.mutations, *mut)
		produced = append(produced, *mut)
		e.recordEvent(EventMutationProposed, "engine", "", EventOutcome{Success: true})
	}

	return produced, nil
}

// shouldProcess filters signals based on the engine's evolution mode.
func (e *EvolutionEngine) shouldProcess(sig Signal) bool {
	switch e.cfg.Mode {
	case ModeRepairOnly:
		return sig.Severity == SeverityCritical
	case ModeHarden:
		return sig.Severity == SeverityCritical || sig.Severity == SeverityWarning
	case ModeBalanced:
		return true
	case ModeInnovate:
		return true
	default:
		return sig.Severity != SeverityInfo
	}
}

func (e *EvolutionEngine) synthesiseMutation(ctx context.Context, sig Signal) (*Mutation, error) {
	matchedGene := e.matchGene(sig)

	e.mutCounter++
	mut := &Mutation{
		ID:           fmt.Sprintf("mut-%06d", e.mutCounter),
		SignalID:     sig.ID,
		RiskEstimate: e.estimateRisk(sig),
		Strategy:     e.cfg.Mode,
		Status:       MutationStatusPending,
		CreatedAt:    time.Now().UTC(),
	}

	switch {
	case matchedGene != nil:
		mut.GeneID = matchedGene.ID
		mut.Reasoning = fmt.Sprintf("matched gene %q (%s) for signal %s", matchedGene.Name, matchedGene.Category, sig.Type)
	case e.llm != nil:
		prompt := fmt.Sprintf(
			"Signal detected: type=%s severity=%s description=%q. "+
				"Suggest a concrete fix as a JSON object with fields: reasoning (string), risk (low/medium/high). "+
				"Be specific about what code or config change would fix this.",
			sig.Type, sig.Severity, sig.Description,
		)
		resp, err := e.llm.Complete(ctx, prompt)
		if err != nil {
			return nil, fmt.Errorf("llm synthesis: %w", err)
		}
		mut.Reasoning = resp
	default:
		mut.Reasoning = sig.SuggestedMutation
		if mut.Reasoning == "" {
			mut.Reasoning = fmt.Sprintf("auto-generated for %s signal", sig.Type)
		}
	}

	return mut, nil
}

func (e *EvolutionEngine) matchGene(sig Signal) *Gene {
	for _, g := range e.genes {
		for _, tag := range g.Tags {
			if tag == string(sig.Type) {
				return g
			}
		}
	}
	return nil
}

func (e *EvolutionEngine) estimateRisk(sig Signal) RiskLevel {
	switch sig.Severity {
	case SeverityCritical:
		return RiskHigh
	case SeverityWarning:
		return RiskMedium
	default:
		return RiskLow
	}
}

func (e *EvolutionEngine) recordEvent(typ EventType, actor string, parent string, payload any) {
	e.eventCounter++
	data, _ := json.Marshal(payload)
	e.events = append(e.events, EvolutionEvent{
		ID:        fmt.Sprintf("evt-%06d", e.eventCounter),
		Timestamp: time.Now().UTC(),
		Type:      typ,
		ActorID:   actor,
		ParentID:  parent,
		Payload:   data,
	})
}

// Events returns the recorded evolution events.
func (e *EvolutionEngine) Events() []EvolutionEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]EvolutionEvent, len(e.events))
	copy(out, e.events)
	return out
}

// Mutations returns the generated mutations.
func (e *EvolutionEngine) Mutations() []Mutation {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Mutation, len(e.mutations))
	copy(out, e.mutations)
	return out
}

// Genes returns registered genes.
func (e *EvolutionEngine) Genes() map[string]*Gene {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make(map[string]*Gene, len(e.genes))
	for k, v := range e.genes {
		out[k] = v
	}
	return out
}
