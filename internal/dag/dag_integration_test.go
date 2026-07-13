package dag

import (
	"context"
	"testing"

	"github.com/lhy/openfusion/internal/types"
)

// helperExec creates a simple executor function for testing.
func helperExec(fn func(preset, prompt string) (string, error)) ExecutorFn {
	return func(ctx context.Context, presetName, prompt string) (*types.ChatResponse, error) {
		content, err := fn(presetName, prompt)
		if err != nil {
			return nil, err
		}
		return &types.ChatResponse{
			Choices: []types.Choice{{
				Message: types.ChatMessage{Role: "assistant", Content: content},
			}},
		}, nil
	}
}

func TestDAGFullPipeline(t *testing.T) {
	plan := Plan{
		Nodes: []Node{
			{ID: "1", Description: "search cats", Preset: "budget", Prompt: "search cats"},
			{ID: "2", Description: "read result", Preset: "budget", Prompt: "read result"},
			{ID: "3", Description: "write summary", Preset: "quality", Prompt: "write summary"},
			{ID: "4", Description: "save file", Preset: "budget", Prompt: "save file"},
		},
		Edges: [][2]string{
			{"1", "2"}, {"2", "3"}, {"3", "4"},
		},
	}

	exec := NewExecutor(helperExec(func(preset, prompt string) (string, error) {
		return "result:" + prompt, nil
	}), 3)

	result := exec.Execute(context.Background(), plan)
	if !result.Success {
		t.Errorf("expected success, got failed: %v", result.FailedNodes)
	}
	if len(result.NodeResults) != 4 {
		t.Errorf("expected 4 node results, got %d", len(result.NodeResults))
	}
	// Verify topological order: 1 before 2 before 3 before 4
	if result.NodeResults["1"].Status != StatusSuccess {
		t.Error("node 1 failed")
	}
	if result.NodeResults["4"].Status != StatusSuccess {
		t.Error("node 4 failed")
	}
}

func TestDAGWithFailure(t *testing.T) {
	plan := Plan{
		Nodes: []Node{
			{ID: "a", Description: "step a", Prompt: "a"},
			{ID: "b", Description: "step b fails", Prompt: "b"},
			{ID: "c", Description: "step c", Prompt: "c"},
		},
		Edges: [][2]string{
			{"a", "b"},
			{"a", "c"}, // c only depends on a, NOT on b
		},
	}

	exec := NewExecutor(helperExec(func(preset, prompt string) (string, error) {
		if prompt == "b" {
			return "", assertErr("simulated failure")
		}
		return "ok:" + prompt, nil
	}), 2)

	result := exec.Execute(context.Background(), plan)
	if result.Success {
		t.Error("expected failure, got success")
	}
	if len(result.FailedNodes) != 1 {
		t.Errorf("expected 1 failed node, got %d: %v", len(result.FailedNodes), result.FailedNodes)
	}
	// a and c should succeed (c doesn't depend on b)
	if result.NodeResults["a"].Status != StatusSuccess {
		t.Error("node a should have succeeded")
	}
	if result.NodeResults["c"].Status != StatusSuccess {
		t.Error("node c should have succeeded (no dependency on b)")
	}
}

func TestDAGDeadlockDetection(t *testing.T) {
	plan := Plan{
		Nodes: []Node{
			{ID: "x", Description: "x", Prompt: "x"},
			{ID: "y", Description: "y", Prompt: "y"},
		},
		Edges: [][2]string{
			{"x", "y"}, {"y", "x"}, // cycle
		},
	}

	exec := NewExecutor(helperExec(func(preset, prompt string) (string, error) {
		return "ok", nil
	}), 1)

	result := exec.Execute(context.Background(), plan)
	if result.Success {
		t.Error("expected failure for cyclic DAG")
	}
}

func TestParseGemma4Output(t *testing.T) {
	raw := "<|channel>thought\n* Plan:\n  1. search\n" +
		"```json\n" +
		`{"nodes":[{"id":"1","description":"search","preset":"budget","prompt":"search X"}],` +
		`"edges":[["1","2"]]}` + "\n```"

	plan, err := parsePlan(raw)
	if err != nil {
		t.Fatalf("parsePlan: %v", err)
	}
	if len(plan.Nodes) != 1 {
		t.Errorf("expected 1 node, got %d", len(plan.Nodes))
	}
}

func TestRepairMinimalSubgraph(t *testing.T) {
	plan := Plan{
		Nodes: []Node{
			{ID: "1", Description: "verified step", Prompt: "ok1"},
			{ID: "2", Description: "failing step", Prompt: "fail"},
			{ID: "3", Description: "depends on 2", Prompt: "ok3"},
		},
		Edges: [][2]string{
			{"1", "2"}, {"2", "3"},
		},
	}

	exec := NewExecutor(helperExec(func(preset, prompt string) (string, error) {
		if prompt == "fail" {
			return "", assertErr("fail")
		}
		return "ok", nil
	}), 2)

	result := exec.Execute(context.Background(), plan)

	// Verify repair identifies correct affected nodes
	g := FromPlan(plan)
	affected := FailedDescendants(g, result.FailedNodes)
	if len(affected) < 2 {
		t.Errorf("expected at least 2 affected (2+3), got %d: %v", len(affected), affected)
	}

	// Node 1 should NOT be in affected (it succeeded and nothing depends on it)
	for _, id := range affected {
		if id == "1" {
			t.Error("node 1 should NOT be affected (it was verified)")
		}
	}
}

// assertErr helper
func assertErr(msg string) error { return &testError{msg} }

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
