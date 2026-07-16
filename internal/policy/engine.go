// Package policy provides an agent governance engine for policy-based access control.
package policy

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/lhy/openfusion/internal/types"
)

// Action defines the enforcement action for a policy rule.
type Action string

const (
	ActionDeny     Action = "deny"
	ActionEscalate Action = "escalate"
	ActionWarn     Action = "warn"
	ActionLog      Action = "log"
)

// PolicyRule defines a single governance rule.
type PolicyRule struct {
	Name string   `yaml:"name"`
	When WhenCond `yaml:"when"`
	Then ThenAct  `yaml:"then"`
}

// WhenCond defines the conditions that trigger the rule.
type WhenCond struct {
	Conditions []Condition `yaml:"conditions"`
}

// Condition is a single predicate in a rule.
// Field is the attribute to check (user_tier, estimated_cost, tool_category, model_name).
// Op is the comparison operator (eq, neq, gt, gte, lt, lte, in, contains).
// Value is the comparison value.
type Condition struct {
	Field string `yaml:"field"`
	Op    string `yaml:"op"`
	Value any    `yaml:"value"`
}

// ThenAct defines the action taken when all conditions match.
type ThenAct struct {
	Action  Action `yaml:"action"`
	Message string `yaml:"message"`
}

// PolicyConfig is the top-level policy configuration.
type PolicyConfig struct {
	Version string       `yaml:"version"`
	Rules   []PolicyRule `yaml:"rules"`
}

// PolicyEvalContext provides the data used to evaluate policy conditions.
type PolicyEvalContext struct {
	UserTier       string  `yaml:"user_tier,omitempty" json:"user_tier,omitempty"`
	EstimatedCost  float64 `yaml:"estimated_cost,omitempty" json:"estimated_cost,omitempty"`
	ToolCategory   string  `yaml:"tool_category,omitempty" json:"tool_category,omitempty"`
	ModelName      string  `yaml:"model_name,omitempty" json:"model_name,omitempty"`
	RequestModel   string  `yaml:"request_model,omitempty" json:"request_model,omitempty"`
}

// PolicyEvalResult is returned by Evaluate.
type PolicyEvalResult struct {
	Allowed  bool              `json:"allowed"`
	Denials  []RuleMatch       `json:"denials,omitempty"`
	Warnings []RuleMatch       `json:"warnings,omitempty"`
	Escalations []RuleMatch    `json:"escalations,omitempty"`
}

// RuleMatch records a rule that fired and its action.
type RuleMatch struct {
	RuleName string `json:"rule_name"`
	Action   Action `json:"action"`
	Message  string `json:"message"`
}

// PolicyEngine loads and evaluates governance policies.
type PolicyEngine struct {
	rules []PolicyRule
}

// New creates an empty PolicyEngine.
func New() *PolicyEngine {
	return &PolicyEngine{}
}

// Load reads a YAML policy file and parses all rules.
func (e *PolicyEngine) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("policy load: read file: %w", err)
	}

	var cfg PolicyConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("policy load: parse yaml: %w", err)
	}

	e.rules = cfg.Rules
	return nil
}

// LoadConfig loads rules from a PolicyConfig directly.
func (e *PolicyEngine) LoadConfig(cfg *PolicyConfig) {
	if cfg != nil {
		e.rules = cfg.Rules
	}
}

// Evaluate checks all rules against the given context and returns the result.
// Any "deny" action makes the result Allowed=false.
func (e *PolicyEngine) Evaluate(ctx context.Context, evalCtx *PolicyEvalContext, req *types.ChatRequest) (*PolicyEvalResult, error) {
	result := &PolicyEvalResult{Allowed: true}

	for _, rule := range e.rules {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		if !e.matchAllConditions(rule.When.Conditions, evalCtx, req) {
			continue
		}

		match := RuleMatch{
			RuleName: rule.Name,
			Action:   rule.Then.Action,
			Message:  rule.Then.Message,
		}

		switch rule.Then.Action {
		case ActionDeny:
			result.Allowed = false
			result.Denials = append(result.Denials, match)
		case ActionEscalate:
			result.Escalations = append(result.Escalations, match)
		case ActionWarn:
			result.Warnings = append(result.Warnings, match)
		case ActionLog:
			// Log-only actions don't affect Allowed state
		}
	}

	return result, nil
}

// matchAllConditions returns true if all conditions in the rule match.
func (e *PolicyEngine) matchAllConditions(conds []Condition, evalCtx *PolicyEvalContext, _ *types.ChatRequest) bool {
	for _, c := range conds {
		fieldVal := e.getFieldValue(c.Field, evalCtx)
		if !e.matchCondition(c.Op, fieldVal, c.Value) {
			return false
		}
	}
	return true
}

// getFieldValue extracts the runtime value for a given field name.
func (e *PolicyEngine) getFieldValue(field string, evalCtx *PolicyEvalContext) any {
	switch field {
	case "user_tier":
		return evalCtx.UserTier
	case "estimated_cost":
		return evalCtx.EstimatedCost
	case "tool_category":
		return evalCtx.ToolCategory
	case "model_name":
		return evalCtx.ModelName
	case "request_model":
		return evalCtx.RequestModel
	default:
		return nil
	}
}

// matchCondition evaluates a single condition predicate.
func (e *PolicyEngine) matchCondition(op string, fieldVal, condVal any) bool {
	switch op {
	case "eq":
		return fmt.Sprint(fieldVal) == fmt.Sprint(condVal)
	case "neq":
		return fmt.Sprint(fieldVal) != fmt.Sprint(condVal)
	case "gt":
		return compareFloat(fieldVal, condVal) > 0
	case "gte":
		return compareFloat(fieldVal, condVal) >= 0
	case "lt":
		return compareFloat(fieldVal, condVal) < 0
	case "lte":
		return compareFloat(fieldVal, condVal) <= 0
	case "in":
		return isInList(fieldVal, condVal)
	case "contains":
		return strings.Contains(strings.ToLower(fmt.Sprint(fieldVal)), strings.ToLower(fmt.Sprint(condVal)))
	default:
		return false
	}
}

func compareFloat(a, b any) int {
	af := toFloat(a)
	bf := toFloat(b)
	if af < bf {
		return -1
	}
	if af > bf {
		return 1
	}
	return 0
}

func toFloat(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case string:
		var f float64
		fmt.Sscanf(val, "%f", &f)
		return f
	default:
		return 0
	}
}

func isInList(fieldVal, condVal any) bool {
	fieldStr := fmt.Sprint(fieldVal)
	switch v := condVal.(type) {
	case []any:
		for _, item := range v {
			if fmt.Sprint(item) == fieldStr {
				return true
			}
		}
	case []string:
		return slices.Contains(v, fieldStr)
	}
	return false
}

// Rules returns the current number of loaded rules.
func (e *PolicyEngine) RuleCount() int {
	return len(e.rules)
}
