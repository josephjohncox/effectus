// schema/registry.go
package schema

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// SchemaRegistry manages type definitions from multiple sources
type SchemaRegistry struct {
	typeSystem *TypeSystem
}

// NewSchemaRegistry creates a new schema registry
func NewSchemaRegistry() *SchemaRegistry {
	return &SchemaRegistry{
		typeSystem: NewTypeSystem(),
	}
}

// LoadDirectory scans a directory for schemas and loads them
func (sr *SchemaRegistry) LoadDirectory(dir string) error {
	files, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading schema directory: %w", err)
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		path := filepath.Join(dir, file.Name())
		ext := filepath.Ext(path)

		switch ext {
		case ".json":
			if err := sr.LoadJSONSchema(path); err != nil {
				return fmt.Errorf("loading JSON schema %s: %w", file.Name(), err)
			}
		case ".proto":
			if err := sr.LoadProtoSchema(path); err != nil {
				return fmt.Errorf("loading proto schema %s: %w", file.Name(), err)
			}
		}
	}

	return nil
}

// LoadJSONSchema loads a JSON schema file containing type definitions
func (sr *SchemaRegistry) LoadJSONSchema(path string) error {
	// Read the file
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading JSON schema file: %w", err)
	}

	// Parse the schema file
	var schemaEntries []struct {
		Path string `json:"path"`
		Type Type   `json:"type"`
	}

	if err := json.Unmarshal(content, &schemaEntries); err != nil {
		return fmt.Errorf("parsing JSON schema file: %w", err)
	}

	// Add each entry to the type system
	for _, entry := range schemaEntries {
		sr.typeSystem.RegisterFactType(entry.Path, &entry.Type)
	}

	return nil
}

// GetTypeSystem returns the underlying type system
func (sr *SchemaRegistry) GetTypeSystem() *TypeSystem {
	return sr.typeSystem
}
