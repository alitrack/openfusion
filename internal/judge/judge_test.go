package judge

import (
	"testing"

)

func TestExtractAnalysis(t *testing.T) {
	answer := `## Structured Analysis

1. **Consensus Points**: Both models agree that X causes Y.
   - Point one about agreement
   - Point two about consensus

2. **Contradictions**: Model A says P, Model B says not P.

3. **Partial Coverage**: Only model A covered the SQL injection angle.
   - SQL injection mitigation strategies

5. **Blind Spots**: Neither model addressed the cost implications.
   - Cost analysis is missing

## Final Answer

Here is the synthesized answer...`

	analysis := extractAnalysis(answer)
	if analysis == nil {
		t.Fatal("analysis is nil")
	}

	if len(analysis.Consensus) == 0 {
		t.Error("expected consensus points")
	}
	if len(analysis.BlindSpots) == 0 {
		t.Error("expected blind spots")
	}
}

func TestExtractAnalysis_NoSections(t *testing.T) {
	answer := "Just a plain answer without any structured sections."
	analysis := extractAnalysis(answer)
	if analysis == nil {
		t.Fatal("analysis is nil")
	}
	if analysis.Consensus != nil {
		t.Error("expected nil consensus for unstructured answer")
	}
}

func TestBuildAnalysisPrompt(t *testing.T) {
	s := NewSynthesizer(nil, 0)
	pctx := PromptContext{
		OriginalQuestion:  "What is X?",
		PanelResponses:    []string{"X is a programming language."},
		PanelLabels:       []string{"openai/gpt-4o"},
		AnalysisDepth:     AnalysisStandard,
	}
	prompt := s.PromptBuilder().Build(pctx)

	if !contains(prompt, "original question") && !contains(prompt, "ORIGINAL QUESTION") {
		t.Error("prompt should contain the original question marker")
	}
	if !contains(prompt, "X is a programming language") {
		t.Error("prompt should contain panel responses")
	}
	if !contains(prompt, "X is a programming language") {
		t.Error("prompt should contain panel response")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsString(s, substr)
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
