// Package preset manages named panel + judge combinations.
package preset

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/lhy/openfusion/internal/types"
)

// Registry holds all available presets, indexed by name.
type Registry struct {
	presets map[string]*types.Preset
}

// NewRegistry creates an empty preset registry.
func NewRegistry() *Registry {
	return &Registry{
		presets: make(map[string]*types.Preset),
	}
}

// Register adds a single preset to the registry.
func (r *Registry) Register(p *types.Preset) error {
	name := normalizeName(p.Name)
	if _, exists := r.presets[name]; exists {
		return fmt.Errorf("duplicate preset: %s", name)
	}
	r.presets[name] = p
	return nil
}

// Get returns a preset by name.
func (r *Registry) Get(name string) (*types.Preset, bool) {
	p, ok := r.presets[normalizeName(name)]
	return p, ok
}

// List returns all registered preset names with descriptions.
func (r *Registry) List() []types.Preset {
	result := make([]types.Preset, 0, len(r.presets))
	for _, p := range r.presets {
		result = append(result, *p)
	}
	return result
}

// LoadDir scans a directory for .yaml preset files and registers them.
func (r *Registry) LoadDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // directory is optional
		}
		return fmt.Errorf("read preset dir %s: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".yaml") && !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		p, err := loadFile(path)
		if err != nil {
			return fmt.Errorf("load preset %s: %w", entry.Name(), err)
		}
		if err := r.Register(p); err != nil {
			return fmt.Errorf("register preset %s: %w", entry.Name(), err)
		}
	}
	return nil
}

// LoadInline presets from config inline definitions.
func (r *Registry) LoadInline(items map[string]ConfigInlinePreset) error {
	for name, item := range items {
		p := &types.Preset{
			Name:        name,
			Description: item.Description,
			Panel:       item.Panel,
			Judge:       item.Judge,
		}
		if err := r.Register(p); err != nil {
			return fmt.Errorf("register inline preset %s: %w", name, err)
		}
	}
	return nil
}

// ConfigInlinePreset is the shape used in config.yaml for inline presets.
type ConfigInlinePreset struct {
	Description string              `yaml:"description"`
	Panel       []types.PanelMember `yaml:"panel"`
	Judge       types.JudgeConfig   `yaml:"judge"`
}

// loadFile reads a single preset YAML file.
func loadFile(path string) (*types.Preset, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var p types.Preset
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, err
	}

	if len(p.Panel) == 0 {
		return nil, fmt.Errorf("preset %q has empty panel", p.Name)
	}
	if p.Judge.Model == "" {
		return nil, fmt.Errorf("preset %q has empty judge model", p.Name)
	}

	return &p, nil
}

// normalizeName normalizes preset names: lowercase, strip "openfusion/" prefix.
func normalizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.TrimPrefix(name, "openfusion/")
	return name
}
