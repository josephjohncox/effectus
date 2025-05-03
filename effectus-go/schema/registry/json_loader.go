package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/effectus/effectus-go/schema/types"
)

// JSONSchemaLoader loads schemas from JSON files
type JSONSchemaLoader struct {
	// Optional prefix for fact paths
	pathPrefix string
}

// NewJSONSchemaLoader creates a new JSON schema loader
func NewJSONSchemaLoader(prefix string) *JSONSchemaLoader {
	return &JSONSchemaLoader{
		pathPrefix: prefix,
	}
}

// CanLoad checks if this loader can handle the given file
func (l *JSONSchemaLoader) CanLoad(path string) bool {
	ext := filepath.Ext(path)
	return ext == ".json"
}

// Load loads type definitions from a JSON file
func (l *JSONSchemaLoader) Load(path string, ts *types.TypeSystem) error {
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
		typ, err := l.convertType(typeInfo, ts)
		if err != nil {
			return fmt.Errorf("converting type %s: %w", name, err)
		}

		ts.RegisterType(name, typ)
	}

	// Register facts
	for path, typeInfo := range schema.Facts {
		typ, err := l.convertType(typeInfo, ts)
		if err != nil {
			return fmt.Errorf("converting fact type for path %s: %w", path, err)
		}

		// Add prefix if specified
		fullPath := path
		if l.pathPrefix != "" {
			fullPath = l.pathPrefix + "." + path
		}

		ts.RegisterFactType(fullPath, typ)
	}

	return nil
}

// JSONTypeInfo represents a type definition in JSON
type JSONTypeInfo struct {
	// Type is the primitive type name
	Type string `json:"type"`

	// Name is a reference to a named type
	Name string `json:"name,omitempty"`

	// ElementType is the element type for lists
	ElementType *JSONTypeInfo `json:"element_type,omitempty"`

	// KeyType is the key type for maps
	KeyType *JSONTypeInfo `json:"key_type,omitempty"`

	// ValueType is the value type for maps
	ValueType *JSONTypeInfo `json:"value_type,omitempty"`
}

// convertType converts a JSON type info to a Type
func (l *JSONSchemaLoader) convertType(info JSONTypeInfo, ts *types.TypeSystem) (*types.Type, error) {
	// Reference to a named type
	if info.Name != "" {
		return &types.Type{
			Name: info.Name,
		}, nil
	}

	// Convert primitive type
	var primType types.PrimitiveType
	switch info.Type {
	case "bool":
		primType = types.TypeBool
	case "int":
		primType = types.TypeInt
	case "float":
		primType = types.TypeFloat
	case "string":
		primType = types.TypeString
	case "list":
		primType = types.TypeList
	case "map":
		primType = types.TypeMap
	case "time":
		primType = types.TypeTime
	case "date":
		primType = types.TypeDate
	case "duration":
		primType = types.TypeDuration
	default:
		return nil, fmt.Errorf("unknown type: %s", info.Type)
	}

	typ := &types.Type{
		PrimType: primType,
	}

	// Handle list element type
	if primType == types.TypeList && info.ElementType != nil {
		elemType, err := l.convertType(*info.ElementType, ts)
		if err != nil {
			return nil, fmt.Errorf("converting list element type: %w", err)
		}
		typ.ListType = elemType
	}

	// Handle map key and value types
	if primType == types.TypeMap {
		if info.KeyType != nil {
			keyType, err := l.convertType(*info.KeyType, ts)
			if err != nil {
				return nil, fmt.Errorf("converting map key type: %w", err)
			}
			typ.MapKeyType = keyType
		} else {
			// Default key type is string
			typ.MapKeyType = &types.Type{PrimType: types.TypeString}
		}

		if info.ValueType != nil {
			valType, err := l.convertType(*info.ValueType, ts)
			if err != nil {
				return nil, fmt.Errorf("converting map value type: %w", err)
			}
			typ.MapValType = valType
		} else {
			// Default value type is unknown
			typ.MapValType = &types.Type{PrimType: types.TypeUnknown}
		}
	}

	return typ, nil
}
