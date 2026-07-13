package dag

import (
	"testing"
)

func TestTopologicalOrder(t *testing.T) {
	g := NewGraph()
	g.AddNode(&Node{ID: "1"})
	g.AddNode(&Node{ID: "2"})
	g.AddNode(&Node{ID: "3"})
	g.AddEdge("1", "2")
	g.AddEdge("1", "3")
	g.BuildAdjacency()

	order := TopologicalOrder(g)
	if order == nil {
		t.Fatal("expected order, got nil (cycle detected)")
	}
	if len(order) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(order))
	}
	if order[0] != "1" {
		t.Errorf("expected node 1 first, got %s", order[0])
	}
}

func TestReadyNodes(t *testing.T) {
	g := NewGraph()
	g.AddNode(&Node{ID: "a"})
	g.AddNode(&Node{ID: "b"})
	g.AddNode(&Node{ID: "c"})
	g.AddEdge("a", "b")
	g.AddEdge("a", "c")
	g.BuildAdjacency()

	completed := map[string]bool{}
	ready := ReadyNodes(g, completed)
	if len(ready) != 1 || ready[0] != "a" {
		t.Errorf("expected only 'a' ready, got %v", ready)
	}

	completed["a"] = true
	g.Nodes["a"].Status = StatusSuccess
	ready = ReadyNodes(g, completed)
	if len(ready) != 2 {
		t.Errorf("expected 'b' and 'c' ready after 'a' done, got %v", ready)
	}
}

func TestFailedDescendants(t *testing.T) {
	g := NewGraph()
	g.AddNode(&Node{ID: "1"})
	g.AddNode(&Node{ID: "2"})
	g.AddNode(&Node{ID: "3"})
	g.AddNode(&Node{ID: "4"})
	g.AddEdge("1", "2")
	g.AddEdge("2", "3")
	g.AddEdge("3", "4")
	g.AddEdge("1", "4") // parallel path
	g.BuildAdjacency()

	affected := FailedDescendants(g, []string{"2"})
	// Node 2 failure affects 2, 3, 4 (not 1)
	if len(affected) < 3 {
		t.Errorf("expected at least 3 affected nodes, got %d: %v", len(affected), affected)
	}
}

func TestSubgraph(t *testing.T) {
	g := NewGraph()
	g.AddNode(&Node{ID: "a"})
	g.AddNode(&Node{ID: "b"})
	g.AddNode(&Node{ID: "c"})
	g.AddEdge("a", "b")
	g.AddEdge("b", "c")
	g.BuildAdjacency()

	sub := Subgraph(g, []string{"a", "c"})
	if _, ok := sub.Nodes["a"]; !ok {
		t.Error("expected node 'a' in subgraph")
	}
	if _, ok := sub.Nodes["b"]; ok {
		t.Error("node 'b' should NOT be in subgraph")
	}
	if _, ok := sub.Nodes["c"]; !ok {
		t.Error("expected node 'c' in subgraph")
	}
}

func TestCycleDetection(t *testing.T) {
	g := NewGraph()
	g.AddNode(&Node{ID: "1"})
	g.AddNode(&Node{ID: "2"})
	g.AddNode(&Node{ID: "3"})
	g.AddEdge("1", "2")
	g.AddEdge("2", "3")
	g.AddEdge("3", "1") // cycle
	g.BuildAdjacency()

	order := TopologicalOrder(g)
	if order != nil {
		t.Errorf("expected nil for cycle, got %v", order)
	}
}

func TestPlanParsing(t *testing.T) {
	// Test JSON with markdown fences (Gemma 4 style)
	raw := "Some thinking...\n```json\n{\"nodes\":[{\"id\":\"1\",\"description\":\"search\"}],\"edges\":[]}\n```\n"
	plan, err := parsePlan(raw)
	if err != nil {
		t.Fatalf("parsePlan failed: %v", err)
	}
	if len(plan.Nodes) != 1 {
		t.Errorf("expected 1 node, got %d", len(plan.Nodes))
	}
}
