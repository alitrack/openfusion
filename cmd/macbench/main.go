// Command macbench runs blind-judge comparison of OpenFusion local-fusion
// vs single models on macOS with Ollama + MLX + moon-bridge providers.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/lhy/openfusion/internal/fusion"
	"github.com/lhy/openfusion/internal/preset"
	"github.com/lhy/openfusion/internal/provider"
	"github.com/lhy/openfusion/internal/types"
)

var tasks = []struct{ id, category, prompt string }{
	{"ai-risk", "reasoning", "What are the three most credible existential risks from advanced AI, ranked by expert consensus? Explain each in one sentence."},
	{"startup", "synthesis", "A startup with 3 engineers raised $2M seed. Should they build microservices or a monolith? Give the single most important reason for your choice, then list the top 3 risks of your choice."},
	{"merge", "code", "Write a Go function that merges two sorted integer slices into one sorted slice. Only the function, no explanation."},
}

type modelEntry struct {
	name     string
	provider string
	pbase    string
	pkey     string
	model    string
	isFusion bool
}

func main() {
	pm := provider.NewManager()
	pm.Register("ollama", provider.NewOpenAIAdapter("ollama", "http://127.0.0.1:11434", "ollama"))
	pm.Register("mlx-1234", provider.NewOpenAIAdapter("mlx-1234", "http://127.0.0.1:1234", "noop"))
	pm.Register("mlx-1235", provider.NewOpenAIAdapter("mlx-1235", "http://127.0.0.1:1235", "noop"))
	pm.Register("moon-bridge", provider.NewOpenAIAdapter("moon-bridge", "http://127.0.0.1:38440", "noop"))

	pr := preset.NewRegistry()
	pr.Register(&types.Preset{
		Name: "local-fusion",
		Panel: []types.PanelMember{
			{Provider: "ollama", Model: "qwen3.6:27b"},
			{Provider: "ollama", Model: "gemma4:26b"},
			{Provider: "ollama", Model: "qwen3.6:35B"},
			{Provider: "ollama", Model: "qwen3.6:27b-coding-mxfp8"},
			{Provider: "mlx-1234", Model: "mlx-community/gemma-4-12B-it-4bit"},
			{Provider: "mlx-1235", Model: "mlx-community/Qwen3-VL-8B-Instruct-4bit"},
		},
		Judge: types.JudgeConfig{Provider: "moon-bridge", Model: "deepseek-v4-pro"},
	})

	models := []modelEntry{
		{"local-fusion", "", "", "", "local-fusion", true},
		{"qwen27b-ollama", "ollama", "http://127.0.0.1:11434", "ollama", "qwen3.6:27b", false},
		{"gemma4-ollama", "ollama", "http://127.0.0.1:11434", "ollama", "gemma4:26b", false},
		{"qwen35b-ollama", "ollama", "http://127.0.0.1:11434", "ollama", "qwen3.6:35B", false},
		{"qwen27b-coding", "ollama", "http://127.0.0.1:11434", "ollama", "qwen3.6:27b-coding-mxfp8", false},
		{"gemma12b-mlx", "mlx-1234", "http://127.0.0.1:1234", "noop", "mlx-community/gemma-4-12B-it-4bit", false},
		{"qwen8b-vl-mlx", "mlx-1235", "http://127.0.0.1:1235", "noop", "mlx-community/Qwen3-VL-8B-Instruct-4bit", false},
		{"ds4-pro", "moon-bridge", "http://127.0.0.1:38440", "noop", "deepseek-v4-pro", false},
	}

	judgeProv := provider.NewOpenAIAdapter("moon-bridge", "http://127.0.0.1:38440", "noop")
	fusionEngine := fusion.NewEngine(pr, pm, 180*time.Second, 120*time.Second, 300*time.Second,
		nil, nil, nil, nil, nil, nil, fusion.NewModelRouter(fusion.DefaultRouterConfig()))

	scores := make(map[string][]int)

	for _, m := range models {
		fmt.Fprintf(os.Stderr, "\n▶ %s\n", m.name)
		for _, task := range tasks {
			var answer string
			var err error

			if m.isFusion {
				resp, e := fusionEngine.Execute("local-fusion", &types.ChatRequest{
					Model:     "openfusion/local-fusion",
					Messages:  []types.ChatMessage{{Role: "user", Content: task.prompt}},
					MaxTokens: 2048,
				})
				err = e
				if err == nil {
					answer = extractFinalAnswer(resp.Choices[0].Message.Content)
				}
			} else {
				prov := provider.NewOpenAIAdapter(m.provider, m.pbase, m.pkey)
				ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
				resp, e := prov.ChatCompletion(ctx, &types.ChatRequest{
					Model:     m.model,
					Messages:  []types.ChatMessage{{Role: "user", Content: task.prompt}},
					MaxTokens: 1024,
				})
				cancel()
				err = e
				if err == nil {
					answer = resp.Choices[0].Message.Content
				}
			}

			if err != nil {
				fmt.Fprintf(os.Stderr, "  %s: ERROR %v\n", task.id, err)
				scores[m.name] = append(scores[m.name], 0)
				continue
			}

			// Blind judge scoring
			score := blindJudge(judgeProv, task.prompt, answer, m.name)
			scores[m.name] = append(scores[m.name], score)
			fmt.Fprintf(os.Stderr, "  %s: %d/100\n", task.id, score)
		}
	}

	// Print results
	fmt.Println("\n═══════════════════════════════════════════════")
	fmt.Printf("%-25s %6s %6s %6s  %6s\n", "Model", "fact", "reas", "code", "AVG")
	fmt.Println("───────────────────────────────────────────────")

	bestSingle := 0.0
	for _, m := range models {
		s := scores[m.name]
		avg := float64(s[0]+s[1]+s[2]) / 3.0
		fmt.Printf("%-25s %5d  %5d  %5d  %6.1f\n", m.name, s[0], s[1], s[2], avg)
		if avg > bestSingle && !m.isFusion {
			bestSingle = avg
		}
	}

	fusionScores := scores["local-fusion"]
	fusionAvg := float64(fusionScores[0]+fusionScores[1]+fusionScores[2]) / 3.0
	lift := fusionAvg - bestSingle
	fmt.Printf("\n🔥 fusion_lift = %+.1f (local-fusion: %.1f vs best single: %.1f)\n", lift, fusionAvg, bestSingle)
}

func blindJudge(judgeProv provider.Provider, question, answer, modelName string) int {
	prompt := fmt.Sprintf(
		`Score this answer to "%s" on a scale of 0-100. Be strict and objective. Return ONLY the number.

Answer from %s:
%s`, question, modelName, truncate(answer, 1500))

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	resp, err := judgeProv.ChatCompletion(ctx, &types.ChatRequest{
		Model:     "deepseek-v4-pro",
		Messages:  []types.ChatMessage{{Role: "user", Content: prompt}},
		MaxTokens: 10,
	})
	cancel()

	if err != nil {
		return 0
	}

	text := strings.TrimSpace(resp.Choices[0].Message.Content)
	var score int
	fmt.Sscanf(text, "%d", &score)
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return score
}

func extractFinalAnswer(content string) string {
	// Find "## Final Answer" or "Final Answer" section
	idx := strings.Index(content, "## Final Answer")
	if idx < 0 {
		idx = strings.Index(content, "Final Answer")
	}
	if idx >= 0 {
		section := content[idx:]
		// Strip the header line
		nl := strings.Index(section, "\n")
		if nl > 0 {
			return strings.TrimSpace(section[nl:])
		}
		return strings.TrimSpace(section)
	}
	return content
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
