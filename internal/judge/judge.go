// Package judge handles multi-model analysis and final answer synthesis.
package judge

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lhy/openfusion/internal/provider"
	"github.com/lhy/openfusion/internal/types"
)

// Synthesizer runs the judge model to analyze panel responses and produce the final answer.
type Synthesizer struct {
	providerManager *provider.Manager
	timeout         time.Duration
	promptBuilder   *JudgePromptBuilder
}

// SynthesizeOption is a functional option for customizing synthesis behavior.
type SynthesizeOption func(*synthesizeOptions)

type synthesizeOptions struct {
	systemPrompt      string
	webSearchContext  string
	skillPromptContext string
	analysisDepth     AnalysisDepth
}

// WithSystemPrompt sets a custom system prompt for the judge.
func WithSystemPrompt(prompt string) SynthesizeOption {
	return func(o *synthesizeOptions) {
		o.systemPrompt = prompt
	}
}

// WithWebSearchContext injects web search results into the judge prompt.
func WithWebSearchContext(ctx string) SynthesizeOption {
	return func(o *synthesizeOptions) {
		o.webSearchContext = ctx
	}
}

// WithSkillPromptContext injects skill-specific context into the judge prompt.
func WithSkillPromptContext(ctx string) SynthesizeOption {
	return func(o *synthesizeOptions) {
		o.skillPromptContext = ctx
	}
}

// WithAnalysisDepth sets the depth of analysis the judge should perform.
func WithAnalysisDepth(depth AnalysisDepth) SynthesizeOption {
	return func(o *synthesizeOptions) {
		o.analysisDepth = depth
	}
}

// NewSynthesizer creates a judge synthesizer.
func NewSynthesizer(pm *provider.Manager, timeout time.Duration) *Synthesizer {
	return &Synthesizer{
		providerManager: pm,
		timeout:         timeout,
		promptBuilder:   NewPromptBuilder(),
	}
}

// Synthesize runs the judge: analyzes panel responses and produces the final answer.
// Uses the default standard analysis depth and no extra context.
func (s *Synthesizer) Synthesize(ctx context.Context, judgeCfg types.JudgeConfig, prompt string, panelResponses []types.PanelResponse) (*types.FusionResult, error) {
	return s.SynthesizeWithOptions(ctx, judgeCfg, prompt, panelResponses)
}

// SynthesizeWithOptions runs the judge with custom options.
func (s *Synthesizer) SynthesizeWithOptions(ctx context.Context, judgeCfg types.JudgeConfig, prompt string, panelResponses []types.PanelResponse, opts ...SynthesizeOption) (*types.FusionResult, error) {
	// Apply options
	o := &synthesizeOptions{
		analysisDepth: AnalysisStandard,
	}
	for _, opt := range opts {
		opt(o)
	}

	// Build labels and collect responses
	labels := make([]string, len(panelResponses))
	contents := make([]string, len(panelResponses))
	for i, pr := range panelResponses {
		labels[i] = pr.Member.Provider + " / " + pr.Member.Model
		contents[i] = pr.Content
	}

	// Build the prompt using PromptBuilder
	promptCtx := PromptContext{
		OriginalQuestion:   prompt,
		PanelResponses:     contents,
		PanelLabels:        labels,
		JudgeSystemPrompt:  o.systemPrompt,
		AnalysisDepth:      o.analysisDepth,
		WebSearchContext:   o.webSearchContext,
		SkillPromptContext: o.skillPromptContext,
	}
	analysisPrompt := s.promptBuilder.Build(promptCtx)

	p, err := s.providerManager.Get(judgeCfg.Provider)
	if err != nil {
		return nil, fmt.Errorf("judge provider: %w", err)
	}

	judgeReq := &types.ChatRequest{
		Model: judgeCfg.Model,
		Messages: []types.ChatMessage{
			{Role: "user", Content: analysisPrompt},
		},
	}

	if judgeCfg.SystemPrompt != "" {
		judgeReq.Messages = append([]types.ChatMessage{
			{Role: "system", Content: judgeCfg.SystemPrompt},
		}, judgeReq.Messages...)
	}

	judgeCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	resp, err := p.ChatCompletion(judgeCtx, judgeReq)
	if err != nil {
		return nil, fmt.Errorf("judge call: %w", err)
	}

	answer := ""
	if len(resp.Choices) > 0 {
		answer = resp.Choices[0].Message.Content
	}

	result := &types.FusionResult{
		Prompt: prompt,
		Panel:  panelResponses,
		Answer: answer,
		Usage:  resp.Usage,
	}

	// Attempt to extract structured analysis from the answer
	result.Analysis = extractAnalysis(answer)

	// Accumulate panel usage — skip failed members (they have zero or misleading usage)
	for _, pr := range panelResponses {
		if pr.Error != "" {
			continue
		}
		result.Usage.PromptTokens += pr.Usage.PromptTokens
		result.Usage.CompletionTokens += pr.Usage.CompletionTokens
		result.Usage.TotalTokens += pr.Usage.TotalTokens
		result.Usage.CostUSD += pr.Usage.CostUSD
	}

	return result, nil
}

// PromptBuilder returns the internal prompt builder for external use.
func (s *Synthesizer) PromptBuilder() *JudgePromptBuilder {
	return s.promptBuilder
}

// extractAnalysis does simple keyword-based extraction for structured analysis.
// In production, the judge model could return JSON; this is a pragmatic fallback.
func extractAnalysis(answer string) *types.FusionAnalysis {
	analysis := &types.FusionAnalysis{
		Consensus:       extractSection(answer, "Consensus Points"),
		Contradictions:  nil, // hard to parse reliably from prose
		PartialCoverage: extractSection(answer, "Partial Coverage"),
		UniqueInsights:  nil,
		BlindSpots:      extractSection(answer, "Blind Spots"),
	}
	return analysis
}

// extractSection extracts bullet points after a section header.
func extractSection(text, section string) []string {
	lower := strings.ToLower(text)
	marker := strings.ToLower(section)
	idx := strings.Index(lower, marker)
	if idx < 0 {
		return nil
	}

	// Find the start (after the header line)
	rest := text[idx+len(marker):]
	nlIdx := strings.Index(rest, "\n")
	if nlIdx >= 0 {
		rest = rest[nlIdx:]
	}

	// Collect bullet points until the next ## header
	var items []string
	for _, line := range strings.Split(rest, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "##") {
			break
		}
		if strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "1.") {
			items = append(items, strings.TrimSpace(trimmed[1:]))
		}
	}

	if len(items) == 0 {
		return nil
	}
	return items
}
