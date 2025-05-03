// schema/verb_plugins.go

package schema

import (
	"fmt"
	"os"
	"path/filepath"
	"plugin"
)

// VerbPlugin is the interface that plugins must implement
type VerbPlugin interface {
	// GetVerbs returns the verbs provided by this plugin
	GetVerbs() []*VerbSpec
}

// LoadVerbPlugins loads verb plugins from a directory
func (vr *VerbRegistry) LoadVerbPlugins(dir string) error {
	files, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading verb plugins directory: %w", err)
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		ext := filepath.Ext(file.Name())
		if ext != ".so" {
			continue
		}

		path := filepath.Join(dir, file.Name())
		if err := vr.loadPlugin(path); err != nil {
			return fmt.Errorf("loading plugin %s: %w", file.Name(), err)
		}
	}

	return nil
}

// loadPlugin loads a verb plugin
func (vr *VerbRegistry) loadPlugin(path string) error {
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
	verbPlugin, ok := symVerbs.(VerbPlugin)
	if !ok {
		return fmt.Errorf("symbol VerbPlugin does not implement VerbPlugin interface")
	}

	// Get the verbs and register them
	verbs := verbPlugin.GetVerbs()
	for _, verb := range verbs {
		if err := vr.RegisterVerb(verb); err != nil {
			return fmt.Errorf("registering verb %s: %w", verb.Name, err)
		}
	}

	return nil
}

// SetVerbExecutor sets the executor for a registered verb
func (vr *VerbRegistry) SetVerbExecutor(name string, executor VerbExecutor) error {
	verb, exists := vr.verbs[name]
	if !exists {
		return fmt.Errorf("verb %s not registered", name)
	}

	verb.Executor = executor
	return nil
}
