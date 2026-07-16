package builtin

import (
	"context"
	"strings"

	"github.com/lhy/openfusion/internal/guard"
	"github.com/lhy/openfusion/internal/types"
)

// ToxicityGuard performs basic keyword-based toxicity filtering.
type ToxicityGuard struct {
	// High-severity patterns (score >= 0.9, block)
	blockList []toxPattern
	// Medium-severity patterns (score >= 0.6, warn)
	warnList []toxPattern
}

type toxPattern struct {
	pattern  string
	score    float64
	category string
}

// NewToxicityGuard creates a keyword-based toxicity filter guard.
func NewToxicityGuard() *ToxicityGuard {
	return &ToxicityGuard{
		blockList: buildBlockList(),
		warnList:  buildWarnList(),
	}
}

// Name returns the guard name.
func (g *ToxicityGuard) Name() string { return "toxicity" }

// CheckInput scans the request for toxic content.
func (g *ToxicityGuard) CheckInput(_ context.Context, req *types.ChatRequest) (*guard.GuardResult, error) {
	text := extractRequestText(req)
	return g.scan(text), nil
}

// CheckOutput scans the response for toxic content.
func (g *ToxicityGuard) CheckOutput(_ context.Context, resp *types.ChatResponse) (*guard.GuardResult, error) {
	text := extractResponseText(resp)
	return g.scan(text), nil
}

func (g *ToxicityGuard) scan(text string) *guard.GuardResult {
	lower := strings.ToLower(text)

	// Check block-list first (highest priority)
	var highestScore float64
	var worstCategory string
	var matchedBlock []string
	var matchedWarn []string

	for _, p := range g.blockList {
		if strings.Contains(lower, p.pattern) {
			if p.score > highestScore {
				highestScore = p.score
				worstCategory = p.category
			}
			matchedBlock = append(matchedBlock, p.pattern)
		}
	}

	if len(matchedBlock) > 0 {
		return guard.Block("toxicity: "+worstCategory+" ("+strings.Join(matchedBlock[:min(3, len(matchedBlock))], ", ")+")", highestScore)
	}

	// Check warn-list
	for _, p := range g.warnList {
		if strings.Contains(lower, p.pattern) {
			if p.score > highestScore {
				highestScore = p.score
				worstCategory = p.category
			}
			matchedWarn = append(matchedWarn, p.pattern)
		}
	}

	if len(matchedWarn) > 0 {
		return guard.Warn("toxicity: "+worstCategory, highestScore)
	}

	return nil
}

func buildBlockList() []toxPattern {
	return []toxPattern{
		{pattern: "kill yourself", score: 1.0, category: "self-harm"},
		{pattern: "commit suicide", score: 1.0, category: "self-harm"},
		{pattern: "how to make a bomb", score: 0.98, category: "violence"},
		{pattern: "how to commit murder", score: 0.98, category: "violence"},
		{pattern: "child pornography", score: 1.0, category: "csam"},
		{pattern: "child abuse", score: 1.0, category: "csam"},
		{pattern: "sexual exploitation of minors", score: 1.0, category: "csam"},
		{pattern: "terrorist attack", score: 0.95, category: "terrorism"},
		{pattern: "bomb-making instructions", score: 0.98, category: "violence"},
		{pattern: "mass shooting", score: 0.95, category: "violence"},
		{pattern: "genocide", score: 0.92, category: "hate-speech"},
		{pattern: "ethnic cleansing", score: 0.95, category: "hate-speech"},
		{pattern: "white supremacy", score: 0.90, category: "hate-speech"},
		{pattern: "neo-nazi", score: 0.90, category: "hate-speech"},
		{pattern: "racial slur", score: 0.90, category: "hate-speech"},
		{pattern: "hate crime", score: 0.90, category: "hate-speech"},
		{pattern: "fentanyl synthesis", score: 0.98, category: "drugs"},
		{pattern: "how to make meth", score: 0.95, category: "drugs"},
		{pattern: "synthesize illegal drugs", score: 0.95, category: "drugs"},
	}
}

func buildWarnList() []toxPattern {
	return []toxPattern{
		{pattern: "i want to die", score: 0.85, category: "self-harm"},
		{pattern: "i hate myself", score: 0.70, category: "self-harm"},
		{pattern: "i'm worthless", score: 0.65, category: "self-harm"},
		{pattern: "nobody loves me", score: 0.60, category: "distress"},
		{pattern: "i can't go on", score: 0.80, category: "self-harm"},
		{pattern: "what's the point of living", score: 0.82, category: "self-harm"},
		{pattern: "violent overthrow", score: 0.80, category: "violence"},
		{pattern: "armed rebellion", score: 0.80, category: "violence"},
		{pattern: "assassination", score: 0.85, category: "violence"},
		{pattern: "torture methods", score: 0.88, category: "violence"},
		{pattern: "incite violence", score: 0.80, category: "violence"},
		{pattern: "hate speech", score: 0.75, category: "hate-speech"},
		{pattern: "discriminate against", score: 0.70, category: "hate-speech"},
		{pattern: "racial profiling", score: 0.72, category: "hate-speech"},
		{pattern: "xenophobic", score: 0.70, category: "hate-speech"},
		{pattern: "misogynistic", score: 0.70, category: "hate-speech"},
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
