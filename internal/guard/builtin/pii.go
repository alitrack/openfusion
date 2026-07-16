package builtin

import (
	"context"
	"regexp"
	"strings"

	"github.com/lhy/openfusion/internal/guard"
	"github.com/lhy/openfusion/internal/types"
)

// PIIGuard detects personally identifiable information in requests and responses.
type PIIGuard struct {
	patterns []piiPattern
}

type piiPattern struct {
	name    string
	re      *regexp.Regexp
	score   float64
	action  guard.Action
}

// NewPIIGuard creates a PII detection guard with standard patterns.
func NewPIIGuard() *PIIGuard {
	return &PIIGuard{
		patterns: []piiPattern{
			// Email addresses
			{name: "email", re: regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`), score: 0.9, action: guard.ActionRedact},
			// US phone numbers
			{name: "phone_us", re: regexp.MustCompile(`\(?\d{3}\)?[\s.\-]?\d{3}[\s.\-]?\d{4}`), score: 0.7, action: guard.ActionWarn},
			// Social Security Numbers (with dashes)
			{name: "ssn", re: regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`), score: 0.95, action: guard.ActionBlock},
			// Credit card numbers (basic pattern for major brands)
			{name: "credit_card", re: regexp.MustCompile(`\b(?:\d[ \-]*?){13,19}\b`), score: 0.95, action: guard.ActionBlock},
			// IPv4 addresses (often sensitive in internal contexts)
			{name: "ipv4", re: regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\b`), score: 0.3, action: guard.ActionLog},
			// API keys / tokens (common patterns)
			{name: "api_key", re: regexp.MustCompile(`\b(sk-[a-zA-Z0-9]{32,}|AIza[0-9A-Za-z\-_]{35}|ghp_[a-zA-Z0-9]{36}|hf_[a-zA-Z0-9]{34})\b`), score: 0.98, action: guard.ActionBlock},
			// AWS access keys
			{name: "aws_key", re: regexp.MustCompile(`\b(AKIA[0-9A-Z]{16}|ASIA[0-9A-Z]{16})\b`), score: 0.98, action: guard.ActionBlock},
			// Private keys (PEM headers)
			{name: "private_key", re: regexp.MustCompile(`-----BEGIN (?:RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----`), score: 1.0, action: guard.ActionBlock},
		},
	}
}

// Name returns the guard name.
func (g *PIIGuard) Name() string { return "pii" }

// CheckInput scans the request messages for PII.
func (g *PIIGuard) CheckInput(_ context.Context, req *types.ChatRequest) (*guard.GuardResult, error) {
	text := extractRequestText(req)
	return g.scan(text), nil
}

// CheckOutput scans the response for PII leakage.
func (g *PIIGuard) CheckOutput(_ context.Context, resp *types.ChatResponse) (*guard.GuardResult, error) {
	text := extractResponseText(resp)
	return g.scan(text), nil
}

func (g *PIIGuard) scan(text string) *guard.GuardResult {
	var highestScore float64
	var worstReason string
	var worstAction guard.Action = guard.ActionLog

	for _, p := range g.patterns {
		matches := p.re.FindAllString(text, -1)
		if len(matches) > 0 {
			if p.score > highestScore {
				highestScore = p.score
				worstAction = p.action
				worstReason = "detected " + p.name + " pattern"
			}
			// Block immediately for block-level patterns
			if p.action == guard.ActionBlock {
				return guard.Block("pii: "+worstReason+" ("+strings.Join(uniqueFirst(matches, 3), ", ")+")", p.score)
			}
		}
	}

	if worstReason != "" {
		switch worstAction {
		case guard.ActionWarn:
			return guard.Warn("pii: "+worstReason, highestScore)
		case guard.ActionRedact:
			return &guard.GuardResult{Allowed: true, Score: highestScore, Reason: "pii: " + worstReason, Action: guard.ActionRedact}
		default:
			return &guard.GuardResult{Allowed: true, Score: highestScore, Reason: "pii: " + worstReason, Action: guard.ActionLog}
		}
	}

	return nil
}

func extractRequestText(req *types.ChatRequest) string {
	var b strings.Builder
	for _, m := range req.Messages {
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}

func extractResponseText(resp *types.ChatResponse) string {
	var b strings.Builder
	for _, c := range resp.Choices {
		b.WriteString(c.Message.Content)
		b.WriteString("\n")
	}
	return b.String()
}

func uniqueFirst(items []string, n int) []string {
	seen := make(map[string]bool)
	var out []string
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			out = append(out, item)
			if len(out) >= n {
				break
			}
		}
	}
	return out
}
