// Package dag provides Atomic Task Graph (ATG) scheduling for OpenFusion.
//
// It decomposes complex tasks into a DAG of subtasks, executes them with
// topological parallelism, and supports minimal subgraph repair on failure.
package dag

import "github.com/lhy/openfusion/internal/types"

// Node represents a single atomic subtask in the DAG.
type Node struct {
	ID          string         `json:"id"`
	Description string         `json:"description"`
	Tool        string         `json:"tool,omitempty"`
	Args        map[string]any `json:"args,omitempty"`
	Preset      string         `json:"preset,omitempty"` // which OpenFusion preset to use
	Prompt      string         `json:"prompt,omitempty"` // the actual prompt to send
	DependsOn   []string       `json:"depends_on"`       // prerequisite node IDs

	// Runtime state
	Status    NodeStatus          `json:"-"`
	Result    *types.ChatResponse `json:"-"`
	Error     string              `json:"-"`
	Retries   int                 `json:"-"`
}

// NodeStatus tracks execution state.
type NodeStatus int

const (
	StatusPending NodeStatus = iota
	StatusReady
	StatusRunning
	StatusSuccess
	StatusFailed
)

func (s NodeStatus) String() string {
	switch s {
	case StatusPending:
		return "pending"
	case StatusReady:
		return "ready"
	case StatusRunning:
		return "running"
	case StatusSuccess:
		return "success"
	case StatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// Graph is a directed acyclic graph of subtask nodes.
type Graph struct {
	Nodes map[string]*Node `json:"nodes"`
	Edges [][2]string      `json:"edges"` // [from, to] pairs

	// Adjacency for fast traversal
	children  map[string][]string
	parents   map[string][]string
}

// Plan is the output of the LLM-driven decomposition step.
type Plan struct {
	Nodes []Node    `json:"nodes"`
	Edges [][2]string `json:"edges"`
}

// PlanResult wraps a decomposition result with metadata.
type PlanResult struct {
	Plan      Plan   `json:"plan"`
	RawOutput string `json:"raw_output"`
	Duration  int64  `json:"duration_ms"`
}

// ExecutionResult captures the outcome of executing a DAG.
type ExecutionResult struct {
	Plan        Plan                `json:"plan"`
	NodeResults map[string]*Node    `json:"node_results"`
	Success     bool                `json:"success"`
	FailedNodes []string             `json:"failed_nodes,omitempty"`
	TotalTokens int                 `json:"total_tokens"`
	CostUSD     float64             `json:"cost_usd"`
	DurationMs  int64               `json:"duration_ms"`
	Repairs     int                 `json:"repairs"`
	Answer      string              `json:"answer,omitempty"`
}

// NewGraph creates an empty DAG.
func NewGraph() *Graph {
	return &Graph{
		Nodes:    make(map[string]*Node),
		children: make(map[string][]string),
		parents:  make(map[string][]string),
	}
}

// AddNode adds a node to the graph.
func (g *Graph) AddNode(n *Node) {
	g.Nodes[n.ID] = n
	if _, ok := g.children[n.ID]; !ok {
		g.children[n.ID] = nil
	}
	if _, ok := g.parents[n.ID]; !ok {
		g.parents[n.ID] = nil
	}
}

// AddEdge adds a directed edge from → to.
func (g *Graph) AddEdge(from, to string) {
	g.Edges = append(g.Edges, [2]string{from, to})
	g.children[from] = append(g.children[from], to)
	g.parents[to] = append(g.parents[to], from)
}

// Children returns the children of a node.
func (g *Graph) Children(id string) []string {
	return g.children[id]
}

// Parents returns the parents of a node.
func (g *Graph) Parents(id string) []string {
	return g.parents[id]
}

// BuildAdjacency reconstructs children/parents maps from Edges.
func (g *Graph) BuildAdjacency() {
	g.children = make(map[string][]string)
	g.parents = make(map[string][]string)
	for _, e := range g.Edges {
		g.children[e[0]] = append(g.children[e[0]], e[1])
		g.parents[e[1]] = append(g.parents[e[1]], e[0])
	}
	for id := range g.Nodes {
		if _, ok := g.children[id]; !ok {
			g.children[id] = nil
		}
		if _, ok := g.parents[id]; !ok {
			g.parents[id] = nil
		}
	}
}

// FromPlan constructs a Graph from a Plan.
func FromPlan(p Plan) *Graph {
	g := NewGraph()
	for i := range p.Nodes {
		n := p.Nodes[i]
		g.AddNode(&n)
	}
	for _, e := range p.Edges {
		g.AddEdge(e[0], e[1])
	}
	g.BuildAdjacency()
	return g
}
