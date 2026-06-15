package skill

import (
	"strings"

	"github.com/lhy/openfusion/internal/types"
)

// ---------------------------------------------------------------------------
// RequestFeatures
// ---------------------------------------------------------------------------

// RequestFeatures is the structured summary of an incoming request.
// Skill triggers match against these fields.
type RequestFeatures struct {
	// Estimated token count of the user messages
	TokenCount int `json:"token_count"`

	// Content categories (code, sql, analysis, translation, greeting, general, research)
	Categories []string `json:"categories"`

	// Whether messages contain image URLs
	HasImages bool `json:"has_images"`

	// Whether messages contain tool/function definitions
	HasToolDefs bool `json:"has_tool_defs"`

	// Whether the request likely requires deep reasoning
	RequiresThink bool `json:"requires_think"`

	// Whether response_format is json_object or similar
	StructuredOutput bool `json:"structured_output"`

	// Complexity score (1-5)
	Complexity int `json:"complexity"`

	// Last user message text (for debugging)
	UserMessage string `json:"user_message,omitempty"`
}

// ---------------------------------------------------------------------------
// Analyzer
// ---------------------------------------------------------------------------

// categoryPatterns maps patterns to category labels.
var categoryPatterns = []struct {
	category string
	patterns []string
	weight   int // minimum matching patterns to trigger
}{
	{"code", []string{"```", "func ", "def ", "class ", "import ", "#include",
		"impl ", "fn ", "interface ", "struct ", "package ", "print(",
		"console.log", "function ", "var ", "let ", "const "}, 2},
	{"code", []string{"写一个", "implement", "write ", "create ",
		"实现", "代码", "source code",
		"python", "golang", "rust", "java", "javascript",
		"typescript", "c++", "c#",
		"线程安全", "thread-safe", "concurrent"}, 1},
	{"sql", []string{"select ", "from ", "where ", "join ", "create table",
		"insert into", "alter table", "group by", "order by"}, 2},
	{"translation", []string{"翻译", "translate", "convert this to",
		"把这个翻译成", "traduire"}, 1},
	{"greeting", []string{"你好", "hello", "hi", "hey", "早上好",
		"下午好", "晚上好", "good morning", "good afternoon"}, 1},
	{"analysis", []string{"分析", "比较", "对比", "优缺点", "pros and cons",
		"trade-off", "compare", "difference between", "vs "}, 1},
	{"research", []string{"深度", "调研", "research", "investigate",
		"why does", "how does", "原理", "机制", "explain in detail",
		"comprehensive", "thorough"}, 1},
}

// thinkPatterns are keywords that suggest deep reasoning is needed.
var thinkPatterns = []string{
	"为什么", "how", "why", "explain", "分析", "推理", "reason",
	"deep", "复杂", "复杂", "architecture", "design", "trade-off",
	"比较", "compare", "evaluate", "review", "debug", "optimize",
	"implement", "implement ", "function ", "class ", "algorithm",
	"parallel", "concurrent", "thread", "async", "performance",
	"refactor", "security", "vulnerability",
}

// AnalyzeRequest converts a ChatRequest into structured features.
func AnalyzeRequest(req *types.ChatRequest) *RequestFeatures {
	f := &RequestFeatures{}

	// Extract last user message
	lastMsg := types.ExtractLastUserMessage(req.Messages)
	f.UserMessage = lastMsg

	// Token estimation
	f.TokenCount = estimateTokens(req.Messages)

	// Content classification
	f.Categories = classifyContent(lastMsg)
	if len(f.Categories) == 0 {
		f.Categories = []string{"general"}
	}

	// Image check
	f.HasImages = hasImageContent(req.Messages)

	// Tool definition check
	f.HasToolDefs = len(req.Tools) > 0

	// Think requirement
	f.RequiresThink = requiresDeepThinking(lastMsg, f)

	// Structured output
	f.StructuredOutput = req.ResponseFormat != nil && req.ResponseFormat.Type == "json_object"

	// Complexity
	f.Complexity = scoreComplexity(f)

	return f
}

// ---------------------------------------------------------------------------
// Analysis helpers
// ---------------------------------------------------------------------------

func estimateTokens(messages []types.ChatMessage) int {
	total := 0
	for _, msg := range messages {
		runes := []rune(msg.Content)
		asciiCount := 0
		for _, ch := range runes {
			if ch < 128 {
				asciiCount++
			}
		}
		nonAscii := len(runes) - asciiCount
		total += nonAscii * 2      // Chinese ~2 token/char
		total += asciiCount / 4    // English ~0.25 token/char
		total += 4                 // overhead per message
	}
	return total
}

func classifyContent(msg string) []string {
	msg = strings.ToLower(msg)
	var categories []string

	for _, cp := range categoryPatterns {
		count := 0
		for _, p := range cp.patterns {
			if strings.Contains(msg, p) {
				count++
			}
		}
		if count >= cp.weight {
			categories = append(categories, cp.category)
		}
	}

	return categories
}

func hasImageContent(messages []types.ChatMessage) bool {
	// Check for image_url in message content
	for _, msg := range messages {
		if strings.Contains(msg.Content, "image_url") ||
			strings.Contains(msg.Content, "data:image/") ||
			strings.Contains(msg.Content, ".jpg") ||
			strings.Contains(msg.Content, ".png") {
			return true
		}
	}
	return false
}

func requiresDeepThinking(msg string, f *RequestFeatures) bool {
	// Long messages likely need reasoning
	if f.TokenCount > 1000 {
		return true
	}

	// Check if any research, analysis, code, or sql categories
	for _, cat := range f.Categories {
		switch cat {
		case "research", "analysis", "code", "sql":
			return true
		}
	}

	// Check think patterns
	msgLower := strings.ToLower(msg)
	for _, p := range thinkPatterns {
		if strings.Contains(msgLower, p) {
			return true
		}
	}

	return false
}

func scoreComplexity(f *RequestFeatures) int {
	score := 1

	if f.TokenCount > 500 {
		score++
	}
	if f.TokenCount > 2000 {
		score++
	}

	for _, cat := range f.Categories {
		switch cat {
		case "code", "sql", "analysis":
			score++
		case "research":
			score += 2
		}
	}

	if f.HasToolDefs {
		score++
	}
	if f.RequiresThink {
		score++
	}

	if score > 5 {
		score = 5
	}
	return score
}
