package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/effectus/effectus-go/pathutil"
	"github.com/effectus/effectus-go/schema/types"
)

// JSONTypeInfo represents type information in JSON format
type JSONTypeInfo struct {
	Type       string                  `json:"type"`
	Items      *JSONTypeInfo           `json:"items,omitempty"`
	Properties map[string]JSONTypeInfo `json:"properties,omitempty"`
	Ref        string                  `json:"$ref,omitempty"`
}

// JSONSchemaLoader loads schemas from JSON files
type JSONSchemaLoader struct {
	// Namespace is the target namespace for facts
	Namespace string
}

// NewJSONSchemaLoader creates a new JSON schema loader
func NewJSONSchemaLoader(namespace string) *JSONSchemaLoader {
	return &JSONSchemaLoader{
		Namespace: namespace,
	}
}

// CanLoad checks if this loader can handle the given file
func (l *JSONSchemaLoader) CanLoad(path string) bool {
	ext := filepath.Ext(path)
	return ext == ".json"
}

// Load loads type definitions from a JSON file
func (l *JSONSchemaLoader) Load(path string, typeSystem *types.TypeSystem) error {
	// Read the file
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading schema file: %w", err)
	}

	// Parse the schema
	var schema struct {
		Types map[string]JSONTypeInfo `json:"types"`
		Facts map[string]JSONTypeInfo `json:"facts"`
	}

	if err := json.Unmarshal(data, &schema); err != nil {
		return fmt.Errorf("parsing schema file: %w", err)
	}

	// Register types
	for name, typeInfo := range schema.Types {
		typ, err := l.convertTypeInfo(typeInfo, typeSystem)
		if err != nil {
			return fmt.Errorf("converting type %s: %w", name, err)
		}
		typeSystem.RegisterType(name, typ)
	}

	// Register facts
	for path, typeInfo := range schema.Facts {
		typ, err := l.convertTypeInfo(typeInfo, typeSystem)
		if err != nil {
			return fmt.Errorf("converting fact type for path %s: %w", path, err)
		}

		// Full path with namespace
		fullPath := fmt.Sprintf("%s.%s", l.Namespace, path)
		parsedPath, err := pathutil.FromString(fullPath)
		if err != nil {
			return fmt.Errorf("parsing fact path %s: %w", fullPath, err)
		}
		typeSystem.RegisterFactType(parsedPath, typ)
	}

	return nil
}

// convertTypeInfo converts JSON type info to a Type
func (l *JSONSchemaLoader) convertTypeInfo(info JSONTypeInfo, ts *types.TypeSystem) (*types.Type, error) {
	// Handle references first
	if info.Ref != "" {
		refType, found := ts.GetType(info.Ref)
		if !found {
			return nil, fmt.Errorf("reference type not found: %s", info.Ref)
		}
		return refType, nil
	}

	// Create new type based on the type field
	switch info.Type {
	case "boolean":
		return types.NewBoolType(), nil
	case "integer":
		return types.NewIntType(), nil
	case "number":
		return types.NewFloatType(), nil
	case "string":
		return types.NewStringType(), nil
	case "array":
		if info.Items == nil {
			return nil, fmt.Errorf("array type must have items")
		}
		itemType, err := l.convertTypeInfo(*info.Items, ts)
		if err != nil {
			return nil, err
		}
		return types.NewListType(itemType), nil
	case "object":
		objType := types.NewObjectType()
		for propName, propInfo := range info.Properties {
			propType, err := l.convertTypeInfo(propInfo, ts)
			if err != nil {
				return nil, fmt.Errorf("property %s: %w", propName, err)
			}
			objType.AddProperty(propName, propType)
		}
		return objType, nil
	default:
		return nil, fmt.Errorf("unknown type: %s", info.Type)
	}
}

// CreateFactProvider creates a memory provider from a JSON file
func (l *JSONSchemaLoader) CreateFactProvider(path string) (*pathutil.MemoryProvider, error) {
	// Read the file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading data file: %w", err)
	}

	// Parse the data
	var rawData map[string]interface{}
	if err := json.Unmarshal(data, &rawData); err != nil {
		return nil, fmt.Errorf("parsing data file: %w", err)
	}

	// Create memory provider
	provider := pathutil.NewMemoryProvider(rawData)
	return provider, nil
}
