package verb

import (
	"fmt"
	"os"
	"path/filepath"
	"plugin"
)

// Plugin is the interface that plugins must implement
type Plugin interface {
	// GetVerbs returns the verbs provided by this plugin
	GetVerbs() []*Spec
}

// LoadPlugins loads verb plugins from a directory
func (r *Registry) LoadPlugins(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading verb plugins directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Only process .so files for plugins
		ext := filepath.Ext(entry.Name())
		if ext != ".so" {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		if err := r.loadPlugin(path); err != nil {
			return fmt.Errorf("loading plugin %s: %w", entry.Name(), err)
		}
	}

	return nil
}

// loadPlugin loads a verb plugin from a .so file
func (r *Registry) loadPlugin(path string) error {
	// Open the plugin
	p, err := plugin.Open(path)
	if err != nil {
		return fmt.Errorf("opening plugin: %w", err)
	}

	// Look up the exported symbol
	symVerbs, err := p.Lookup("VerbPlugin")
	if err != nil {
		return fmt.Errorf("lookup VerbPlugin symbol: %w", err)
	}

	// Assert that the symbol is a VerbPlugin
	verbPlugin, ok := symVerbs.(Plugin)
	if !ok {
		return fmt.Errorf("symbol VerbPlugin does not implement Plugin interface")
	}

	// Get the verbs and register them
	verbs := verbPlugin.GetVerbs()
	for _, verb := range verbs {
		if err := r.RegisterVerb(verb); err != nil {
			return fmt.Errorf("registering verb %s: %w", verb.Name, err)
		}
	}

	return nil
}

// PluginBuilder helps build verb plugins
type PluginBuilder struct {
	verbs []*Spec
}

// NewPluginBuilder creates a new plugin builder
func NewPluginBuilder() *PluginBuilder {
	return &PluginBuilder{
		verbs: make([]*Spec, 0),
	}
}

// AddVerb adds a verb to the plugin
func (b *PluginBuilder) AddVerb(spec *Spec) *PluginBuilder {
	b.verbs = append(b.verbs, spec)
	return b
}

// GetVerbs implements the Plugin interface
func (b *PluginBuilder) GetVerbs() []*Spec {
	return b.verbs
}

// SimpleVerbPlugin is a basic implementation of the Plugin interface
type SimpleVerbPlugin struct {
	verbs []*Spec
}

// NewSimpleVerbPlugin creates a new simple verb plugin
func NewSimpleVerbPlugin(verbs ...*Spec) *SimpleVerbPlugin {
	return &SimpleVerbPlugin{
		verbs: verbs,
	}
}

// GetVerbs implements the Plugin interface
func (p *SimpleVerbPlugin) GetVerbs() []*Spec {
	return p.verbs
}

// ExportPlugin exports a SimpleVerbPlugin as a plugin symbol
// This is used by plugin implementations
func ExportPlugin(plugin *SimpleVerbPlugin) Plugin {
	return plugin
}
