package pathutil

import (
	"sort"

	"github.com/effectus/effectus-go"
)

// UniverseRegistry groups fact providers by universe and namespace.
type UniverseRegistry struct {
	universes map[string]*Registry
}

// NewUniverseRegistry creates an empty universe registry.
func NewUniverseRegistry() *UniverseRegistry {
	return &UniverseRegistry{universes: make(map[string]*Registry)}
}

// Register adds a provider for a universe + namespace combination.
func (u *UniverseRegistry) Register(universe, namespace string, provider FactProvider) {
	if u == nil {
		return
	}
	if u.universes == nil {
		u.universes = make(map[string]*Registry)
	}
	reg := u.universes[universe]
	if reg == nil {
		reg = NewRegistry()
		u.universes[universe] = reg
	}
	reg.Register(namespace, provider)
}

// RegisterMerged registers a merged provider for a universe + namespace.
func (u *UniverseRegistry) RegisterMerged(universe, namespace string, sources []SourceProvider, strategy MergeStrategy) {
	u.Register(universe, namespace, NewMergedFactProvider(sources, strategy))
}

// Universe returns the registry for a given universe.
func (u *UniverseRegistry) Universe(universe string) *Registry {
	if u == nil {
		return nil
	}
	return u.universes[universe]
}

// Universes returns the list of registered universe names.
func (u *UniverseRegistry) Universes() []string {
	if u == nil {
		return nil
	}
	names := make([]string, 0, len(u.universes))
	for name := range u.universes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Get resolves a path within a universe.
func (u *UniverseRegistry) Get(universe, path string) (interface{}, bool) {
	if u == nil {
		return nil, false
	}
	reg := u.universes[universe]
	if reg == nil {
		return nil, false
	}
	return reg.Get(path)
}

// GetWithContext resolves a path within a universe with context.
func (u *UniverseRegistry) GetWithContext(universe, path string) (interface{}, *ResolutionResult) {
	if u == nil {
		return nil, &ResolutionResult{Path: path, Exists: false, Universe: universe}
	}
	reg := u.universes[universe]
	if reg == nil {
		return nil, &ResolutionResult{Path: path, Exists: false, Universe: universe}
	}
	value, result := reg.GetWithContext(path)
	if result != nil {
		result.Universe = universe
	}
	return value, result
}

// UniverseFacts scopes fact access to a specific universe.
type UniverseFacts struct {
	Universe string
	Registry *UniverseRegistry
	schema   effectus.SchemaInfo
}

// NewUniverseFacts creates a facts wrapper for a universe.
func NewUniverseFacts(registry *UniverseRegistry, universe string, schema effectus.SchemaInfo) *UniverseFacts {
	return &UniverseFacts{Universe: universe, Registry: registry, schema: schema}
}

// Get returns a value within the universe.
func (u *UniverseFacts) Get(path string) (interface{}, bool) {
	if u == nil || u.Registry == nil {
		return nil, false
	}
	if path == "" {
		reg := u.Registry.Universe(u.Universe)
		if reg == nil {
			return nil, false
		}
		snapshot := make(map[string]interface{})
		for namespace, provider := range reg.Providers() {
			if provider == nil {
				continue
			}
			if value, ok := provider.Get(""); ok {
				snapshot[namespace] = value
			}
		}
		if len(snapshot) == 0 {
			return nil, false
		}
		return snapshot, true
	}
	return u.Registry.Get(u.Universe, path)
}

// Schema returns the schema for this universe.
func (u *UniverseFacts) Schema() effectus.SchemaInfo {
	if u == nil {
		return nil
	}
	return u.schema
}
