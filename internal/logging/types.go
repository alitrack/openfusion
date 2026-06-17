// Package logging provides async CSV fusion-logging with monthly rotation and zstd compression.
package logging

import "fmt"

// FusionLog captures one fusion request for offline analysis.
type FusionLog struct {
	FusionID        string `csv:"fusion_id"`
	Timestamp       string `csv:"timestamp"`
	Preset          string `csv:"preset"`
	Skill           string `csv:"skill"`
	Query           string `csv:"query"`
	ModelAName      string `csv:"model_a_name"`
	ModelAOutput    string `csv:"model_a_output"`
	ModelAStatus    string `csv:"model_a_status"`
	ModelANameB     string `csv:"model_b_name"`
	ModelBOutput    string `csv:"model_b_output"`
	ModelBStatus    string `csv:"model_b_status"`
	ModelCName      string `csv:"model_c_name"`
	ModelCOutput    string `csv:"model_c_output"`
	ModelCStatus    string `csv:"model_c_status"`
	JudgeModel      string `csv:"judge_model"`
	JudgeAnalysis   string `csv:"judge_analysis"`
	FinalAnswer     string `csv:"final_answer"`
	PromptTokens    int    `csv:"prompt_tokens"`
	CompletionTokens int   `csv:"completion_tokens"`
	TotalTokens     int    `csv:"total_tokens"`
	CostUSD         float64 `csv:"cost_usd"`
	LatencyMs       int64  `csv:"latency_ms"`
	PanelErrors     int    `csv:"panel_errors"`
}

// CSVHeaders returns column names in order.
func (FusionLog) CSVHeaders() []string {
	return []string{
		"fusion_id", "timestamp", "preset", "skill", "query",
		"model_a_name", "model_a_output", "model_a_status",
		"model_b_name", "model_b_output", "model_b_status",
		"model_c_name", "model_c_output", "model_c_status",
		"judge_model", "judge_analysis", "final_answer",
		"prompt_tokens", "completion_tokens", "total_tokens",
		"cost_usd", "latency_ms", "panel_errors",
	}
}

// CSVRecord returns field values in header order.
func (f FusionLog) CSVRecord() []string {
	return []string{
		f.FusionID, f.Timestamp, f.Preset, f.Skill, f.Query,
		f.ModelAName, f.ModelAOutput, f.ModelAStatus,
		f.ModelANameB, f.ModelBOutput, f.ModelBStatus,
		f.ModelCName, f.ModelCOutput, f.ModelCStatus,
		f.JudgeModel, f.JudgeAnalysis, f.FinalAnswer,
		itoa(f.PromptTokens), itoa(f.CompletionTokens), itoa(f.TotalTokens),
		ftoa(f.CostUSD), itoa64(f.LatencyMs), itoa(f.PanelErrors),
	}
}

// KnowledgeEntry is a high-quality fusion record extracted for the knowledge base.
type KnowledgeEntry struct {
	FusionID     string  `csv:"fusion_id"`
	Timestamp    string  `csv:"timestamp"`
	Skill        string  `csv:"skill"`
	Query        string  `csv:"query"`
	PanelSummary string  `csv:"panel_summary"`
	JudgeAnalysis string `csv:"judge_analysis"`
	FinalAnswer  string  `csv:"final_answer"`
	QualityScore float64 `csv:"quality_score"`
}

// CSVHeaders returns column names in order.
func (KnowledgeEntry) CSVHeaders() []string {
	return []string{
		"fusion_id", "timestamp", "skill", "query",
		"panel_summary", "judge_analysis", "final_answer", "quality_score",
	}
}

// CSVRecord returns field values in header order.
func (k KnowledgeEntry) CSVRecord() []string {
	return []string{
		k.FusionID, k.Timestamp, k.Skill, k.Query,
		k.PanelSummary, k.JudgeAnalysis, k.FinalAnswer, ftoa(k.QualityScore),
	}
}

// ---------------------------------------------------------------------------
// small helpers — avoid strconv import in the hot log path
// ---------------------------------------------------------------------------

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func ftoa(f float64) string {
	if f == 0 {
		return "0"
	}
	// Simple formatting: up to 6 decimal places, strip trailing zeros
	s := fmt.Sprintf("%.6f", f)
	// Strip trailing zeros after decimal point
	i := len(s) - 1
	for i >= 0 && s[i] == '0' {
		i--
	}
	if s[i] == '.' {
		i--
	}
	if i < 0 {
		return "0"
	}
	return s[:i+1]
}
