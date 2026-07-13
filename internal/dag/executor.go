package dag

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/lhy/openfusion/internal/types"
)

// ExecutorFn is the function signature for executing a single DAG node.
// It takes a node's prompt and preset, and returns a chat response.
type ExecutorFn func(ctx context.Context, presetName, prompt string) (*types.ChatResponse, error)

// Executor executes a DAG plan with topological parallelism.
type Executor struct {
	execFn      ExecutorFn
	maxParallel int
}

// NewExecutor creates a new DAG executor.
func NewExecutor(execFn ExecutorFn, maxParallel int) *Executor {
	if maxParallel <= 0 {
		maxParallel = 3
	}
	return &Executor{
		execFn:      execFn,
		maxParallel: maxParallel,
	}
}

// Execute runs the DAG plan, respecting dependencies.
// Nodes with all dependencies satisfied run in parallel.
func (e *Executor) Execute(ctx context.Context, plan Plan) *ExecutionResult {
	g := FromPlan(plan)
	order := TopologicalOrder(g)
	if order == nil {
		return &ExecutionResult{
			Plan:    plan,
			Success: false,
			NodeResults: map[string]*Node{
				"_error": {ID: "_error", Error: "cycle detected in DAG"},
			},
		}
	}

	start := time.Now()
	completed := make(map[string]bool)
	nodeResults := make(map[string]*Node)
	var mu sync.Mutex
	var totalTokens int
	var totalCost float64

	for len(completed) < len(g.Nodes) {
		ready := ReadyNodes(g, completed)
		if len(ready) == 0 {
			// Deadlock: no ready nodes but not all completed
			remaining := []string{}
			for id := range g.Nodes {
				if !completed[id] {
					remaining = append(remaining, id)
				}
			}
			return &ExecutionResult{
				Plan:        plan,
				Success:     false,
				NodeResults: nodeResults,
				FailedNodes: remaining,
				DurationMs:  time.Since(start).Milliseconds(),
			}
		}

		// Limit parallelism
		if len(ready) > e.maxParallel {
			ready = ready[:e.maxParallel]
		}

		// Mark nodes as running
		for _, id := range ready {
			g.Nodes[id].Status = StatusRunning
		}

		// Execute in parallel
		var wg sync.WaitGroup
		results := make([]struct {
			id     string
			node   *Node
		}, len(ready))

		for i, id := range ready {
			wg.Add(1)
			go func(idx int, nodeID string) {
				defer wg.Done()
				node := g.Nodes[nodeID]

				preset := node.Preset
				if preset == "" {
					preset = "budget"
				}
				prompt := node.Prompt
				if prompt == "" {
					prompt = node.Description
				}

				resp, err := e.execFn(ctx, preset, prompt)

				mu.Lock()
				defer mu.Unlock()

				if err != nil {
					node.Status = StatusFailed
					node.Error = err.Error()
					log.Printf("[dag] node %s FAILED: %v", nodeID, err)
				} else {
					node.Status = StatusSuccess
					node.Result = resp
					if resp != nil {
						totalTokens += resp.Usage.TotalTokens
						totalCost += resp.Usage.CostUSD
					}
				}
				completed[nodeID] = node.Status == StatusSuccess
				nodeResults[nodeID] = node
				results[idx] = struct {
					id   string
					node *Node
				}{nodeID, node}
			}(i, id)
		}
		wg.Wait()
	}

	// Collect failed nodes
	var failed []string
	success := true
	for _, node := range nodeResults {
		if node.Status == StatusFailed {
			failed = append(failed, node.ID)
			success = false
		}
	}

	// Synthesize answer from final node (last in topological order)
	answer := ""
	if len(order) > 0 {
		lastID := order[len(order)-1]
		if n, ok := nodeResults[lastID]; ok && n.Result != nil && len(n.Result.Choices) > 0 {
			answer = n.Result.Choices[0].Message.Content
		}
	}

	return &ExecutionResult{
		Plan:        plan,
		NodeResults: nodeResults,
		Success:     success,
		FailedNodes: failed,
		TotalTokens: totalTokens,
		CostUSD:     totalCost,
		DurationMs:  time.Since(start).Milliseconds(),
		Answer:      answer,
	}
}

// Dummy to keep types import
var _ = fmt.Sprintf
