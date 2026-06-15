// Package skill implements the Skill-based request routing and execution system.
//
// Skills replace static presets with responsive task strategies:
//   - Triggers define WHEN a skill activates (via RequestFeatures matching)
//   - Mode defines HOW to execute (direct / self-ensemble / fusion)
//   - Strategy defines WHAT to run (panel members, judge config)
package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/lhy/openfusion/internal/types"
)

// ---------------------------------------------------------------------------
// Execution mode
// ---------------------------------------------------------------------------

// Mode is one of: direct, self-ensemble, fusion.
type Mode string

const (
	ModeDirect        Mode = "direct"
	ModeSelfEnsemble  Mode = "self-ensemble"
	ModeFusion        Mode = "fusion"
)

// ---------------------------------------------------------------------------
// Triggers
// ---------------------------------------------------------------------------

// Trigger defines AND-level conditions.
// Multiple Trigger items are OR-ed together.
type Trigger struct {
	// Token range: "<300", ">1000", "500-2000", "500-", "-1000"
	Tokens string `yaml:"tokens,omitempty" json:"tokens,omitempty"`
	// MinTokens is a shorthand for tokens >= N
	MinTokens int `yaml:"min_tokens,omitempty" json:"min_tokens,omitempty"`
	// Categories is a regex match (e.g. "code|sql")
	Categories string `yaml:"categories,omitempty" json:"categories,omitempty"`
	// RequiresThink filters by think requirement
	RequiresThink *bool `yaml:"requires_think,omitempty" json:"requires_think,omitempty"`
	// HasTools filters by tool definition presence
	HasTools *bool `yaml:"has_tools,omitempty" json:"has_tools,omitempty"`
}

// Matches checks if all non-zero conditions in this trigger match the features.
func (t *Trigger) Matches(f *RequestFeatures) bool {
	// Token range check
	if t.Tokens != "" {
		if !matchTokenRange(t.Tokens, f.TokenCount) {
			return false
		}
	}
	if t.MinTokens > 0 && f.TokenCount < t.MinTokens {
		return false
	}

	// Category regex check
	if t.Categories != "" {
		if !matchCategories(t.Categories, f.Categories) {
			return false
		}
	}

	// Think requirement
	if t.RequiresThink != nil && *t.RequiresThink != f.RequiresThink {
		return false
	}

	// Tools
	if t.HasTools != nil && *t.HasTools != f.HasToolDefs {
		return false
	}

	return true
}

// matchTokenRange parses and matches token range expressions.
func matchTokenRange(expr string, val int) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return true
	}

	switch {
	case strings.HasPrefix(expr, "<"):
		// <300
		n := 0
		fmt.Sscanf(expr, "<%d", &n)
		return val < n
	case strings.HasPrefix(expr, ">"):
		// >1000
		n := 0
		fmt.Sscanf(expr, ">%d", &n)
		return val > n
	case strings.Contains(expr, "-"):
		// 500-2000, 500-, -1000
		parts := strings.SplitN(expr, "-", 2)
		lo, hi := 0, int(1<<31-1)
		if parts[0] != "" {
			fmt.Sscanf(parts[0], "%d", &lo)
		}
		if len(parts) > 1 && parts[1] != "" {
			fmt.Sscanf(parts[1], "%d", &hi)
		}
		return val >= lo && val <= hi
	default:
		// exact
		n := 0
		fmt.Sscanf(expr, "%d", &n)
		return val == n
	}
}

// matchCategories checks if any category in the features matches the pattern.
func matchCategories(pattern string, categories []string) bool {
	for _, cat := range categories {
		if pattern == cat {
			return true
		}
		// Simple glob: "code|sql" matches if any category starts with these
		for _, p := range strings.Split(pattern, "|") {
			p = strings.TrimSpace(p)
			if p == cat || (strings.HasSuffix(p, "*") && strings.HasPrefix(cat, strings.TrimSuffix(p, "*"))) {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// PanelMemberConfig
// ---------------------------------------------------------------------------

// PanelMemberConfig extends the base panel member with skill-specific params.
type PanelMemberConfig struct {
	Provider     string   `yaml:"provider" json:"provider"`
	Model        string   `yaml:"model" json:"model"`
	System       string   `yaml:"system,omitempty" json:"system,omitempty"`
	Think        *bool    `yaml:"think,omitempty" json:"think,omitempty"`
	Temperature  *float64 `yaml:"temperature,omitempty" json:"temperature,omitempty"`
}

// ToBase returns a base types.PanelMember.
func (pmc PanelMemberConfig) ToBase() types.PanelMember {
	return types.PanelMember{
		Provider: pmc.Provider,
		Model:    pmc.Model,
		System:   pmc.System,
	}
}

// JudgeConfig extends the base judge config with skill-specific params.
type JudgeConfig struct {
	Provider     string   `yaml:"provider,omitempty" json:"provider,omitempty"`
	Model        string   `yaml:"model,omitempty" json:"model,omitempty"`
	SystemPrompt string   `yaml:"system,omitempty" json:"system,omitempty"`
	Think        *bool    `yaml:"think,omitempty" json:"think,omitempty"`
	Temperature  *float64 `yaml:"temperature,omitempty" json:"temperature,omitempty"`
	Enabled      bool     `yaml:"enabled" json:"enabled"`
}

// ToBase returns a base types.JudgeConfig.
func (jc JudgeConfig) ToBase() types.JudgeConfig {
	return types.JudgeConfig{
		Provider:     jc.Provider,
		Model:        jc.Model,
		SystemPrompt: jc.SystemPrompt,
	}
}

// ---------------------------------------------------------------------------
// Strategy
// ---------------------------------------------------------------------------

// Strategy defines the execution plan for a skill.
type Strategy struct {
	// Provider + Model used for direct mode
	Provider string  `yaml:"provider,omitempty" json:"provider,omitempty"`
	Model    string  `yaml:"model,omitempty" json:"model,omitempty"`
	System   string  `yaml:"system,omitempty" json:"system,omitempty"`

	// Panel members (for self-ensemble or fusion)
	Panel []PanelMemberConfig `yaml:"panel,omitempty" json:"panel,omitempty"`

	// Judge configuration
	Judge JudgeConfig `yaml:"judge" json:"judge"`
}

// ---------------------------------------------------------------------------
// SkillParams
// ---------------------------------------------------------------------------

// SkillParams holds model invocation parameters.
type SkillParams struct {
	MaxTokens        int      `yaml:"max_tokens,omitempty" json:"max_tokens,omitempty"`
	Temperature      *float64 `yaml:"temperature,omitempty" json:"temperature,omitempty"`
	FrequencyPenalty float64  `yaml:"frequency_penalty,omitempty" json:"frequency_penalty,omitempty"`
	Think            *bool    `yaml:"think,omitempty" json:"think,omitempty"`
	ThinkBudget      int      `yaml:"think_budget,omitempty" json:"think_budget,omitempty"`
}

// ---------------------------------------------------------------------------
// Skill
// ---------------------------------------------------------------------------

// Skill defines a complete request-handling strategy.
type Skill struct {
	Name        string      `yaml:"name" json:"name"`
	Version     int         `yaml:"version,omitempty" json:"version,omitempty"`
	Description string      `yaml:"description,omitempty" json:"description,omitempty"`
	Triggers    []Trigger   `yaml:"triggers" json:"triggers"`
	Mode        Mode        `yaml:"mode" json:"mode"`
	Strategy    Strategy    `yaml:"strategy" json:"strategy"`
	Params      SkillParams `yaml:"params,omitempty" json:"params,omitempty"`
	Priority    int         `yaml:"priority,omitempty" json:"priority,omitempty"`
}

// Matches checks if the skill's triggers match the given features.
// Multiple triggers are OR-ed; conditions within a trigger are AND-ed.
func (s *Skill) Matches(f *RequestFeatures) bool {
	for _, t := range s.Triggers {
		if t.Matches(f) {
			return true
		}
	}
	return false
}

// Validate checks that the skill definition is internally consistent.
func (s *Skill) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("skill name is required")
	}
	switch s.Mode {
	case ModeDirect:
		if s.Strategy.Provider == "" || s.Strategy.Model == "" {
			return fmt.Errorf("direct mode requires provider and model")
		}
	case ModeSelfEnsemble:
		if len(s.Strategy.Panel) < 2 {
			return fmt.Errorf("self-ensemble requires at least 2 panel members")
		}
	case ModeFusion:
		if len(s.Strategy.Panel) < 2 {
			return fmt.Errorf("fusion requires at least 2 panel members")
		}
	default:
		return fmt.Errorf("unknown mode: %s", s.Mode)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Load / Parse
// ---------------------------------------------------------------------------

// LoadFile loads a single skill from a YAML file.
func LoadFile(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read skill file %s: %w", path, err)
	}

	var skill Skill
	if err := yaml.Unmarshal(data, &skill); err != nil {
		return nil, fmt.Errorf("parse skill %s: %w", path, err)
	}

	if err := skill.Validate(); err != nil {
		return nil, fmt.Errorf("validate skill %s: %w", path, err)
	}

	return &skill, nil
}

// LoadDir loads all .skill.yaml files from a directory.
func LoadDir(dir string) ([]*Skill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read skill dir %s: %w", dir, err)
	}

	var skills []*Skill
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".skill.yaml") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		skill, err := LoadFile(path)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", path, err)
		}
		skills = append(skills, skill)
	}

	return skills, nil
}

// ---------------------------------------------------------------------------
// Auto-generate from Preset
// ---------------------------------------------------------------------------

// FromPreset creates a skill from a types.Preset for backward compatibility.
// Generated skills have basic triggers from the description.
func FromPreset(p *types.Preset) *Skill {
	mode := ModeDirect
	if len(p.Panel) > 1 {
		if allSameModel(p.Panel) {
			mode = ModeSelfEnsemble
		} else {
			mode = ModeFusion
		}
	}

	panel := make([]PanelMemberConfig, len(p.Panel))
	for i, pm := range p.Panel {
		panel[i] = PanelMemberConfig{
			Provider: pm.Provider,
			Model:    pm.Model,
			System:   pm.System,
		}
	}

	return &Skill{
		Name:        p.Name,
		Description: p.Description,
		Triggers:    autoTriggers(p),
		Mode:        mode,
		Strategy: Strategy{
			Provider: func() string {
				if len(p.Panel) > 0 {
					return p.Panel[0].Provider
				}
				return ""
			}(),
			Model: func() string {
				if len(p.Panel) > 0 {
					return p.Panel[0].Model
				}
				return ""
			}(),
			Panel: panel,
		Judge: JudgeConfig{
			Provider: p.Judge.Provider,
			Model:    p.Judge.Model,
			SystemPrompt: p.Judge.SystemPrompt,
			Enabled:     true,
		},
		},
		Params:   SkillParams{},
		Priority: 0, // presets get lowest priority
	}
}

func allSameModel(panel []types.PanelMember) bool {
	if len(panel) == 0 {
		return true
	}
	first := panel[0].Provider + ":" + panel[0].Model
	for _, pm := range panel[1:] {
		if pm.Provider+":"+pm.Model != first {
			return false
		}
	}
	return true
}

func autoTriggers(p *types.Preset) []Trigger {
	// Generate triggers from the preset description
	desc := strings.ToLower(p.Description)
	var cats []string
	if strings.Contains(desc, "代码") || strings.Contains(desc, "coding") || strings.Contains(desc, "code") {
		cats = append(cats, "code")
	}
	if strings.Contains(desc, "研究") || strings.Contains(desc, "research") || strings.Contains(desc, "深度") {
		cats = append(cats, "research")
	}
	if strings.Contains(desc, "分析") || strings.Contains(desc, "analysis") || strings.Contains(desc, "决策") {
		cats = append(cats, "analysis")
	}
	if strings.Contains(desc, "平价") || strings.Contains(desc, "budget") || strings.Contains(desc, "简单") {
		cats = append(cats, "general")
	}

	triggers := []Trigger{}
	if len(cats) > 0 {
		triggers = append(triggers, Trigger{
			Categories: strings.Join(cats, "|"),
			MinTokens:  50,
		})
	}
	if len(cats) == 0 {
		// Fallback: match anything with medium+ complexity
		triggers = append(triggers, Trigger{
			MinTokens: 100,
		})
	}
	return triggers
}
