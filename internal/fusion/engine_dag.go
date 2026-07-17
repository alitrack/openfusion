package fusion

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/lhy/openfusion/internal/dag"
	"github.com/lhy/openfusion/internal/types"
)

// ExecuteDAG decomposes a complex task into an atomic task graph,
// executes it with topological parallelism, and applies repair on failure.
func (e *Engine) ExecuteDAG(req *types.ChatRequest) (*types.ChatResponse, error) {
	start := time.Now()
	task := types.ExtractLastUserMessage(req.Messages)
	if task == "" {
		return nil, fmt.Errorf("dag: no user message found")
	}

	// Get available presets
	presets := e.presetRegistry.Load().List()
	if len(presets) == 0 {
		return nil, fmt.Errorf("dag: no presets available")
	}
	presetNames := make([]string, len(presets))
	for i, p := range presets {
		presetNames[i] = p.Name
	}

	// Find a planner provider from config, with sensible fallbacks
	plannerProvider := e.dagPlanner.Provider
	plannerModel := e.dagPlanner.Model
	if plannerProvider == "" {
		plannerProvider = "cc-switch"
	}
	if plannerModel == "" {
		plannerModel = "deepseek-chat"
	}

	// Create planner with LLM call function that uses Engine's provider manager
	callFn := func(ctx context.Context, system, user string, maxTokens int) (string, error) {
		pm := e.getProviderManager()
		p, err := pm.Get(plannerProvider)
		if err != nil {
			return "", fmt.Errorf("dag planner provider '%s': %w", plannerProvider, err)
		}

		// Inject memory context for planner if available.
		plannerSystem := system
		if e.memoryStore != nil && req.UserID != "" && req.ProjectID != "" {
			if s := e.memoryStore.ContextSummary(req.UserID, req.ProjectID, 5); s != "" {
				plannerSystem = s + "\n" + system
			}
		}
		plannerReq := &types.ChatRequest{
			Model: plannerModel,
			Messages: []types.ChatMessage{
				{Role: "system", Content: plannerSystem},
				{Role: "user", Content: user},
			},
			MaxTokens: maxTokens,
		}
		resp, err := p.ChatCompletion(ctx, plannerReq)
		if err != nil {
			return "", fmt.Errorf("dag planner call: %w", err)
		}
		if len(resp.Choices) == 0 {
			return "", fmt.Errorf("dag planner: empty response")
		}
		return resp.Choices[0].Message.Content, nil
	}

	planner := dag.NewPlanner(dag.PlannerConfig{
		Provider: plannerProvider,
		Model:    plannerModel,
		MaxDepth: 2,
	}, callFn)

	// Decompose the task
	planResult, err := planner.Decompose(context.Background(), task, presetNames)
	if err != nil {
		// Fallback: treat as single-node DAG
		log.Printf("[dag] decompose failed: %v, falling back to single-node", err)
		singlePlan := dag.Plan{
			Nodes: []dag.Node{{
				ID:          "1",
				Description: task,
				Preset:      presetNames[0],
				Prompt:      task,
			}},
		}
		planResult = &dag.PlanResult{Plan: singlePlan}
	}

	log.Printf("[dag] decomposed into %d nodes, %d edges in %dms",
		len(planResult.Plan.Nodes), len(planResult.Plan.Edges), planResult.Duration)

	// Execute: each node runs through the full fusion pipeline (preset → panel → judge)
	execFn := func(ctx context.Context, presetName, prompt string) (*types.ChatResponse, error) {
		nodeReq := &types.ChatRequest{
			Model: presetName,
			Messages: []types.ChatMessage{
				{Role: "user", Content: prompt},
			},
		}
		return e.Execute(presetName, nodeReq)
	}

	executor := dag.NewExecutor(execFn, 3) // max 3 parallel nodes
	result := executor.Execute(context.Background(), planResult.Plan)

	// Repair if needed
	if !result.Success {
		repairer := dag.NewRepairer(planner, execFn)
		repaired, repairErr := repairer.Repair(context.Background(), result, presetNames)
		if repairErr != nil {
			log.Printf("[dag] repair failed: %v", repairErr)
		} else {
			result = repaired
		}
	}

	// Build response
	content := result.Answer
	if content == "" && !result.Success {
		content = fmt.Sprintf("DAG execution failed: %d nodes failed", len(result.FailedNodes))
	}

	resp := &types.ChatResponse{
		ID:      fmt.Sprintf("dag-%d", time.Now().UnixMilli()),
		Object:  "chat.completion",
		Model:   "openfusion/dag",
		Choices: []types.Choice{{
			Index:   0,
			Message: types.ChatMessage{Role: "assistant", Content: content},
		}},
		Usage: types.Usage{
			TotalTokens: result.TotalTokens,
			CostUSD:     result.CostUSD,
		},
	}

	_ = start
	log.Printf("[dag] completed in %dms: success=%v nodes=%d failed=%d repairs=%d",
		time.Since(start).Milliseconds(), result.Success,
		len(result.NodeResults), len(result.FailedNodes), result.Repairs)

	return resp, nil
}
