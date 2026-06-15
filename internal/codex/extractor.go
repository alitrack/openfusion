// Package codex provides structured code output extraction from fusion results.
//
// When a request has "codex": true, the fusion pipeline generates code via
// self-ensemble (two panel members with different perspectives, one judge).
// This package extracts structured code blocks, explanations, and tests
// from the judge's natural language output.
package codex

import (
	"regexp"
	"strings"

	"github.com/lhy/openfusion/internal/types"
)

// languagePatterns maps common code block language identifiers.
// Order matters: first match wins.
var languagePatterns = []struct {
	pattern *regexp.Regexp
	lang    string
	ext     string
}{
	{regexp.MustCompile(`(?i)\bpython\b`), "python", ".py"},
	{regexp.MustCompile(`(?i)\bjavascript\b`), "javascript", ".js"},
	{regexp.MustCompile(`(?i)\btypescript\b`), "typescript", ".ts"},
	{regexp.MustCompile(`(?i)\bgo\b`), "go", ".go"},
	{regexp.MustCompile(`(?i)\brust\b`), "rust", ".rs"},
	{regexp.MustCompile(`(?i)\bjava\b`), "java", ".java"},
	{regexp.MustCompile(`(?i)\bcpp\b|\bc\+\+\b`), "cpp", ".cpp"},
	{regexp.MustCompile(`(?i)\bc#\b|\bcsharp\b`), "csharp", ".cs"},
	{regexp.MustCompile(`(?i)\bsql\b`), "sql", ".sql"},
	{regexp.MustCompile(`(?i)\byaml\b`), "yaml", ".yaml"},
	{regexp.MustCompile(`(?i)\bjson\b`), "json", ".json"},
	{regexp.MustCompile(`(?i)\bbash\b|\bshell\b`), "bash", ".sh"},
}

// codeBlockRegex matches ```language ... ``` blocks.
var codeBlockRegex = regexp.MustCompile("```(\\w*)\\n([\\s\\S]*?)```")

// Extract parses a judge's final answer and returns structured code output.
// The answer is expected to contain:
//   - Code blocks (```lang ... ```)
//   - An explanation section
//   - Optionally, a tests section
func Extract(answer string, panelCount int) *types.CodexResponse {
	cx := &types.CodexResponse{
		Analysis: &types.CodexAnalysis{
			PanelCount: panelCount,
		},
	}

	// Extract code blocks
	blocks := codeBlockRegex.FindAllStringSubmatch(answer, -1)

	// Determine primary language from content
	cx.Language = detectLanguage(answer, blocks)

	// Build files and separate test code
	var mainFiles []types.CodexFile
	var testContent strings.Builder

	for _, block := range blocks {
		lang := block[1]
		if lang == "" {
			lang = inferLanguage(block[2])
		}
		content := strings.TrimSpace(block[2])
		if content == "" {
			continue
		}

		ext := languageExt(lang)
		fileName := "main" + ext

		// Check if this looks like test code
		if isTestCode(content, lang) {
			testFileName := "main_test" + ext
			if testContent.Len() > 0 {
				testContent.WriteString("\n\n")
			}
			testContent.WriteString(content)
			mainFiles = append(mainFiles, types.CodexFile{
				Path:     testFileName,
				Content:  content,
				Language: lang,
			})
		} else {
			mainFiles = append(mainFiles, types.CodexFile{
				Path:     fileName,
				Content:  content,
				Language: lang,
			})
		}
	}

	cx.Files = mainFiles
	cx.Tests = strings.TrimSpace(testContent.String())

	// Extract explanation (text before/after code blocks)
	cx.Explanation = extractExplanation(answer, blocks)

	return cx
}

// detectLanguage determines the primary programming language.
func detectLanguage(answer string, blocks [][]string) string {
	// First pass: check code block annotations
	for _, block := range blocks {
		if block[1] != "" {
			lang := strings.ToLower(block[1])
			if _, ok := knownLang(lang); ok {
				return lang
			}
		}
	}

	// Second pass: check for textual mentions
	for _, lp := range languagePatterns {
		if lp.pattern.MatchString(answer) {
			return lp.lang
		}
	}

	return "text"
}

func knownLang(lang string) (string, bool) {
	known := map[string]string{
		"python": "python", "py": "python",
		"javascript": "javascript", "js": "javascript",
		"typescript": "typescript", "ts": "typescript",
		"go": "go", "golang": "go",
		"rust": "rust", "rs": "rust",
		"java": "java",
		"cpp": "cpp", "c++": "cpp", "c": "cpp",
		"csharp": "csharp", "cs": "csharp",
		"sql": "sql",
		"bash": "bash", "shell": "bash", "sh": "bash",
		"yaml": "yaml", "yml": "yaml",
		"json": "json",
	}
	v, ok := known[lang]
	return v, ok
}

func languageExt(lang string) string {
	for _, lp := range languagePatterns {
		if lp.lang == lang {
			return lp.ext
		}
	}
	return ".txt"
}

func inferLanguage(code string) string {
	code = strings.TrimSpace(code)
	if strings.Contains(code, "def ") || strings.Contains(code, "import ") {
		return "python"
	}
	if strings.Contains(code, "func ") || strings.Contains(code, "package ") {
		return "go"
	}
	if strings.Contains(code, "fn ") {
		return "rust"
	}
	if strings.Contains(code, "SELECT ") || strings.Contains(code, "FROM ") {
		return "sql"
	}
	if strings.Contains(code, "function ") || strings.Contains(code, "console.log") {
		return "javascript"
	}
	return "text"
}

func isTestCode(content, lang string) bool {
	contentLower := strings.ToLower(content)
	switch lang {
	case "go":
		return strings.Contains(content, "func test") || strings.Contains(content, "func Test")
	case "python":
		return strings.Contains(contentLower, "def test_") || strings.Contains(contentLower, "unittest")
	case "javascript", "typescript":
		return strings.Contains(contentLower, "describe(") || strings.Contains(contentLower, "it(") ||
			strings.Contains(contentLower, "test(")
	default:
		return false
	}
}

func extractExplanation(answer string, blocks [][]string) string {
	// Remove all code blocks from the answer
	cleaned := codeBlockRegex.ReplaceAllString(answer, "")
	// Remove extra blank lines
	cleaned = regexp.MustCompile(`\n{3,}`).ReplaceAllString(cleaned, "\n\n")
	return strings.TrimSpace(cleaned)
}
