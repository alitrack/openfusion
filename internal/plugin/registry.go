package plugin

import "sync"

// Registry holds all registered plugins.
var (
	registryMu sync.RWMutex
	registry   = map[string]ModelPlugin{}
)

// Register adds a plugin (called from init() or main()).
// Panics on duplicate.
func Register(p ModelPlugin) {
	registryMu.Lock()
	defer registryMu.Unlock()
	name := p.Name()
	if _, exists := registry[name]; exists {
		panic("plugin already registered: " + name)
	}
	registry[name] = p
}

// Get returns a plugin by name (nil if not found).
func Get(name string) ModelPlugin {
	registryMu.RLock()
	p := registry[name]
	registryMu.RUnlock()
	return p
}

// List returns all registered plugin names.
func List() []string {
	registryMu.RLock()
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	registryMu.RUnlock()
	return names
}
