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
}

// NewSynthesizer creates a judge synthesizer.
func NewSynthesizer(pm *provider.Manager, timeout time.Duration) *Synthesizer {
	return &Synthesizer{
		providerManager: pm,
		timeout:         timeout,
	}
}

// Synthesize runs the judge: analyzes panel responses and produces the final answer.
func (s *Synthesizer) Synthesize(ctx context.Context, judgeCfg types.JudgeConfig, prompt string, panelResponses []types.PanelResponse) (*types.FusionResult, error) {
	analysisPrompt := s.buildAnalysisPrompt(prompt, panelResponses)

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

// buildAnalysisPrompt constructs the prompt sent to the judge model.
func (s *Synthesizer) buildAnalysisPrompt(originalPrompt string, responses []types.PanelResponse) string {
	var b strings.Builder

	b.WriteString("You are an expert AI response analyst. Below is a complex research question followed by answers from multiple AI models.\n\n")
	b.WriteString("=== ORIGINAL QUESTION ===\n")
	b.WriteString(originalPrompt)
	b.WriteString("\n\n")

	for _, pr := range responses {
		b.WriteString("=== ")
		b.WriteString(pr.Member.Provider)
		b.WriteString(" / ")
		b.WriteString(pr.Member.Model)
		b.WriteString(" ===\n")

		if pr.Error != "" {
			b.WriteString("[ERROR: ")
			b.WriteString(pr.Error)
			b.WriteString("]\n\n")
			continue
		}

		b.WriteString(pr.Content)
		b.WriteString("\n\n")
	}

	b.WriteString(`Please analyze these responses and produce:

## Structured Analysis

1. **Consensus Points**: What all or most models agree on (high confidence findings).
2. **Contradictions**: Where models disagree. For each, note which model said what.
3. **Partial Coverage**: Important points only covered by some models.
4. **Unique Insights**: Valuable points made by only a single model (with source).
5. **Blind Spots**: Important angles or facts that ALL models missed.

## Final Answer

Write a comprehensive, well-structured final answer that:
- Synthesizes the best reasoning from all models
- Notes areas of agreement as higher-confidence
- Honestly acknowledges contradictions where they exist
- Highlights any unique insights
- Acknowledges blind spots
- Is written as a coherent narrative (not a comparison table)

Base your answer ONLY on the content provided by the models. Do not add external knowledge.
`)
	return b.String()
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
