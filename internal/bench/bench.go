// Package bench provides benchmark infrastructure for measuring fusion quality.
package bench

import (
	"encoding/json"
	"fmt"
)

// Task represents a single benchmark task.
type Task struct {
	ID              string `json:"id"`
	Category        string `json:"category"`
	Prompt          string `json:"prompt"`
	Reference       string `json:"reference"`
	ScoringCriteria string `json:"scoring_criteria"`
}

// TestSet holds all benchmark tasks.
type TestSet struct {
	Version     int    `json:"version"`
	Description string `json:"description"`
	Tasks       []Task `json:"tasks"`
}

// Score holds the judge's evaluation for one response.
type Score struct {
	Accuracy       int    `json:"accuracy"`
	Completeness   int    `json:"completeness"`
	Clarity        int    `json:"clarity"`
	CitationRating int    `json:"citation_rating"`
	Notes          string `json:"notes,omitempty"`
}

// Total returns the average of all score dimensions.
func (s Score) Total() int {
	return (s.Accuracy + s.Completeness + s.Clarity + s.CitationRating) / 4
}

// TaskResult holds the final output for one task-variant combination.
type TaskResult struct {
	TaskID   string `json:"task_id"`
	Variant  string `json:"variant"`
	Response string `json:"response"`
	Score    Score  `json:"score"`
	JudgeRaw string `json:"judge_raw,omitempty"`
}

// Report is the complete comparison output.
type Report struct {
	TestSetDescription string       `json:"description"`
	Variants           []string     `json:"variants"`
	Results            []TaskResult `json:"results"`
	SummaryRows        []SummaryRow `json:"summary,omitempty"`
}

// SummaryRow is a variant's aggregate performance.
type SummaryRow struct {
	Variant      string  `json:"variant"`
	AvgScore     float64 `json:"avg_score"`
	TotalTokens  int     `json:"total_tokens"`
	TotalCostUSD float64 `json:"total_cost_usd"`
}

// LoadTestSet loads the embedded benchmark test set.
func LoadTestSet() (*TestSet, error) {
	return ParseTestSet(testSetRaw)
}

// ParseTestSet parses JSON into a TestSet.
func ParseTestSet(data []byte) (*TestSet, error) {
	var ts TestSet
	if err := json.Unmarshal(data, &ts); err != nil {
		return nil, fmt.Errorf("parse test set: %w", err)
	}
	if len(ts.Tasks) == 0 {
		return nil, fmt.Errorf("test set has no tasks")
	}
	return &ts, nil
}

// BuildJudgePrompt creates the scoring prompt for a task + response.
func BuildJudgePrompt(task Task, response string) string {
	return fmt.Sprintf(`You are an expert evaluator. Score the following response to a question on four dimensions (0-100 each).

## Question
%s

## Reference / Scoring Criteria
%s

## Response to Evaluate
%s

Return ONLY a JSON object with this exact structure (no markdown, no explanation):
{"accuracy": 85, "completeness": 80, "clarity": 90, "citation_rating": 75, "notes": "Brief reasoning for scores"}

Scoring guidelines:
- accuracy: factual correctness (0=completely wrong, 100=perfect)
- completeness: coverage of key points (0=all missed, 100=all covered)
- clarity: organization and readability (0=incoherent, 100=well-structured)
- citation_rating: use of evidence when relevant (0=none, 100=well-cited; deduct reasonably if citations aren't applicable)
- notes: brief 1-sentence justification
`, task.Prompt, task.ScoringCriteria, response)
}

// ExtractScore parses the judge's JSON response into a Score.
func ExtractScore(judgeResponse string) (Score, error) {
	// Try to find and parse JSON from within the response
	// The judge should return pure JSON, but sometimes wraps in markdown
	return parseScoreJSON(judgeResponse)
}

func parseScoreJSON(text string) (Score, error) {
	// Strip markdown code blocks if present
	cleaned := text
	if len(cleaned) >= 3 && cleaned[:3] == "```" {
		// Find closing ```
		start := 3
		if idx := indexAny(cleaned[start:], "\n\r"); idx >= 0 {
			start += idx + 1
		}
		end := len(cleaned)
		if idx := lastIndex(cleaned, "```"); idx > start {
			end = idx
		}
		cleaned = cleaned[start:end]
	}

	var s Score
	if err := json.Unmarshal([]byte(cleaned), &s); err != nil {
		return s, fmt.Errorf("parse score JSON: %w (text: %.100s)", err, text)
	}
	// Clamp to 0-100
	s.Accuracy = clamp(s.Accuracy)
	s.Completeness = clamp(s.Completeness)
	s.Clarity = clamp(s.Clarity)
	s.CitationRating = clamp(s.CitationRating)
	return s, nil
}

func clamp(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func indexAny(s, chars string) int {
	for i := 0; i < len(s); i++ {
		for j := 0; j < len(chars); j++ {
			if s[i] == chars[j] {
				return i
			}
		}
	}
	return -1
}

func lastIndex(s, substr string) int {
	for i := len(s) - len(substr); i >= 0; i-- {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
