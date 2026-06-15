// Package types defines shared data types used across OpenFusion.
package types

import "time"

// ---------------------------------------------------------------------------
// OpenAI-compatible Chat API types
// ---------------------------------------------------------------------------

// ChatMessage represents a single message in the chat conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest is the incoming request following the OpenAI chat.completions format.
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
}

// ChatResponse is the outgoing response following the OpenAI chat.completions format.
type ChatResponse struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Model   string        `json:"model"`
	Choices []Choice      `json:"choices"`
	Usage   Usage         `json:"usage,omitempty"`
	Analysis *FusionAnalysis `json:"analysis,omitempty"`
}

// Choice represents a single completion choice.
type Choice struct {
	Index   int         `json:"index"`
	Message ChatMessage `json:"message"`
}

// Usage tracks token and cost information.
type Usage struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	CostUSD          float64 `json:"cost_usd,omitempty"`
}

// ---------------------------------------------------------------------------
// Fusion-specific types
// ---------------------------------------------------------------------------

// PanelMember defines a single model in the fusion panel.
type PanelMember struct {
	Provider string `yaml:"provider" json:"provider"`
	Model    string `yaml:"model" json:"model"`
	System   string `yaml:"system,omitempty" json:"system,omitempty"`
}

// JudgeConfig defines the judge/synthesizer model.
type JudgeConfig struct {
	Provider     string `yaml:"provider" json:"provider"`
	Model        string `yaml:"model" json:"model"`
	SystemPrompt string `yaml:"system,omitempty" json:"system,omitempty"`
}

// Preset defines a named panel + judge combination.
type Preset struct {
	Name        string        `yaml:"name" json:"name"`
	Description string        `yaml:"description" json:"description"`
	Panel       []PanelMember `yaml:"panel" json:"panel"`
	Judge       JudgeConfig   `yaml:"judge" json:"judge"`
}

// PanelResponse holds the response from a single panel member.
type PanelResponse struct {
	Member   PanelMember `json:"member"`
	Content  string      `json:"content"`
	Usage    Usage       `json:"usage"`
	Error    string      `json:"error,omitempty"`
	TimedOut bool        `json:"timed_out,omitempty"`
	Duration time.Duration `json:"duration_ms,omitempty"`
}

// FusionAnalysis is the structured output from the judge.
type FusionAnalysis struct {
	Consensus       []string        `json:"consensus"`
	Contradictions  []Contradiction `json:"contradictions"`
	PartialCoverage []string        `json:"partial_coverage"`
	UniqueInsights  []Insight       `json:"unique_insights"`
	BlindSpots      []string        `json:"blind_spots"`
}

// Contradiction represents a point where models disagreed.
type Contradiction struct {
	Issue   string            `json:"issue"`
	Views   map[string]string `json:"views"` // model → statement
}

// Insight represents a unique contribution from a single model.
type Insight struct {
	Model   string `json:"model"`
	Insight string `json:"insight"`
}

// FusionResult is the complete output of a fusion run.
type FusionResult struct {
	Prompt   string           `json:"prompt"`
	Panel    []PanelResponse  `json:"panel_responses"`
	Analysis *FusionAnalysis  `json:"analysis"`
	Answer   string           `json:"answer"`
	Usage    Usage            `json:"usage"`
}

// ---------------------------------------------------------------------------
// Error types
// ---------------------------------------------------------------------------

// APIError represents a structured API error response.
type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
