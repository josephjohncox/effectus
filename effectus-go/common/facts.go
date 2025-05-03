package common

import (
	"errors"
	"fmt"
)

// BasicSchema provides a simple schema implementation
type BasicSchema struct {
	// Type mapping for paths
	typeInfo map[string]*Type
}

// NewBasicSchema creates a new basic schema
func NewBasicSchema() *BasicSchema {
	return &BasicSchema{
		typeInfo: make(map[string]*Type),
	}
}

// ValidatePath validates a path
func (s *BasicSchema) ValidatePath(path string) bool {
	// Basic schema validates all non-empty paths
	return path != ""
}

// GetPathType implements SchemaInfo interface
func (s *BasicSchema) GetPathType(path string) *Type {
	if s.typeInfo == nil {
		return nil
	}
	return s.typeInfo[path]
}

// RegisterPathType implements SchemaInfo interface
func (s *BasicSchema) RegisterPathType(path string, typ *Type) {
	if s.typeInfo == nil {
		s.typeInfo = make(map[string]*Type)
	}
	s.typeInfo[path] = typ
}

// BasicFacts provides an immutable facts implementation
type BasicFacts struct {
	data   map[string]interface{}
	schema SchemaInfo
}

// NewBasicFacts creates a new facts instance from data and schema
func NewBasicFacts(data map[string]interface{}, schema SchemaInfo) *BasicFacts {
	if schema == nil {
		schema = NewBasicSchema()
	}

	// Make a deep copy of the data to ensure immutability
	copiedData := deepCopyMap(data)

	return &BasicFacts{
		data:   copiedData,
		schema: schema,
	}
}

// Get implements Facts interface
func (f *BasicFacts) Get(path string) (interface{}, bool) {
	result, info := f.GetWithContext(path)
	return result, info != nil && info.Exists
}

// GetWithContext implements Facts interface
func (f *BasicFacts) GetWithContext(path string) (interface{}, *ResolutionResult) {
	if f.data == nil {
		return nil, &ResolutionResult{
			Exists: false,
			Error:  errors.New("no data"),
			Path:   Path{},
		}
	}

	if path == "" {
		return nil, &ResolutionResult{
			Exists: false,
			Error:  errors.New("empty path"),
			Path:   Path{},
		}
	}

	parsedPath, err := ParseString(path)
	if err != nil {
		return nil, &ResolutionResult{
			Exists: false,
			Error:  fmt.Errorf("invalid path: %w", err),
			Path:   Path{},
		}
	}

	// Resolve the path
	return f.resolvePath(parsedPath)
}

// Schema implements Facts interface
func (f *BasicFacts) Schema() SchemaInfo {
	return f.schema
}

// HasPath implements Facts interface
func (f *BasicFacts) HasPath(path string) bool {
	_, exists := f.Get(path)
	return exists
}

// WithData creates a new Facts instance with updated data
func (f *BasicFacts) WithData(updatedData map[string]interface{}) *BasicFacts {
	return NewBasicFacts(updatedData, f.schema)
}

// resolvePath resolves a parsed path against the facts data
func (f *BasicFacts) resolvePath(path Path) (interface{}, *ResolutionResult) {
	// Extract the namespace
	var current interface{} = f.data

	// Handle namespace access
	if m, ok := current.(map[string]interface{}); ok {
		if nsData, ok := m[path.Namespace]; ok {
			current = nsData
		} else {
			return nil, &ResolutionResult{
				Path:   path,
				Exists: false,
				Error:  fmt.Errorf("namespace not found: %s", path.Namespace),
			}
		}
	}

	// Navigate through elements
	for i, elem := range path.Elements {
		switch currentMap := current.(type) {
		case map[string]interface{}:
			// Try to get the value by key
			if value, ok := currentMap[elem.Name]; ok {
				if elem.HasIndex() {
					// Handle array access
					if arr, ok := value.([]interface{}); ok {
						idx, _ := elem.GetIndex()
						if idx >= 0 && idx < len(arr) {
							current = arr[idx]
						} else {
							return nil, &ResolutionResult{
								Path:   path,
								Exists: false,
								Error:  fmt.Errorf("index out of bounds: %d", idx),
							}
						}
					} else {
						return nil, &ResolutionResult{
							Path:   path,
							Exists: false,
							Error:  fmt.Errorf("not an array: %s", elem.Name),
						}
					}
				} else if elem.HasStringKey() {
					// Handle map key access
					if mapVal, ok := value.(map[string]interface{}); ok {
						key, _ := elem.GetStringKey()
						if val, ok := mapVal[key]; ok {
							current = val
						} else {
							return nil, &ResolutionResult{
								Path:   path,
								Exists: false,
								Error:  fmt.Errorf("key not found: %s", key),
							}
						}
					} else {
						return nil, &ResolutionResult{
							Path:   path,
							Exists: false,
							Error:  fmt.Errorf("not a map: %s", elem.Name),
						}
					}
				} else {
					// Regular field access
					current = value
				}
			} else {
				return nil, &ResolutionResult{
					Path:   path,
					Exists: false,
					Error:  fmt.Errorf("field not found: %s", elem.Name),
				}
			}

		case []interface{}:
			// Array elements need to have an index
			if !elem.HasIndex() {
				return nil, &ResolutionResult{
					Path:   path,
					Exists: false,
					Error:  fmt.Errorf("array access without index: %s", elem.Name),
				}
			}

			idx, _ := elem.GetIndex()
			if idx >= 0 && idx < len(currentMap) {
				current = currentMap[idx]
			} else {
				return nil, &ResolutionResult{
					Path:   path,
					Exists: false,
					Error:  fmt.Errorf("index out of bounds: %d", idx),
				}
			}

		default:
			// If we're at a leaf, return the current value
			if i == len(path.Elements)-1 {
				return current, &ResolutionResult{
					Path:   path,
					Value:  current,
					Exists: true,
				}
			}

			// Otherwise, we can't navigate further
			return nil, &ResolutionResult{
				Path:   path,
				Exists: false,
				Error:  fmt.Errorf("cannot navigate past leaf value at: %s", elem.Name),
			}
		}
	}

	// Get the type for this path
	pathType := f.schema.GetPathType(path.String())

	// If we get here, we've successfully navigated the path
	return current, &ResolutionResult{
		Path:   path,
		Value:  current,
		Exists: true,
		Type:   pathType,
	}
}

// Helper function for deep-copying maps to ensure immutability
func deepCopyMap(original map[string]interface{}) map[string]interface{} {
	if original == nil {
		return nil
	}

	copied := make(map[string]interface{}, len(original))
	for key, value := range original {
		copied[key] = deepCopyValue(value)
	}

	return copied
}

// Helper function for deep-copying values of any type
func deepCopyValue(value interface{}) interface{} {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case map[string]interface{}:
		return deepCopyMap(v)
	case []interface{}:
		copiedSlice := make([]interface{}, len(v))
		for i, item := range v {
			copiedSlice[i] = deepCopyValue(item)
		}
		return copiedSlice
	default:
		// For primitive types, they're already passed by value
		return v
	}
}
