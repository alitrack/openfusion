package dag

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/lhy/openfusion/internal/types"
)

// Planner decomposes a complex task into a DAG using an LLM.
type Planner struct {
	provider  string
	model     string
	callFn    func(ctx context.Context, system, user string, maxTokens int) (string, error)
	maxDepth  int
}

// PlannerConfig configures the decomposition planner.
type PlannerConfig struct {
	Provider  string
	Model     string
	MaxDepth  int
}

// NewPlanner creates a new DAG planner.
func NewPlanner(cfg PlannerConfig, callFn func(ctx context.Context, system, user string, maxTokens int) (string, error)) *Planner {
	if cfg.MaxDepth <= 0 {
		cfg.MaxDepth = 3
	}
	return &Planner{
		provider: cfg.Provider,
		model:    cfg.Model,
		callFn:   callFn,
		maxDepth: cfg.MaxDepth,
	}
}

const decomposePrompt = `You are a task planner. Decompose the following task into atomic subtasks.

Task: %s
Available presets (choose one per subtask): %s

Return ONLY a JSON object with "nodes" and "edges":
{
  "nodes": [
    {"id": "1", "description": "...", "preset": "budget", "prompt": "the specific prompt for this step"},
    {"id": "2", "description": "...", "preset": "quality", "prompt": "..."}
  ],
  "edges": [["1", "2"]]
}

Rules:
- Each node is ONE atomic operation
- "preset" must be one of the available presets
- "prompt" contains the specific instruction for that step
- edges define dependencies: ["from", "to"]
- Output EXACTLY the JSON, no markdown fences, no explanation.`

// Decompose takes a task description and returns a DAG plan.
func (p *Planner) Decompose(ctx context.Context, task string, presets []string) (*PlanResult, error) {
	presetsDesc := strings.Join(presets, ", ")
	prompt := fmt.Sprintf(decomposePrompt, task, presetsDesc)

	start := time.Now()
	raw, err := p.callFn(ctx, "You are a JSON-only assistant. Output ONLY valid JSON, no thinking.", prompt, 4096)
	if err != nil {
		return nil, fmt.Errorf("planner generate: %w", err)
	}

	plan, err := parsePlan(raw)
	if err != nil {
		return nil, fmt.Errorf("parse plan: %w (raw: %s)", err, truncate(raw, 200))
	}

	return &PlanResult{
		Plan:      *plan,
		RawOutput: raw,
		Duration:  time.Since(start).Milliseconds(),
	}, nil
}

// parsePlan extracts a Plan from LLM output, handling various formats.
func parsePlan(raw string) (*Plan, error) {
	text := raw

	// Strip thinking markers (Qwen3 style)
	re := regexp.MustCompile(`(?s)<think>.*?</think>`)
	text = re.ReplaceAllString(text, "")

	// Strip Gemma 4 channel markers: remove lines between <|channel|thought and JSON
	re = regexp.MustCompile(`(?s)<\|channel\|thought.*?\n(\{|\n\[)`)
	text = re.ReplaceAllString(text, "$1")
	// Also handle the simple case: everything from <|channel|thought to end
	re = regexp.MustCompile(`(?s)<\|channel\|thought[^\n]*\n`)
	text = re.ReplaceAllString(text, "")

	text = strings.TrimSpace(text)

	// Try direct parse
	if plan := tryParse(text); plan != nil {
		return plan, nil
	}

	// Extract from markdown fences
	re = regexp.MustCompile("(?s)```(?:json)?\\s*(.+?)\\s*```")
	matches := re.FindAllStringSubmatch(text, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		if plan := tryParse(matches[i][1]); plan != nil {
			return plan, nil
		}
	}

	// Extract JSON object by bracket counting from the end
	for i := len(text) - 1; i >= 0; i-- {
		if text[i] == '}' {
			depth := 0
			start := -1
			for j := i; j >= 0; j-- {
				if text[j] == '}' {
					depth++
				} else if text[j] == '{' {
					depth--
					if depth == 0 {
						start = j
						break
					}
				}
			}
			if start >= 0 {
				if plan := tryParse(text[start : i+1]); plan != nil {
					return plan, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("no valid plan JSON found in %d chars", len(raw))
}

func tryParse(s string) *Plan {
	s = strings.TrimSpace(s)
	var plan Plan
	if err := json.Unmarshal([]byte(s), &plan); err == nil && len(plan.Nodes) > 0 {
		return &plan
	}
	// Try wrapped: {"plan": {...}} or {"tasks": [...]}
	var wrapper struct {
		Plan  Plan   `json:"plan"`
		Tasks []Node `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(s), &wrapper); err == nil {
		if len(wrapper.Plan.Nodes) > 0 {
			return &wrapper.Plan
		}
		if len(wrapper.Tasks) > 0 {
			// Convert tasks-only to plan
			plan := Plan{Nodes: wrapper.Tasks}
			return &plan
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// Dummy types for compilation — will be replaced with real provider call
var _ = types.ChatMessage{}
