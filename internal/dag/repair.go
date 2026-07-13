package dag

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/lhy/openfusion/internal/types"
)

// RepairMaxAttempts is the maximum number of repair cycles.
const RepairMaxAttempts = 3

// Repairer handles failure recovery using minimal subgraph reconstruction.
type Repairer struct {
	planner *Planner
	execFn  ExecutorFn
}

// NewRepairer creates a new repair handler.
func NewRepairer(planner *Planner, execFn ExecutorFn) *Repairer {
	return &Repairer{
		planner: planner,
		execFn:  execFn,
	}
}

// Repair attempts to fix failed nodes by re-decomposing the affected subgraph.
// It follows the ATG paper's approach: identify the minimal affected subgraph,
// keep verified (successful) nodes frozen, and only re-plan the failed region.
func (r *Repairer) Repair(ctx context.Context, result *ExecutionResult, presets []string) (*ExecutionResult, error) {
	if len(result.FailedNodes) == 0 {
		return result, nil
	}

	g := FromPlan(result.Plan)
	affected := FailedDescendants(g, result.FailedNodes)
	if len(affected) == 0 {
		return result, nil
	}

	log.Printf("[dag/repair] %d failed nodes → %d affected for repair", len(result.FailedNodes), len(affected))

	// Build the repair context: what was the original task, what succeeded, what failed
	contextDesc := buildRepairContext(g, result)

	// Re-decompose the affected region
	newPlan, err := r.planner.Decompose(ctx, contextDesc, presets)
	if err != nil {
		return result, fmt.Errorf("repair decompose: %w", err)
	}

	// Merge: keep verified nodes from original, add new nodes from repair
	mergedPlan := mergePlans(result.Plan, newPlan.Plan, affected)

	// Execute only the new/repaired nodes
	executor := NewExecutor(r.execFn, 3)
	newResult := executor.Execute(ctx, mergedPlan)
	newResult.Repairs = result.Repairs + 1
	newResult.DurationMs += result.DurationMs + newPlan.Duration

	// If still failing, retry up to max attempts
	if !newResult.Success && newResult.Repairs < RepairMaxAttempts {
		log.Printf("[dag/repair] repair #%d still has %d failures, retrying...", newResult.Repairs, len(newResult.FailedNodes))
		return r.Repair(ctx, newResult, presets)
	}

	return newResult, nil
}

// buildRepairContext creates a prompt describing what to re-plan.
func buildRepairContext(g *Graph, result *ExecutionResult) string {
	desc := "Repair the following failed subtasks while preserving successful ones.\n\n"
	desc += "SUCCESSFUL (do NOT change):\n"
	for id, node := range result.NodeResults {
		if node.Status == StatusSuccess {
			desc += fmt.Sprintf("  [%s] %s → %s\n", id, node.Description, summarizeResult(node.Result))
		}
	}
	desc += "\nFAILED (need re-planning):\n"
	for _, id := range result.FailedNodes {
		if node, ok := g.Nodes[id]; ok {
			desc += fmt.Sprintf("  [%s] %s\n", id, node.Description)
		}
	}
	desc += "\nRe-plan ONLY the failed steps. Use different approaches if possible."
	return desc
}

// mergePlans combines the original plan with repaired nodes.
// Verified nodes from the original are kept; failed nodes are replaced.
func mergePlans(original Plan, repair Plan, affectedIDs []string) Plan {
	affected := make(map[string]bool)
	for _, id := range affectedIDs {
		affected[id] = true
	}

	// Keep verified nodes
	var nodes []Node
	for _, n := range original.Nodes {
		if !affected[n.ID] {
			nodes = append(nodes, n)
		}
	}

	// Add repaired nodes with new IDs to avoid collision
	prefix := fmt.Sprintf("r%d_", time.Now().UnixMilli()%100000)
	for _, n := range repair.Nodes {
		newID := prefix + n.ID
		n.ID = newID
		nodes = append(nodes, n)
	}

	// Merge edges: keep original verified edges, add repair edges
	var edges [][2]string
	for _, e := range original.Edges {
		if !affected[e[0]] && !affected[e[1]] {
			edges = append(edges, e)
		}
	}
	for _, e := range repair.Edges {
		edges = append(edges, [2]string{prefix + e[0], prefix + e[1]})
	}

	// Connect repair nodes to verified parents
	for i, n := range repair.Nodes {
		// Find which verified nodes are parents of the original failed node
		// by looking at edges in the original graph that point TO failed nodes
		for _, e := range original.Edges {
			if affected[e[1]] && !affected[e[0]] {
				// e[0] is a verified parent, connect it to the repair node
				edges = append(edges, [2]string{e[0], prefix + n.ID})
			}
		}
		_ = i
	}

	return Plan{Nodes: nodes, Edges: edges}
}

func summarizeResult(resp *types.ChatResponse) string {
	if resp == nil || len(resp.Choices) == 0 {
		return "(no output)"
	}
	content := resp.Choices[0].Message.Content
	if len(content) > 60 {
		return content[:60] + "..."
	}
	return content
}

var _ = fmt.Sprintf
