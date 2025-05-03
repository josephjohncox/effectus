// Package registry provides schema management for Effectus
package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/effectus/effectus-go/schema/types"
)

// Registry manages type definitions from multiple sources
type Registry struct {
	// typeSystem is the underlying type system
	typeSystem *types.TypeSystem

	// mu protects concurrent access
	mu sync.RWMutex

	// loaders is a list of registered schema loaders
	loaders []Loader
}

// Loader is the interface for schema loaders
type Loader interface {
	// CanLoad returns true if this loader can handle the given file
	CanLoad(path string) bool

	// Load loads type definitions from a file
	Load(path string, ts *types.TypeSystem) error
}

// NewRegistry creates a new schema registry
func NewRegistry() *Registry {
	return &Registry{
		typeSystem: types.NewTypeSystem(),
		loaders:    make([]Loader, 0),
	}
}

// GetTypeSystem returns the underlying type system
func (sr *Registry) GetTypeSystem() *types.TypeSystem {
	return sr.typeSystem
}

// RegisterLoader adds a schema loader to the registry
func (sr *Registry) RegisterLoader(loader Loader) {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	sr.loaders = append(sr.loaders, loader)
}

// LoadFile loads a schema file using an appropriate loader
func (sr *Registry) LoadFile(path string) error {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	// Find a loader that can handle this file
	for _, loader := range sr.loaders {
		if loader.CanLoad(path) {
			return loader.Load(path, sr.typeSystem)
		}
	}

	return fmt.Errorf("no loader found for file: %s", path)
}

// LoadDirectory scans a directory for schema files and loads them
func (sr *Registry) LoadDirectory(dir string) error {
	files, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading schema directory: %w", err)
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		path := filepath.Join(dir, file.Name())

		// Try each loader
		loaded := false
		for _, loader := range sr.loaders {
			if loader.CanLoad(path) {
				if err := loader.Load(path, sr.typeSystem); err != nil {
					return fmt.Errorf("loading schema file %s: %w", file.Name(), err)
				}
				loaded = true
				break
			}
		}

		if !loaded {
			// Skip files that no loader can handle
			continue
		}
	}

	return nil
}

// RegisterType registers a named type in the system
func (sr *Registry) RegisterType(name string, typ *types.Type) {
	sr.typeSystem.RegisterType(name, typ)
}

// GetType retrieves a type by name
func (sr *Registry) GetType(name string) (*types.Type, bool) {
	return sr.typeSystem.GetType(name)
}

// RegisterFactType registers a type for a fact path
func (sr *Registry) RegisterFactType(path string, typ *types.Type) error {
	return sr.typeSystem.RegisterFactType(path, typ)
}

// GetFactType retrieves the type for a fact path
func (sr *Registry) GetFactType(path string) (*types.Type, error) {
	return sr.typeSystem.GetFactType(path)
}

// GetAllFactPaths returns all registered fact paths
func (sr *Registry) GetAllFactPaths() []string {
	return sr.typeSystem.GetAllFactPaths()
}

// GetAllTypes returns all registered named types
func (sr *Registry) GetAllTypes() map[string]*types.Type {
	return sr.typeSystem.GetAllTypes()
}
