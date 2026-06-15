package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/lhy/openfusion/internal/logger"
	"github.com/lhy/openfusion/internal/types"
)

// ---------------------------------------------------------------------------
// Registry
// ---------------------------------------------------------------------------

// Registry holds all skills and provides lookup by name and matching.
type Registry struct {
	mu       sync.RWMutex
	skills   []*Skill
	byName   map[string]*Skill
	defaults []*Skill // skills auto-generated from presets
}

// NewRegistry creates an empty skill registry.
func NewRegistry() *Registry {
	return &Registry{
		byName: make(map[string]*Skill),
	}
}

// LoadDir loads explicit skill files from a directory.
func (r *Registry) LoadDir(dir string) error {
	skills, err := LoadDir(dir)
	if err != nil {
		return fmt.Errorf("load skill dir: %w", err)
	}
	r.mu.Lock()
	for _, s := range skills {
		if err := r.addLocked(s); err != nil {
			r.mu.Unlock()
			return fmt.Errorf("add skill %s: %w", s.Name, err)
		}
		logger.Info("skill registered", "name", s.Name, "desc", s.Description, "mode", string(s.Mode), "priority", fmt.Sprintf("%d", s.Priority))
	}
	r.mu.Unlock()
	return nil
}

// LoadPresets auto-generates skills from presets (backward compat).
func (r *Registry) LoadPresets(presets []types.Preset) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range presets {
		fromPreset := FromPreset(&p)
		if existing, ok := r.byName[fromPreset.Name]; ok {
			// Explicit skill overrides auto-generated one
			logger.Info("preset auto-skill", "name", fromPreset.Name, "status", "overridden", "explicit", existing.Name)
			r.defaults = append(r.defaults, fromPreset)
			continue
		}
		r.byName[fromPreset.Name] = fromPreset
		r.defaults = append(r.defaults, fromPreset)
		logger.Info("preset auto-skill", "name", fromPreset.Name, "mode", string(fromPreset.Mode))
	}

	// Append defaults to skills list (lower priority)
	r.skills = append(r.skills, r.defaults...)
}

// addLocked adds a skill while holding r.mu.
func (r *Registry) addLocked(s *Skill) error {
	if _, exists := r.byName[s.Name]; exists {
		return fmt.Errorf("duplicate skill: %s", s.Name)
	}
	r.byName[s.Name] = s
	r.skills = append(r.skills, s)
	return nil
}

// Add registers an explicit skill.
func (r *Registry) Add(s *Skill) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.addLocked(s)
}

// Get returns a skill by name.
func (r *Registry) Get(name string) (*Skill, bool) {
	r.mu.RLock()
	s, ok := r.byName[name]
	r.mu.RUnlock()
	return s, ok
}

// List returns all registered skills.
func (r *Registry) List() []*Skill {
	r.mu.RLock()
	out := r.skills
	r.mu.RUnlock()
	return out
}

// Matcher creates a matcher from all registered skills.
func (r *Registry) Matcher(defaultRef string) *Matcher {
	return NewMatcher(r.List(), defaultRef)
}

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

// Config mirrors the YAML configuration for the skill system.
type Config struct {
	Dir     string `yaml:"dir"`     // skills/ directory path
	Default string `yaml:"default"` // default skill name for unmatched requests
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		Dir:     "skills",
		Default: "qa-simple",
	}
}

// ResolveSkillDir returns the absolute path to the skills directory.
// Checks: skills/ then presets/ then empty (no explicit skills).
func ResolveSkillDir(baseDir string) string {
	// Try explicit skills dir first
	skillDir := filepath.Join(baseDir, "skills")
	if dirExists(skillDir) {
		return skillDir
	}
	return ""
}

func dirExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
