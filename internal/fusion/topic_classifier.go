package fusion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// TopicClassification is the result of classifying a user request by topic type.
type TopicClassification struct {
	Topic      string  `json:"topic"`      // "open" | "fact" | "simple" | "medium"
	Confidence float64 `json:"confidence"` // [0,1]
	Reason     string  `json:"reason"`     // classifier's short justification
	Classifier string  `json:"classifier"` // e.g. "gemma4"
	Fallback   bool    `json:"fallback"`   // true if confidence below threshold → conservative fallback
}

// TopicClassifier classifies requests into topic tiers using a fast local model (Gemma4 on 115:8012).
// Gemma4's chat template loops forever on /v1/chat/completions, so this calls /v1/completions directly.
type TopicClassifier struct {
	url       string // e.g. http://10.10.10.115:8012/v1/completions
	model     string // e.g. "gemma-4-26B-A4B-it"
	timeout   time.Duration
	threshold float64
	client    *http.Client
}

// NewTopicClassifier creates a TopicClassifier.
// classifierURL points at the fast model's /v1/completions endpoint (e.g. 115:8012).
func NewTopicClassifier(classifierURL, model string, timeout time.Duration, threshold float64) *TopicClassifier {
	if classifierURL == "" {
		classifierURL = "http://10.10.10.115:8012/v1/completions"
	}
	if model == "" {
		model = "gemma-4-26B-A4B-it"
	}
	if timeout <= 0 {
		timeout = 12 * time.Second
	}
	if threshold <= 0 {
		threshold = 0.7
	}
	return &TopicClassifier{
		url:       classifierURL,
		model:     model,
		timeout:   timeout,
		threshold: threshold,
		client:    &http.Client{Timeout: timeout},
	}
}

// classifyPrompt instructs the fast model to output a JSON classification.
const classifyPrompt = `<bos><start_of_turn>user
把下面的问题分类为三种类型之一，只输出JSON，不要其他文字：
- "open": 开放型/综述型/方法论/多视角分析（如"比较A和B的利弊"、"讨论未来趋势"、"方法论上有哪些张力"）
- "fact": 强事实型/需要具体数字/安全合规约束（如"XX的评级是什么"、"收益率多少"、"是否合规"）
- "simple": 简单直接/常识/低复杂度（如"1+1=?"、"翻译一句话"）

JSON格式: {"topic": "open|fact|simple", "confidence": 0.0到1.0的小数, "reason": "一句话理由"}

问题: {question}
<end_of_turn>
<start_of_turn>model
`

// Classify runs the classifier and returns the topic with confidence.
// On any failure it returns the conservative "medium" topic with Fallback=true.
func (c *TopicClassifier) Classify(ctx context.Context, question string) (TopicClassification, error) {
	if len(question) > 3000 {
		question = question[:3000]
	}

	prompt := strings.Replace(classifyPrompt, "{question}", question, 1)

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	payload := map[string]any{
		"model":       c.model,
		"prompt":      prompt,
		"max_tokens":  200,
		"temperature": 0.0,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return TopicClassification{Topic: "medium", Confidence: 0, Reason: err.Error(), Classifier: c.model, Fallback: true}, nil
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return TopicClassification{Topic: "medium", Confidence: 0, Reason: "classifier unreachable: " + err.Error(), Classifier: c.model, Fallback: true}, nil
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))

	out := extractCompletionText(raw)
	tc := parseClassification(out)
	if tc.Topic == "" {
		return TopicClassification{Topic: "medium", Confidence: 0, Reason: "unparseable: " + truncate(out, 120), Classifier: c.model, Fallback: true}, nil
	}
	tc.Classifier = c.model
	if tc.Confidence < c.threshold {
		tc.Fallback = true
		tc.Reason = fmt.Sprintf("low confidence %.2f < %.2f", tc.Confidence, c.threshold) + "; " + tc.Reason
		tc.Topic = "medium"
	}
	return tc, nil
}

// extractCompletionText parses a vLLM /v1/completions response.
func extractCompletionText(raw []byte) string {
	var d struct {
		Choices []struct {
			Text string `json:"text"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return strings.TrimSpace(string(raw))
	}
	if len(d.Choices) > 0 {
		return strings.TrimSpace(d.Choices[0].Text)
	}
	return ""
}

func parseClassification(out string) TopicClassification {
	s := strings.TrimSpace(out)
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return TopicClassification{}
	}
	var tc TopicClassification
	if err := json.Unmarshal([]byte(s[start:end+1]), &tc); err != nil {
		return TopicClassification{}
	}
	tc.Topic = strings.ToLower(strings.TrimSpace(tc.Topic))
	switch tc.Topic {
	case "open", "fact", "simple":
		// valid
	default:
		tc.Topic = ""
	}
	if tc.Confidence < 0 || tc.Confidence > 1 {
		tc.Confidence = 0
	}
	return tc
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
