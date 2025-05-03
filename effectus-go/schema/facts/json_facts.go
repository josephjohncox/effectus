package facts

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/effectus/effectus-go/schema/common"
)

// JSONFacts provides a JSON-based implementation of the Facts interface
type JSONFacts struct {
	data   map[string]interface{}
	schema SchemaInfo
}

// NewJSONFacts creates a new JSONFacts instance from a data map
func NewJSONFacts(data map[string]interface{}) *JSONFacts {
	return &JSONFacts{
		data:   data,
		schema: NewSimpleSchema(),
	}
}

// NewJSONFactsFromString creates a new JSONFacts from a JSON string
func NewJSONFactsFromString(jsonStr string) (*JSONFacts, error) {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return nil, fmt.Errorf("error unmarshaling JSON: %w", err)
	}

	return NewJSONFacts(data), nil
}

// Get retrieves a fact value by path
func (f *JSONFacts) Get(path string) (interface{}, bool) {
	value, info := f.GetWithContext(path)
	return value, info != nil && info.Exists
}

// GetWithContext retrieves a fact with detailed resolution information
func (f *JSONFacts) GetWithContext(path string) (interface{}, *common.ResolutionResult) {
	if f.data == nil {
		return nil, &common.ResolutionResult{
			Exists: false,
			Error:  fmt.Errorf("no data"),
			Path:   path,
		}
	}

	// Parse and resolve the path
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return nil, &common.ResolutionResult{
			Exists: false,
			Error:  fmt.Errorf("invalid path: %s", path),
			Path:   path,
		}
	}

	// Traverse the path
	current := f.data
	for i, part := range parts {
		// Parse array index if present
		name, index := extractIndexFromSegment(part)

		if i < len(parts)-1 {
			// We're at an intermediate segment

			if index >= 0 {
				// Handle array index
				if arr, ok := current[name].([]interface{}); ok {
					if index < len(arr) {
						if mapVal, ok := arr[index].(map[string]interface{}); ok {
							current = mapVal
							continue
						} else {
							return nil, &common.ResolutionResult{
								Exists: false,
								Error:  fmt.Errorf("path segment %s[%d] is not a map", name, index),
								Path:   path,
							}
						}
					} else {
						return nil, &common.ResolutionResult{
							Exists: false,
							Error:  fmt.Errorf("index %d out of bounds for array %s", index, name),
							Path:   path,
						}
					}
				} else {
					return nil, &common.ResolutionResult{
						Exists: false,
						Error:  fmt.Errorf("path segment %s is not an array", name),
						Path:   path,
					}
				}
			} else {
				// Regular object traversal
				if nextMap, ok := current[name].(map[string]interface{}); ok {
					current = nextMap
				} else {
					return nil, &common.ResolutionResult{
						Exists: false,
						Error:  fmt.Errorf("path segment %s is not a map", name),
						Path:   path,
					}
				}
			}
		} else {
			// We're at the final segment
			if index >= 0 {
				// Handle array index for final segment
				if arr, ok := current[name].([]interface{}); ok {
					if index < len(arr) {
						value := arr[index]
						return value, &common.ResolutionResult{
							Exists:    true,
							Value:     value,
							Path:      path,
							ValueType: f.schema.GetPathType(path),
						}
					} else {
						return nil, &common.ResolutionResult{
							Exists: false,
							Error:  fmt.Errorf("index %d out of bounds for array %s", index, name),
							Path:   path,
						}
					}
				} else {
					return nil, &common.ResolutionResult{
						Exists: false,
						Error:  fmt.Errorf("path segment %s is not an array", name),
						Path:   path,
					}
				}
			} else {
				// Regular property access
				if value, ok := current[name]; ok {
					return value, &common.ResolutionResult{
						Exists:    true,
						Value:     value,
						Path:      path,
						ValueType: f.schema.GetPathType(path),
					}
				} else {
					return nil, &common.ResolutionResult{
						Exists: false,
						Error:  fmt.Errorf("path segment %s does not exist", name),
						Path:   path,
					}
				}
			}
		}
	}

	// If we get here, we found a value at the root level
	return current, &common.ResolutionResult{
		Exists:    true,
		Value:     current,
		Path:      path,
		ValueType: f.schema.GetPathType(path),
	}
}

// extractIndexFromSegment parses array indexing from segment names (e.g., "items[0]")
func extractIndexFromSegment(segment string) (string, int) {
	// Find opening and closing brackets
	openBracket := strings.Index(segment, "[")
	closeBracket := strings.Index(segment, "]")

	if openBracket >= 0 && closeBracket > openBracket {
		name := segment[:openBracket]
		indexStr := segment[openBracket+1 : closeBracket]

		// Try to parse index
		var index int
		if _, err := fmt.Sscanf(indexStr, "%d", &index); err == nil {
			return name, index
		}
	}

	// No valid index found
	return segment, -1
}

// Schema returns the schema information
func (f *JSONFacts) Schema() SchemaInfo {
	return f.schema
}

// HasPath checks if a path exists
func (f *JSONFacts) HasPath(path string) bool {
	_, exists := f.Get(path)
	return exists
}

// GetData returns the underlying data map - exposed for tests
func (f *JSONFacts) GetData() map[string]interface{} {
	return f.data
}

// RegisterJSONTypes registers types from JSON data with a type system
func (f *JSONFacts) RegisterJSONTypes(ts common.TypeSystem, namespace string) {
	if f.data == nil {
		return
	}

	if nsData, ok := f.data[namespace]; ok {
		if nsMap, ok := nsData.(map[string]interface{}); ok {
			registerTypesFromMap(ts, nsMap, namespace, "")
		}
	}
}

// RegisterJSONTypes registers common JSON value types with a type system
func RegisterJSONTypes(ts common.TypeSystem) error {
	// Register primitive types if needed
	return nil
}

// Helper function to register types from a nested map structure
func registerTypesFromMap(ts common.TypeSystem, data map[string]interface{}, namespace, prefix string) {
	for key, value := range data {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		fullPath := namespace + "." + path

		// Register the type based on the value
		typ := inferTypeFromValue(value)
		ts.RegisterFactType(fullPath, typ)

		// Recurse into nested maps
		if nestedMap, ok := value.(map[string]interface{}); ok {
			registerTypesFromMap(ts, nestedMap, namespace, path)
		}
	}
}

// inferTypeFromValue infers a type from a JSON value
func inferTypeFromValue(value interface{}) *common.Type {
	if value == nil {
		return &common.Type{PrimType: common.TypeUnknown}
	}

	switch v := value.(type) {
	case bool:
		return &common.Type{PrimType: common.TypeBool}
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return &common.Type{PrimType: common.TypeInt}
	case float32, float64:
		return &common.Type{PrimType: common.TypeFloat}
	case string:
		return &common.Type{PrimType: common.TypeString}
	case []interface{}:
		// For arrays/lists, infer element type from first element if available
		elemType := &common.Type{PrimType: common.TypeUnknown}
		if len(v) > 0 {
			elemType = inferTypeFromValue(v[0])
		}
		return &common.Type{
			PrimType: common.TypeList,
			ListType: elemType,
		}
	case map[string]interface{}:
		// For maps, we can only determine key type (string)
		return &common.Type{
			PrimType:   common.TypeMap,
			MapKeyType: &common.Type{PrimType: common.TypeString},
			MapValType: &common.Type{PrimType: common.TypeUnknown},
		}
	default:
		return &common.Type{PrimType: common.TypeUnknown}
	}
}
