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
	systemPrompt       string
	webSearchContext   string
	skillPromptContext string
	analysisDepth      AnalysisDepth
	twoTierMoA         bool
	secondPassProvider string
	secondPassModel    string
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

// WithTwoTierMoA enables two-tier Mixture-of-Agents synthesis (MoA paper, arXiv 2406.04692):
// tier-1 judge synthesizes from panel responses, then a second judge pass references
// both the tier-1 synthesis and the raw panel outputs to produce the final answer.
func WithTwoTierMoA(secondPassProvider, secondPassModel string) SynthesizeOption {
	return func(o *synthesizeOptions) {
		o.twoTierMoA = true
		o.secondPassProvider = secondPassProvider
		o.secondPassModel = secondPassModel
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

	// Two-tier MoA: tier-1 synthesis is done; now run a second judge pass that
	// references the tier-1 synthesis plus raw panel outputs (MoA layering).
	if o.twoTierMoA && answer != "" && o.secondPassProvider != "" && o.secondPassModel != "" {
		secondPass, err := s.runSecondPass(ctx, o, prompt, panelResponses, labels, answer)
		if err != nil {
			// Second pass is best-effort; keep tier-1 answer on failure.
			_ = err
		} else if secondPass != "" {
			answer = secondPass
		}
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

// runSecondPass executes the tier-2 MoA judge call. It builds a prompt that
// contains the original question, the tier-1 synthesis, and the raw panel
// responses, then asks the second-pass model to produce the final answer.
func (s *Synthesizer) runSecondPass(ctx context.Context, o *synthesizeOptions, prompt string,
	panelResponses []types.PanelResponse, labels []string, tier1Answer string) (string, error) {

	var sb strings.Builder
	sb.WriteString("You are the final arbiter in a two-tier model ensemble.\n\n")
	sb.WriteString("=== ORIGINAL QUESTION ===\n")
	sb.WriteString(prompt)
	sb.WriteString("\n\n=== TIER-1 SYNTHESIS (initial judge output) ===\n")
	sb.WriteString(tier1Answer)
	sb.WriteString("\n\n=== RAW MODEL RESPONSES ===\n")
	for i, pr := range panelResponses {
		if pr.Error != "" {
			continue
		}
		label := fmt.Sprintf("Model %d", i+1)
		if i < len(labels) && labels[i] != "" {
			label = labels[i]
		}
		sb.WriteString("--- " + label + " ---\n")
		sb.WriteString(pr.Content)
		sb.WriteString("\n\n")
	}
	sb.WriteString("Produce the FINAL answer. Improve on the tier-1 synthesis by resolving ")
	sb.WriteString("remaining contradictions, filling gaps only where the raw responses support it, ")
	sb.WriteString("and tightening the overall quality. Output only the final answer.\n")

	p, err := s.providerManager.Get(o.secondPassProvider)
	if err != nil {
		return "", fmt.Errorf("second-pass judge provider: %w", err)
	}

	judgeReq := &types.ChatRequest{
		Model: o.secondPassModel,
		Messages: []types.ChatMessage{
			{Role: "user", Content: sb.String()},
		},
	}

	judgeCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	resp, err := p.ChatCompletion(judgeCtx, judgeReq)
	if err != nil {
		return "", fmt.Errorf("second-pass judge call: %w", err)
	}

	if len(resp.Choices) > 0 {
		return resp.Choices[0].Message.Content, nil
	}
	return "", nil
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
