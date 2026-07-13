package dag

import (
	"sort"
)

// TopologicalOrder returns nodes in topological order (Kahn's algorithm).
// Returns nil if the graph has a cycle.
func TopologicalOrder(g *Graph) []string {
	inDegree := make(map[string]int)
	for id := range g.Nodes {
		inDegree[id] = len(g.Parents(id))
	}

	var queue []string
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	sort.Strings(queue) // deterministic order

	var result []string
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		result = append(result, id)

		for _, child := range g.Children(id) {
			inDegree[child]--
			if inDegree[child] == 0 {
				queue = append(queue, child)
			}
		}
		sort.Strings(queue)
	}

	if len(result) != len(g.Nodes) {
		return nil // cycle detected
	}
	return result
}

// ReadyNodes returns nodes whose dependencies are all satisfied.
func ReadyNodes(g *Graph, completed map[string]bool) []string {
	var ready []string
	for id, node := range g.Nodes {
		if completed[id] {
			continue
		}
		if node.Status != StatusPending {
			continue
		}
		allDone := true
		for _, parent := range g.Parents(id) {
			if !completed[parent] {
				allDone = false
				break
			}
		}
		if allDone {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)
	return ready
}

// FailedDescendants finds all descendants of failed nodes that need repair.
func FailedDescendants(g *Graph, failedIDs []string) []string {
	failed := make(map[string]bool)
	for _, id := range failedIDs {
		failed[id] = true
	}

	// BFS from each failed node to find affected descendants
	var affected []string
	visited := make(map[string]bool)
	for _, id := range failedIDs {
		queue := []string{id}
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			if visited[current] {
				continue
			}
			visited[current] = true

			for _, child := range g.Children(current) {
				if !visited[child] {
					failed[child] = true
					affected = append(affected, child)
					queue = append(queue, child)
				}
			}
		}
	}

	// Add the original failed nodes
	for _, id := range failedIDs {
		affected = append(affected, id)
	}
	return affected
}

// Subgraph extracts a subgraph containing only the given node IDs + their edges.
func Subgraph(g *Graph, nodeIDs []string) *Graph {
	include := make(map[string]bool)
	for _, id := range nodeIDs {
		include[id] = true
	}

	sub := NewGraph()
	for _, id := range nodeIDs {
		if n, ok := g.Nodes[id]; ok {
			clone := *n
			clone.Status = StatusPending
			clone.Result = nil
			clone.Error = ""
			sub.AddNode(&clone)
		}
	}

	for _, e := range g.Edges {
		if include[e[0]] && include[e[1]] {
			sub.AddEdge(e[0], e[1])
		}
	}
	sub.BuildAdjacency()
	return sub
}

// ResetNodes resets node statuses for retry.
func ResetNodes(nodes []*Node) {
	for _, n := range nodes {
		n.Status = StatusPending
		n.Result = nil
		n.Error = ""
	}
}
