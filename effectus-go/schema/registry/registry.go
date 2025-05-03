package registry

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/effectus/effectus-go/schema/types"
	"github.com/tidwall/gjson"
)

// SchemaRegistry provides centralized management of schemas
type SchemaRegistry struct {
	// typeSystem manages type definitions
	typeSystem *types.TypeSystem

	// factData is the raw JSON data for facts
	factData string

	// loaders for different schema formats
	loaders []SchemaLoader
}

// SchemaLoader defines an interface for loading schemas from files
type SchemaLoader interface {
	// CanLoad checks if this loader can handle the given file
	CanLoad(path string) bool

	// Load loads type definitions from a file
	Load(path string, typeSystem *types.TypeSystem) error
}

// NewSchemaRegistry creates a new schema registry
func NewSchemaRegistry(typeSystem *types.TypeSystem) *SchemaRegistry {
	if typeSystem == nil {
		typeSystem = types.NewTypeSystem()
	}

	return &SchemaRegistry{
		typeSystem: typeSystem,
		loaders:    make([]SchemaLoader, 0),
	}
}

// RegisterLoader adds a schema loader to the registry
func (sr *SchemaRegistry) RegisterLoader(loader SchemaLoader) {
	sr.loaders = append(sr.loaders, loader)
}

// RegisterFactData sets the raw JSON data for facts
func (sr *SchemaRegistry) RegisterFactData(jsonData string) {
	sr.factData = jsonData
}

// GetFactValue gets a fact value from the registered fact data
func (sr *SchemaRegistry) GetFactValue(path string) (interface{}, bool) {
	if sr.factData == "" {
		return nil, false
	}

	result := gjson.Get(sr.factData, path)
	if !result.Exists() {
		return nil, false
	}

	// Convert to proper Go type
	var value interface{}
	switch result.Type {
	case gjson.True:
		value = true
	case gjson.False:
		value = false
	case gjson.Number:
		value = result.Num
	case gjson.String:
		value = result.Str
	case gjson.JSON:
		// Handle arrays and objects
		if result.IsArray() {
			value = result.Array()
		} else {
			value = result.Map()
		}
	default:
		value = nil
	}

	return value, true
}

// GetTypeSystem returns the underlying type system
func (sr *SchemaRegistry) GetTypeSystem() *types.TypeSystem {
	return sr.typeSystem
}

// LoadSchema loads a schema from a file
func (sr *SchemaRegistry) LoadSchema(path string) error {
	for _, loader := range sr.loaders {
		if loader.CanLoad(path) {
			return loader.Load(path, sr.typeSystem)
		}
	}
	return fmt.Errorf("no loader available for %s", path)
}

// LoadDirectory loads all schema files from a directory
func (sr *SchemaRegistry) LoadDirectory(dir string) error {
	files, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		return fmt.Errorf("reading schema directory: %w", err)
	}

	for _, path := range files {
		// Skip directories
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}

		// Try to load with each registered loader
		loaded := false
		for _, loader := range sr.loaders {
			if loader.CanLoad(path) {
				if err := loader.Load(path, sr.typeSystem); err != nil {
					return fmt.Errorf("loading schema file %s: %w", path, err)
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
