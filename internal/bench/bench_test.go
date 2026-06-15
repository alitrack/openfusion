package bench

import (
	"strings"
	"testing"
)

func TestLoadTestSet(t *testing.T) {
	ts, err := LoadTestSet()
	if err != nil {
		t.Fatalf("LoadTestSet() error: %v", err)
	}
	if ts.Version != 1 {
		t.Errorf("version = %d, want 1", ts.Version)
	}
	if len(ts.Tasks) != 10 {
		t.Errorf("tasks = %d, want 10", len(ts.Tasks))
	}
}

func TestTaskCategories(t *testing.T) {
	ts, _ := LoadTestSet()
	cats := map[string]int{}
	for _, task := range ts.Tasks {
		cats[task.Category]++
	}
	expected := map[string]int{"factual": 2, "reasoning": 2, "code": 2, "synthesis": 2, "controversial": 2}
	for cat, count := range expected {
		if cats[cat] != count {
			t.Errorf("category %s: got %d, want %d", cat, cats[cat], count)
		}
	}
}

func TestBuildJudgePrompt(t *testing.T) {
	ts, _ := LoadTestSet()
	task := ts.Tasks[0]
	prompt := BuildJudgePrompt(task, "Test response here")
	if !strings.Contains(prompt, task.Prompt) {
		t.Error("judge prompt missing question")
	}
	if !strings.Contains(prompt, task.ScoringCriteria) {
		t.Error("judge prompt missing scoring criteria")
	}
	if !strings.Contains(prompt, "Test response here") {
		t.Error("judge prompt missing response")
	}
}

func TestExtractScore(t *testing.T) {
	json := `{"accuracy": 85, "completeness": 75, "clarity": 90, "citation_rating": 60, "notes": "Good but missing some details"}`
	score, err := ExtractScore(json)
	if err != nil {
		t.Fatalf("ExtractScore error: %v", err)
	}
	if score.Accuracy != 85 || score.Completeness != 75 || score.Clarity != 90 || score.CitationRating != 60 {
		t.Errorf("score = %+v", score)
	}
	if score.Notes != "Good but missing some details" {
		t.Errorf("notes = %q", score.Notes)
	}
}

func TestExtractScore_Clamp(t *testing.T) {
	score, err := ExtractScore(`{"accuracy": 150, "completeness": -10, "clarity": 0, "citation_rating": 100}`)
	if err != nil {
		t.Fatalf("ExtractScore error: %v", err)
	}
	if score.Accuracy != 100 || score.Completeness != 0 {
		t.Errorf("clamp failed: %+v", score)
	}
}

func TestExtractScore_MarkdownWrapped(t *testing.T) {
	text := "```json\n{\"accuracy\": 80, \"completeness\": 70, \"clarity\": 85, \"citation_rating\": 65}\n```"
	score, err := ExtractScore(text)
	if err != nil {
		t.Fatalf("ExtractScore error for markdown: %v", err)
	}
	if score.Accuracy != 80 {
		t.Errorf("accuracy = %d, want 80", score.Accuracy)
	}
}

func TestScoreTotal(t *testing.T) {
	s := Score{Accuracy: 80, Completeness: 70, Clarity: 90, CitationRating: 60}
	expected := (80 + 70 + 90 + 60) / 4
	if s.Total() != expected {
		t.Errorf("Total() = %d, want %d", s.Total(), expected)
	}
}

func TestParseTestSet_Invalid(t *testing.T) {
	_, err := ParseTestSet([]byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "no tasks") {
		t.Errorf("expected 'no tasks' error, got: %v", err)
	}

	_, err = ParseTestSet([]byte(`{invalid json`))
	if err == nil {
		t.Error("expected parse error for invalid JSON")
	}
}

func TestExtractScore_InvalidJSON(t *testing.T) {
	_, err := ExtractScore("not json at all")
	if err == nil {
		t.Error("expected error for non-JSON input")
	}
}
