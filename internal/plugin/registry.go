package plugin

// Registry holds all registered plugins.
var registry = map[string]ModelPlugin{}

// Register adds a plugin (called from init() or main()).
// Panics on duplicate.
func Register(p ModelPlugin) {
	name := p.Name()
	if _, exists := registry[name]; exists {
		panic("plugin already registered: " + name)
	}
	registry[name] = p
}

// Get returns a plugin by name (nil if not found).
func Get(name string) ModelPlugin {
	return registry[name]
}

// List returns all registered plugin names.
func List() []string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	return names
}
