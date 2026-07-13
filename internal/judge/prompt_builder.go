// Package judge handles multi-model analysis and final answer synthesis.
package judge

import (
	"fmt"
	"strings"
)

// PromptContext holds all the contextual information needed to build a judge prompt.
type PromptContext struct {
	OriginalQuestion    string
	PanelResponses      []string // one per panel member, in order
	PanelLabels         []string // display labels for each panel response
	JudgeSystemPrompt   string
	AnalysisDepth       AnalysisDepth
	WebSearchContext    string
	SkillPromptContext  string
}

// AnalysisDepth controls how thorough the judge's analysis should be.
type AnalysisDepth string

const (
	// AnalysisSimple: brief comparison, no structured sections.
	AnalysisSimple AnalysisDepth = "simple"
	// AnalysisStandard: consensus, contradictions, blind spots.
	AnalysisStandard AnalysisDepth = "standard"
	// AnalysisDeep: full structured analysis with partial coverage and unique insights.
	AnalysisDeep AnalysisDepth = "deep"
)

// JudgePromptBuilder assembles judge prompts conditionally based on context.
type JudgePromptBuilder struct{}

// NewPromptBuilder creates a new JudgePromptBuilder.
func NewPromptBuilder() *JudgePromptBuilder {
	return &JudgePromptBuilder{}
}

// Build assembles the full judge prompt from the given context.
// Sections are included conditionally based on the depth and what's provided.
func (b *JudgePromptBuilder) Build(ctx PromptContext) string {
	var sb strings.Builder

	// System prompt (prepended if provided)
	if ctx.JudgeSystemPrompt != "" {
		sb.WriteString("### System Instructions\n")
		sb.WriteString(ctx.JudgeSystemPrompt)
		sb.WriteString("\n\n")
	}

	// Task header
	sb.WriteString("You are an expert AI response analyst. Below is a question followed by answers from multiple AI models.\n\n")

	// Web search context (if available)
	if ctx.WebSearchContext != "" {
		sb.WriteString("### Web Search Context\n")
		sb.WriteString(ctx.WebSearchContext)
		sb.WriteString("\n\n")
	}

	// Skill prompt context (if available)
	if ctx.SkillPromptContext != "" {
		sb.WriteString("### Skill Context\n")
		sb.WriteString(ctx.SkillPromptContext)
		sb.WriteString("\n\n")
	}

	// Original question
	sb.WriteString("=== ORIGINAL QUESTION ===\n")
	sb.WriteString(ctx.OriginalQuestion)
	sb.WriteString("\n\n")

	// Panel responses
	for i, resp := range ctx.PanelResponses {
		label := fmt.Sprintf("Model %d", i+1)
		if i < len(ctx.PanelLabels) && ctx.PanelLabels[i] != "" {
			label = ctx.PanelLabels[i]
		}
		sb.WriteString("=== ")
		sb.WriteString(label)
		sb.WriteString(" ===\n")
		sb.WriteString(resp)
		sb.WriteString("\n\n")
	}

	// Analysis instructions — vary by depth
	b.writeAnalysisInstructions(&sb, ctx.AnalysisDepth)

	return sb.String()
}

// writeAnalysisInstructions appends the analysis instructions section.
func (b *JudgePromptBuilder) writeAnalysisInstructions(sb *strings.Builder, depth AnalysisDepth) {
	switch depth {
	case AnalysisSimple:
		sb.WriteString(`Please provide a concise final answer that synthesizes the best reasoning from all models.
Note areas of agreement and briefly mention any contradictions.
Do not add external knowledge beyond what the models provided.
`)
	case AnalysisDeep:
		sb.WriteString(`Please analyze these responses and produce:

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
	default: // AnalysisStandard
		sb.WriteString(`Please analyze these responses and produce:

## Structured Analysis

1. **Consensus Points**: What all or most models agree on (high confidence findings).
2. **Contradictions**: Where models disagree. For each, note which model said what.
3. **Blind Spots**: Important angles or facts that ALL models missed.

## Final Answer

Write a comprehensive, well-structured final answer that:
- Synthesizes the best reasoning from all models
- Notes areas of agreement as higher-confidence
- Honestly acknowledges contradictions where they exist
- Acknowledges blind spots
- Is written as a coherent narrative (not a comparison table)

Base your answer ONLY on the content provided by the models. Do not add external knowledge.
`)
	}
}
