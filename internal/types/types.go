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
	Model          string            `json:"model"`
	Messages       []ChatMessage     `json:"messages"`
	MaxTokens      int               `json:"max_tokens,omitempty"`
	Temperature    *float64          `json:"temperature,omitempty"`
	Stream         bool              `json:"stream,omitempty"`
	NoJudge        *bool             `json:"no_judge,omitempty"`
	Tools          []any     `json:"tools,omitempty"`
	ResponseFormat *ResponseFormat   `json:"response_format,omitempty"`
	Think          *bool             `json:"think,omitempty"`
	ThinkBudget    int               `json:"think_budget,omitempty"`
	Codex          bool              `json:"codex,omitempty"`
	// PanelOverride replaces the preset's panel entirely when non-nil.
	PanelOverride []PanelMember `json:"panel,omitempty"`
	// JudgeOverride replaces the preset's judge entirely when non-nil.
	JudgeOverride *JudgeConfig `json:"judge,omitempty"`
}

// ResponseFormat controls structured output.
type ResponseFormat struct {
	Type string `json:"type"` // "text" or "json_object"
}

// StreamChunk represents a single SSE data chunk in OpenAI format.
type StreamChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []StreamChoice `json:"choices"`
}

// StreamChoice represents a single streaming choice.
type StreamChoice struct {
	Index int          `json:"index"`
	Delta StreamDelta  `json:"delta"`
}

// StreamDelta represents the incremental content delta.
type StreamDelta struct {
	Content string `json:"content,omitempty"`
}

// PanelResponseSummary is a public summary of one panel model's response.
type PanelResponseSummary struct {
	Model      string `json:"model"`
	Content    string `json:"content"`
	DurationMs int64  `json:"duration_ms"`
	PromptTokens int  `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens int   `json:"total_tokens"`
	CostUSD    float64 `json:"cost_usd"`
	Error      string `json:"error,omitempty"`
}

// ChatResponse is the outgoing response following the OpenAI chat.completions format.
type ChatResponse struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Model   string        `json:"model"`
	Choices []Choice      `json:"choices"`
	Usage   Usage         `json:"usage,omitempty"`
	Analysis *FusionAnalysis `json:"analysis,omitempty"`
	PanelResponses []PanelResponseSummary `json:"panel_responses,omitempty"`
	Codex   *CodexResponse `json:"codex,omitempty"`
}

// CodexFile represents a single generated file.
type CodexFile struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Language string `json:"language"`
}

// CodexResponse is the structured output for codex: true requests.
type CodexResponse struct {
	Language   string      `json:"language"`
	Files      []CodexFile `json:"files"`
	Explanation string     `json:"explanation"`
	Tests      string      `json:"tests,omitempty"`
	Analysis   *CodexAnalysis `json:"analysis,omitempty"`
}

// CodexAnalysis shows what the judge found.
type CodexAnalysis struct {
	PanelCount     int      `json:"panel_count"`
	Consensus      []string `json:"consensus,omitempty"`
	ChosenApproach string   `json:"chosen_approach,omitempty"`
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

// WebSearchConfig defines web search behavior for a skill/preset.
type WebSearchConfig struct {
	Enabled          bool   `yaml:"enabled" json:"enabled"`
	Queries          int    `yaml:"queries,omitempty" json:"queries,omitempty"`
	MaxResults       int    `yaml:"max_results,omitempty" json:"max_results,omitempty"`
	MaxContextLength int    `yaml:"max_context_length,omitempty" json:"max_context_length,omitempty"`
	Backend          string `yaml:"backend,omitempty" json:"backend,omitempty"` // brave, custom
	APIKey           string `yaml:"api_key,omitempty" json:"-"`                 // never serialized
	CustomEndpoint   string `yaml:"custom_endpoint,omitempty" json:"custom_endpoint,omitempty"`
}

// Preset defines a named panel + judge combination.
type Preset struct {
	Name        string        `yaml:"name" json:"name"`
	Description string        `yaml:"description" json:"description"`
	Panel       []PanelMember `yaml:"panel" json:"panel"`
	Judge       JudgeConfig   `yaml:"judge" json:"judge"`
	WebSearch   *WebSearchConfig `yaml:"web_search,omitempty" json:"web_search,omitempty"`
}

// InlinePreset is used for presets defined inline (embedded in config.yaml).
type InlinePreset struct {
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
// Utility helpers
// ---------------------------------------------------------------------------

// ExtractLastUserMessage extracts the content of the last user message.
func ExtractLastUserMessage(messages []ChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}
