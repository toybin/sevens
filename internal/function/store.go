package function

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"sevens/internal/triple"
)

// Pipeline store predicates. These live in the function package because
// they describe pipeline state, not KB domain entities.
const (
	PredPipelineRoot         = "pipeline/root"
	PredPipelineFunction     = "pipeline/function"
	PredPipelineTarget       = "pipeline/target"
	PredPipelinePhase        = "pipeline/phase"
	PredPipelineStep         = "pipeline/step"
	PredPipelineResult       = "pipeline/result"
	PredPipelineAccumulator  = "pipeline/accumulator"
	PredPipelinePriorResults = "pipeline/prior-results"
	PredPipelineRevisions    = "pipeline/revisions"
	PredPipelineBackend      = "pipeline/backend"
)

// PipelineStore persists pipeline state as triples.
type PipelineStore struct {
	store *triple.Store
}

// NewPipelineStore creates a store backed by a triple.Store.
func NewPipelineStore(s *triple.Store) *PipelineStore {
	return &PipelineStore{store: s}
}

// Save writes or overwrites all pipeline state triples.
func (ps *PipelineStore) Save(ctx context.Context, p *Pipeline) error {
	// Retract existing state for this pipeline ID
	if err := ps.store.RetractBySubject(ctx, p.ID); err != nil {
		return fmt.Errorf("retracting old pipeline state: %w", err)
	}

	triples := []triple.Triple{
		{Subject: p.ID, Predicate: PredPipelineRoot, Object: p.Root},
		{Subject: p.ID, Predicate: PredPipelineFunction, Object: p.FunctionName},
		{Subject: p.ID, Predicate: PredPipelineTarget, Object: p.Target},
		{Subject: p.ID, Predicate: PredPipelinePhase, Object: p.Phase.String()},
		{Subject: p.ID, Predicate: PredPipelineStep, Object: strconv.Itoa(p.CurrentStep)},
	}
	if p.BackendName != "" {
		triples = append(triples, triple.Triple{
			Subject: p.ID, Predicate: PredPipelineBackend, Object: p.BackendName,
		})
	}

	if p.CurrentResult != nil {
		resultJSON, err := json.Marshal(p.CurrentResult)
		if err != nil {
			return fmt.Errorf("marshalling current result: %w", err)
		}
		triples = append(triples, triple.Triple{
			Subject: p.ID, Predicate: PredPipelineResult, Object: string(resultJSON),
		})
	}

	if p.Accumulator != nil {
		accJSON, err := json.Marshal(p.Accumulator)
		if err != nil {
			return fmt.Errorf("marshalling accumulator: %w", err)
		}
		triples = append(triples, triple.Triple{
			Subject: p.ID, Predicate: PredPipelineAccumulator, Object: string(accJSON),
		})
	}

	if len(p.PriorStepResults) > 0 {
		priorJSON, err := json.Marshal(p.PriorStepResults)
		if err != nil {
			return fmt.Errorf("marshalling prior results: %w", err)
		}
		triples = append(triples, triple.Triple{
			Subject: p.ID, Predicate: PredPipelinePriorResults, Object: string(priorJSON),
		})
	}

	if len(p.RevisionChain) > 0 {
		revJSON, err := json.Marshal(p.RevisionChain)
		if err != nil {
			return fmt.Errorf("marshalling revision chain: %w", err)
		}
		triples = append(triples, triple.Triple{
			Subject: p.ID, Predicate: PredPipelineRevisions, Object: string(revJSON),
		})
	}

	return ps.store.AssertBatch(ctx, triples)
}

// Load reconstructs a Pipeline from its stored triples.
func (ps *PipelineStore) Load(ctx context.Context, id string) (*Pipeline, error) {
	triples, err := ps.store.BySubject(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(triples) == 0 {
		return nil, fmt.Errorf("pipeline %q not found", id)
	}

	p := &Pipeline{ID: id}
	for _, t := range triples {
		switch t.Predicate {
		case PredPipelineRoot:
			p.Root = t.Object
		case PredPipelineFunction:
			p.FunctionName = t.Object
		case PredPipelineTarget:
			p.Target = t.Object
		case PredPipelinePhase:
			p.Phase = parsePhase(t.Object)
		case PredPipelineStep:
			p.CurrentStep, _ = strconv.Atoi(t.Object)
		case PredPipelineResult:
			var r TransformResult
			if err := json.Unmarshal([]byte(t.Object), &r); err == nil {
				p.CurrentResult = &r
			}
		case PredPipelineAccumulator:
			var r TransformResult
			if err := json.Unmarshal([]byte(t.Object), &r); err == nil {
				p.Accumulator = &r
			}
		case PredPipelinePriorResults:
			var results []TransformResult
			if err := json.Unmarshal([]byte(t.Object), &results); err == nil {
				p.PriorStepResults = results
			}
		case PredPipelineRevisions:
			var revisions []RevisionEntry
			if err := json.Unmarshal([]byte(t.Object), &revisions); err == nil {
				p.RevisionChain = revisions
			}
		case PredPipelineBackend:
			p.BackendName = t.Object
		}
	}

	return p, nil
}

// FindPending returns all pipelines in the Pending phase, optionally filtered by root.
func (ps *PipelineStore) FindPending(ctx context.Context, root string) ([]*Pipeline, error) {
	subjects, err := ps.store.ByPredicateObject(ctx, PredPipelinePhase, PhasePending.String())
	if err != nil {
		return nil, err
	}

	var result []*Pipeline
	for _, id := range subjects {
		p, err := ps.Load(ctx, id)
		if err != nil {
			continue
		}
		if root != "" && p.Root != root {
			continue
		}
		result = append(result, p)
	}
	return result, nil
}

// Delete removes all triples for a pipeline.
func (ps *PipelineStore) Delete(ctx context.Context, id string) error {
	return ps.store.RetractBySubject(ctx, id)
}

func parsePhase(s string) PipelinePhase {
	switch s {
	case "Running":
		return PhaseRunning
	case "Pending":
		return PhasePending
	case "Accepted":
		return PhaseAccepted
	case "Looping":
		return PhaseLooping
	case "Rejected":
		return PhaseRejected
	case "Cancelled":
		return PhaseCancelled
	case "Completed":
		return PhaseCompleted
	default:
		return PhaseRunning
	}
}
