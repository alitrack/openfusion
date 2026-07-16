package guard

import (
	"context"
	"fmt"
	"sync"

	"github.com/lhy/openfusion/internal/types"
)

// PipelineResult holds the aggregated results of a guard pipeline run.
type PipelineResult struct {
	Blocked    bool          `json:"blocked"`
	Results    []GuardResult `json:"results"`
	FirstBlock *GuardResult  `json:"first_block,omitempty"`
}

// GuardPipeline chains multiple Guards together and runs them sequentially.
// It collects all results and blocks on the first "block" action encountered.
type GuardPipeline struct {
	mu     sync.RWMutex
	guards []Guard
}

// NewPipeline creates a new empty GuardPipeline.
func NewPipeline(guards ...Guard) *GuardPipeline {
	return &GuardPipeline{guards: guards}
}

// Add appends a guard to the pipeline.
func (p *GuardPipeline) Add(g Guard) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.guards = append(p.guards, g)
}

// Len returns the number of guards in the pipeline.
func (p *GuardPipeline) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.guards)
}

// Guards returns a snapshot of the current guards.
func (p *GuardPipeline) Guards() []Guard {
	p.mu.RLock()
	defer p.mu.RUnlock()
	cp := make([]Guard, len(p.guards))
	copy(cp, p.guards)
	return cp
}

// CheckInput runs all guards' CheckInput methods sequentially.
// It collects all results and blocks (returns error) on the first "block" action.
// Non-blocking actions (warn, redact, log) are collected but do not interrupt.
func (p *GuardPipeline) CheckInput(ctx context.Context, req *types.ChatRequest) (*PipelineResult, error) {
	p.mu.RLock()
	guards := make([]Guard, len(p.guards))
	copy(guards, p.guards)
	p.mu.RUnlock()

	result := &PipelineResult{Blocked: false, Results: make([]GuardResult, 0, len(guards))}

	for _, g := range guards {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		gr, err := g.CheckInput(ctx, req)
		if err != nil {
			return result, fmt.Errorf("guard %q input check: %w", g.Name(), err)
		}
		if gr == nil {
			continue
		}

		gr.GuardName = g.Name()
		result.Results = append(result.Results, *gr)

		if gr.Action == ActionBlock {
			result.Blocked = true
			result.FirstBlock = gr
			return result, fmt.Errorf("guard %q blocked input: %s", g.Name(), gr.Reason)
		}
	}

	return result, nil
}

// CheckOutput runs all guards' CheckOutput methods sequentially.
// It collects all results and blocks on the first "block" action.
func (p *GuardPipeline) CheckOutput(ctx context.Context, resp *types.ChatResponse) (*PipelineResult, error) {
	p.mu.RLock()
	guards := make([]Guard, len(p.guards))
	copy(guards, p.guards)
	p.mu.RUnlock()

	result := &PipelineResult{Blocked: false, Results: make([]GuardResult, 0, len(guards))}

	for _, g := range guards {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		gr, err := g.CheckOutput(ctx, resp)
		if err != nil {
			return result, fmt.Errorf("guard %q output check: %w", g.Name(), err)
		}
		if gr == nil {
			continue
		}

		gr.GuardName = g.Name()
		result.Results = append(result.Results, *gr)

		if gr.Action == ActionBlock {
			result.Blocked = true
			result.FirstBlock = gr
			return result, fmt.Errorf("guard %q blocked output: %s", g.Name(), gr.Reason)
		}
	}

	return result, nil
}

// Warns returns only warning-level results from the pipeline run.
func (r *PipelineResult) Warns() []GuardResult {
	var out []GuardResult
	for _, gr := range r.Results {
		if gr.Action == ActionWarn {
			out = append(out, gr)
		}
	}
	return out
}

// HasAction returns true if any result has the given action.
func (r *PipelineResult) HasAction(action Action) bool {
	for _, gr := range r.Results {
		if gr.Action == action {
			return true
		}
	}
	return false
}
