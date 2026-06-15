package skill

import (
	"testing"

	"github.com/lhy/openfusion/internal/types"
)

func TestCodeGenMatching(t *testing.T) {
	reg := NewRegistry()

	// Load explicit skills
	if err := reg.LoadDir("../../skills"); err != nil {
		t.Fatalf("load skills: %v", err)
	}
	t.Logf("Loaded %d skills", len(reg.List()))

	for _, s := range reg.List() {
		t.Logf("  skill %s (pri=%d) mode=%s", s.Name, s.Priority, s.Mode)
		for i, tr := range s.Triggers {
			t.Logf("    trigger[%d]: tokens=%q min=%d cats=%q think=%v tools=%v",
				i, tr.Tokens, tr.MinTokens, tr.Categories, tr.RequiresThink, tr.HasTools)
		}
	}

	// Test query
	req := &types.ChatRequest{
		Messages: []types.ChatMessage{
			{Role: "user", Content: "用 Python 写一个 LRU Cache，线程安全"},
		},
	}
	features := AnalyzeRequest(req)
	t.Logf("Features: T=%d C=%v Think=%v", features.TokenCount, features.Categories, features.RequiresThink)

	matcher := reg.Matcher("budget")
	matched := matcher.Match(features)

	if matched == nil {
		t.Fatal("NO SKILL MATCHED — auto-gen skills should at least match")
	}
	t.Logf("Matched: %s (mode=%s, pri=%d)", matched.Name, matched.Mode, matched.Priority)

	if matched.Name != "code-gen" {
		t.Logf("WARNING: expected code-gen, got %s", matched.Name)
	}

	// Test a simple greeting
	req2 := &types.ChatRequest{
		Messages: []types.ChatMessage{
			{Role: "user", Content: "你好"},
		},
	}
	f2 := AnalyzeRequest(req2)
	t.Logf("Greeting features: T=%d C=%v", f2.TokenCount, f2.Categories)
	matched2 := matcher.Match(f2)
	if matched2 == nil {
		t.Fatal("No skill matched for greeting!")
	}
	t.Logf("Greeting matched: %s", matched2.Name)
	if matched2.Name != "qa-simple" {
		t.Errorf("expected qa-simple for greeting, got %s", matched2.Name)
	}
}
