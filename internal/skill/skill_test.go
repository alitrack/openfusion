package skill

import (
	"testing"

	"github.com/lhy/openfusion/internal/types"
)

func TestAnalyzeRequest_CodeDetection(t *testing.T) {
	req := &types.ChatRequest{
		Messages: []types.ChatMessage{
			{Role: "user", Content: "Write a Python class for LRU Cache with def get and def put methods, thread safe"},
		},
	}
	f := AnalyzeRequest(req)

	if !containsAny(f.Categories, []string{"code"}) {
		t.Errorf("expected category 'code', got %v", f.Categories)
	}
	if f.RequiresThink != true {
		t.Errorf("expected RequiresThink=true for code request")
	}
	if f.Complexity < 2 {
		t.Errorf("expected Complexity >= 2, got %d", f.Complexity)
	}
}

func TestAnalyzeRequest_CodeDetectionChinese(t *testing.T) {
	req := &types.ChatRequest{
		Messages: []types.ChatMessage{
			{Role: "user", Content: "用 Python 写一个 LRU Cache，线程安全"},
		},
	}
	f := AnalyzeRequest(req)

	if !containsAny(f.Categories, []string{"code"}) {
		t.Errorf("expected category 'code' for Chinese code request, got %v", f.Categories)
	}
}

func TestAnalyzeRequest_Greeting(t *testing.T) {
	req := &types.ChatRequest{
		Messages: []types.ChatMessage{
			{Role: "user", Content: "你好"},
		},
	}
	f := AnalyzeRequest(req)

	if !containsAny(f.Categories, []string{"greeting"}) {
		t.Errorf("expected category 'greeting', got %v", f.Categories)
	}
	if f.Complexity != 1 {
		t.Errorf("expected Complexity=1 for greeting, got %d", f.Complexity)
	}
	if f.RequiresThink {
		t.Errorf("expected RequiresThink=false for greeting")
	}
}

func TestAnalyzeRequest_SQL(t *testing.T) {
	req := &types.ChatRequest{
		Messages: []types.ChatMessage{
			{Role: "user", Content: "SELECT u.name, COUNT(o.id) FROM users u JOIN orders o ON u.id = o.user_id GROUP BY u.name"},
		},
	}
	f := AnalyzeRequest(req)

	if !containsAny(f.Categories, []string{"sql"}) {
		t.Errorf("expected category 'sql', got %v", f.Categories)
	}
}

func TestAnalyzeRequest_Research(t *testing.T) {
	req := &types.ChatRequest{
		Messages: []types.ChatMessage{
			{Role: "user", Content: "深度调研 Transformer 的注意力机制原理，给出 comprehensive 分析"},
		},
	}
	f := AnalyzeRequest(req)

	if !containsAny(f.Categories, []string{"research"}) {
		t.Errorf("expected category 'research', got %v", f.Categories)
	}
	if !f.RequiresThink {
		t.Errorf("expected RequiresThink=true for research")
	}
	if f.Complexity < 3 {
		t.Errorf("expected Complexity >= 3, got %d", f.Complexity)
	}
}

func TestAnalyzeRequest_Empty(t *testing.T) {
	req := &types.ChatRequest{
		Messages: []types.ChatMessage{
			{Role: "user", Content: ""},
		},
	}
	f := AnalyzeRequest(req)

	if !containsAny(f.Categories, []string{"general"}) {
		t.Errorf("expected fallback category 'general', got %v", f.Categories)
	}
	if f.TokenCount < 3 {
		t.Errorf("expected TokenCount >= 3 (overhead), got %d", f.TokenCount)
	}
}

func TestAnalyzeRequest_ToolDefs(t *testing.T) {
	req := &types.ChatRequest{
		Messages: []types.ChatMessage{
			{Role: "user", Content: "What's the weather in Tokyo?"},
		},
		Tools: []interface{}{map[string]interface{}{"type": "function"}},
	}
	f := AnalyzeRequest(req)

	if !f.HasToolDefs {
		t.Errorf("expected HasToolDefs=true when tools are present")
	}
}

func TestAnalyzeRequest_ImageCheck(t *testing.T) {
	tests := []struct {
		content   string
		hasImages bool
	}{
		{"Describe this image", false},
		{"What's in data:image/png;base64,abc...", true},
		{"Check this image_url", true},
		{"See attachment.jpg", true},
		{"Plain text only", false},
	}

	for _, tt := range tests {
		req := &types.ChatRequest{
			Messages: []types.ChatMessage{
				{Role: "user", Content: tt.content},
			},
		}
		f := AnalyzeRequest(req)
		if f.HasImages != tt.hasImages {
			t.Errorf("content=%q: expected HasImages=%v, got %v", tt.content, tt.hasImages, f.HasImages)
		}
	}
}

func TestTokenEstimate(t *testing.T) {
	tests := []struct {
		messages []types.ChatMessage
		min      int
		max      int
	}{
		{
			messages: []types.ChatMessage{
				{Role: "user", Content: "hello"}, // 5/4 + 4 ≈ 5
			},
			min: 3,
			max: 10,
		},
		{
			messages: []types.ChatMessage{
				{Role: "user", Content: "你好世界"}, // 4*2 + 4 ≈ 12
			},
			min: 8,
			max: 20,
		},
	}

	for _, tt := range tests {
		got := estimateTokens(tt.messages)
		if got < tt.min || got > tt.max {
			t.Errorf("messages=%v: expected token count between %d and %d, got %d", tt.messages, tt.min, tt.max, got)
		}
	}
}

func TestTrigger_TokenRange(t *testing.T) {
	tests := []struct {
		expr    string
		val     int
		expect  bool
	}{
		{"<300", 150, true},
		{"<300", 300, false},
		{">1000", 1500, true},
		{">1000", 500, false},
		{"500-2000", 1000, true},
		{"500-2000", 300, false},
		{"500-2000", 2500, false},
		{"500-", 500, true},
		{"500-", 10000, true},
		{"500-", 499, false},
		{"-1000", 500, true},
		{"-1000", 1500, false},
		{"500", 500, true},
		{"500", 501, false},
	}

	for _, tt := range tests {
		got := matchTokenRange(tt.expr, tt.val)
		if got != tt.expect {
			t.Errorf("matchTokenRange(%q, %d) = %v, want %v", tt.expr, tt.val, got, tt.expect)
		}
	}
}

func TestTrigger_Categories(t *testing.T) {
	tests := []struct {
		pattern    string
		categories []string
		expect     bool
	}{
		{"code", []string{"code"}, true},
		{"code", []string{"general"}, false},
		{"code|sql", []string{"sql"}, true},
		{"code|sql", []string{"general"}, false},
		{"code*", []string{"code-review"}, true},
	}

	for _, tt := range tests {
		got := matchCategories(tt.pattern, tt.categories)
		if got != tt.expect {
			t.Errorf("matchCategories(%q, %v) = %v, want %v", tt.pattern, tt.categories, got, tt.expect)
		}
	}
}

func TestTrigger_Match(t *testing.T) {
	tests := []struct {
		name    string
		trigger Trigger
		feat    RequestFeatures
		expect  bool
	}{
		{
			name:    "simple token match",
			trigger: Trigger{Tokens: "<300", Categories: "greeting"},
			feat:    RequestFeatures{TokenCount: 50, Categories: []string{"greeting"}},
			expect:  true,
		},
		{
			name:    "token mismatch",
			trigger: Trigger{Tokens: "<300"},
			feat:    RequestFeatures{TokenCount: 500},
			expect:  false,
		},
		{
			name:    "category mismatch",
			trigger: Trigger{Categories: "code"},
			feat:    RequestFeatures{Categories: []string{"general"}},
			expect:  false,
		},
		{
			name:    "think requirement",
			trigger: Trigger{RequiresThink: boolPtr(true)},
			feat:    RequestFeatures{RequiresThink: true},
			expect:  true,
		},
		{
			name:    "think mismatch",
			trigger: Trigger{RequiresThink: boolPtr(true)},
			feat:    RequestFeatures{RequiresThink: false},
			expect:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.trigger.Matches(&tt.feat)
			if got != tt.expect {
				t.Errorf("trigger %+v match features %+v = %v, want %v", tt.trigger, tt.feat, got, tt.expect)
			}
		})
	}
}

func TestFromPreset(t *testing.T) {
	preset := &types.Preset{
		Name:        "test-preset",
		Description: "测试研究的代码分析预设",
		Panel: []types.PanelMember{
			{Provider: "local-ds", Model: "deepseek-v4-flash", System: "严谨"},
			{Provider: "local-ds", Model: "deepseek-v4-flash", System: "创意"},
		},
		Judge: types.JudgeConfig{
			Provider: "local-ds",
			Model:    "deepseek-v4-flash",
		},
	}

	skill := FromPreset(preset)

	if skill.Name != "test-preset" {
		t.Errorf("expected name 'test-preset', got %s", skill.Name)
	}
	if skill.Mode != ModeSelfEnsemble {
		t.Errorf("expected ModeSelfEnsemble for same model, got %s", skill.Mode)
	}
	if len(skill.Strategy.Panel) != 2 {
		t.Errorf("expected 2 panel members, got %d", len(skill.Strategy.Panel))
	}

	// Check triggers generated from description
	if len(skill.Triggers) == 0 {
		t.Error("expected auto-generated triggers")
	}
}

func TestSkill_Validate(t *testing.T) {
	tests := []struct {
		name    string
		skill   Skill
		wantErr bool
	}{
		{
			name: "valid direct",
			skill: Skill{
				Name: "test",
				Mode: ModeDirect,
				Strategy: Strategy{
					Provider: "local-ds",
					Model:    "deepseek-v4-flash",
				},
			},
			wantErr: false,
		},
		{
			name: "direct missing provider",
			skill: Skill{
				Name: "test",
				Mode: ModeDirect,
				Strategy: Strategy{
					Model: "deepseek-v4-flash",
				},
			},
			wantErr: true,
		},
		{
			name: "self-ensemble needs 2 panel",
			skill: Skill{
				Name: "test",
				Mode: ModeSelfEnsemble,
				Strategy: Strategy{
					Panel: []PanelMemberConfig{
						{Provider: "a", Model: "m1"},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "valid self-ensemble",
			skill: Skill{
				Name: "test",
				Mode: ModeSelfEnsemble,
				Strategy: Strategy{
					Panel: []PanelMemberConfig{
						{Provider: "a", Model: "m1"},
						{Provider: "a", Model: "m1"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "empty name",
			skill: Skill{
				Mode: ModeDirect,
				Strategy: Strategy{
					Provider: "local-ds",
					Model:    "deepseek-v4-flash",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.skill.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestSkill_Matches(t *testing.T) {
	s := &Skill{
		Name: "test",
		Triggers: []Trigger{
			{Categories: "code", MinTokens: 100},
			{Categories: "research"},
		},
	}

	if !s.Matches(&RequestFeatures{Categories: []string{"code"}, TokenCount: 200}) {
		t.Error("expected code trigger to match")
	}
	if !s.Matches(&RequestFeatures{Categories: []string{"research"}}) {
		t.Error("expected research trigger to match")
	}
	if s.Matches(&RequestFeatures{Categories: []string{"greeting"}}) {
		t.Error("expected greeting to NOT match")
	}
	if s.Matches(&RequestFeatures{Categories: []string{"code"}, TokenCount: 50}) {
		t.Error("expected code with low tokens to NOT match (min_tokens=100)")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func boolPtr(b bool) *bool {
	return &b
}
