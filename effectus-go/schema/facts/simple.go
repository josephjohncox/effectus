package facts

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/effectus/effectus-go/common"
)

// ResolutionResult alias for backward compatibility
type ResolutionResult = common.ResolutionResult

// SimpleSchema provides a simple schema implementation
type SimpleSchema struct {
	// Type mapping for paths
	typeInfo map[string]*common.Type
}

// NewSimpleSchema creates a new simple schema
func NewSimpleSchema() *SimpleSchema {
	return &SimpleSchema{
		typeInfo: make(map[string]*common.Type),
	}
}

// ValidatePath validates a path
func (s *SimpleSchema) ValidatePath(path string) bool {
	// Simple schema validates all non-empty paths
	return path != ""
}

// GetPathType implements SchemaInfo interface
func (s *SimpleSchema) GetPathType(path string) *common.Type {
	if s.typeInfo == nil {
		return nil
	}
	return s.typeInfo[path]
}

// RegisterPathType implements SchemaInfo interface
func (s *SimpleSchema) RegisterPathType(path string, typ *common.Type) {
	if s.typeInfo == nil {
		s.typeInfo = make(map[string]*common.Type)
	}
	s.typeInfo[path] = typ
}

// SimpleFacts provides a simple facts implementation
type SimpleFacts struct {
	data   map[string]interface{}
	schema SchemaInfo
}

// NewSimpleFacts creates a new facts instance from data and schema
func NewSimpleFacts(data map[string]interface{}, schema SchemaInfo) *SimpleFacts {
	if schema == nil {
		schema = NewSimpleSchema()
	}
	return &SimpleFacts{
		data:   data,
		schema: schema,
	}
}

// Get implements Facts interface
func (f *SimpleFacts) Get(path string) (interface{}, bool) {
	result, info := f.GetWithContext(path)
	return result, info != nil && info.Exists
}

// GetWithContext implements Facts interface
func (f *SimpleFacts) GetWithContext(path string) (interface{}, *ResolutionResult) {
	if f.data == nil {
		return nil, &ResolutionResult{
			Exists: false,
			Error:  errors.New("no data"),
			Path:   path,
		}
	}

	if path == "" {
		return nil, &ResolutionResult{
			Exists: false,
			Error:  errors.New("empty path"),
			Path:   path,
		}
	}

	// Split the path into parts
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return nil, &ResolutionResult{
			Exists: false,
			Error:  errors.New("invalid path"),
			Path:   path,
		}
	}

	// Resolve the path
	current := f.data
	var segment string
	var value interface{} = current

	// Navigate the path
	for i, part := range parts {
		// Check if we're at a leaf node
		if i == len(parts)-1 {
			segment = part
			break
		}

		// Otherwise, continue navigation
		if mapVal, ok := current[part]; ok {
			if nextMap, ok := mapVal.(map[string]interface{}); ok {
				current = nextMap
			} else {
				return nil, &ResolutionResult{
					Exists: false,
					Error:  fmt.Errorf("path segment %s is not a map", part),
					Path:   path,
				}
			}
		} else {
			return nil, &ResolutionResult{
				Exists: false,
				Error:  fmt.Errorf("path segment %s does not exist", part),
				Path:   path,
			}
		}
	}

	// Extract the value at the leaf node
	if segment != "" {
		if val, ok := current[segment]; ok {
			value = val
		} else {
			return nil, &ResolutionResult{
				Exists: false,
				Error:  fmt.Errorf("leaf segment %s does not exist", segment),
				Path:   path,
			}
		}
	}

	// Create result with the resolved path
	result := &ResolutionResult{
		Exists: true,
		Value:  value,
		Path:   path,
		Type:   f.schema.GetPathType(path),
	}

	return value, result
}

// Schema implements Facts interface
func (f *SimpleFacts) Schema() SchemaInfo {
	return f.schema
}

// HasPath implements Facts interface
func (f *SimpleFacts) HasPath(path string) bool {
	_, exists := f.Get(path)
	return exists
}

// Set implements MutableFacts interface
func (f *SimpleFacts) Set(path string, value interface{}) error {
	// Validate the path is not empty
	if path == "" {
		return fmt.Errorf("invalid path: %s", path)
	}

	if f.data == nil {
		f.data = make(map[string]interface{})
	}

	// Split the path into parts
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return errors.New("invalid path")
	}

	// Navigate and create intermediate maps as needed
	current := f.data
	for i, part := range parts {
		// If we're at the last segment, set the value
		if i == len(parts)-1 {
			current[part] = value
			return nil
		}

		// Otherwise, ensure the intermediate path exists
		if next, ok := current[part]; ok {
			// If the next segment exists and is a map, continue
			if nextMap, ok := next.(map[string]interface{}); ok {
				current = nextMap
			} else {
				// The path exists but is not a map, replace it
				nextMap = make(map[string]interface{})
				current[part] = nextMap
				current = nextMap
			}
		} else {
			// Create a new map for this path segment
			nextMap := make(map[string]interface{})
			current[part] = nextMap
			current = nextMap
		}
	}

	return nil
}

// Delete implements MutableFacts interface
func (f *SimpleFacts) Delete(path string) error {
	// Validate the path is not empty
	if path == "" {
		return fmt.Errorf("invalid path: %s", path)
	}

	if f.data == nil {
		return errors.New("no data")
	}

	// Split the path into parts
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return errors.New("invalid path")
	}

	// If single segment, delete from the root map
	if len(parts) == 1 {
		delete(f.data, parts[0])
		return nil
	}

	// For multi-segment paths, navigate to parent and delete
	current := f.data
	for i := 0; i < len(parts)-1; i++ {
		part := parts[i]
		if next, ok := current[part]; ok {
			if nextMap, ok := next.(map[string]interface{}); ok {
				current = nextMap
			} else {
				// If path doesn't lead to a map, the path doesn't exist
				return fmt.Errorf("path segment %s is not a map", part)
			}
		} else {
			// If segment doesn't exist, nothing to delete
			return fmt.Errorf("path segment %s does not exist", part)
		}
	}

	// Delete the leaf segment
	delete(current, parts[len(parts)-1])
	return nil
}

// ToJSON serializes the facts to JSON
func (f *SimpleFacts) ToJSON() (string, error) {
	if f.data == nil {
		return "{}", nil
	}

	jsonBytes, err := json.Marshal(f.data)
	if err != nil {
		return "", err
	}

	return string(jsonBytes), nil
}

// FromJSON deserializes facts from JSON
func (f *SimpleFacts) FromJSON(jsonStr string) error {
	if jsonStr == "" {
		f.data = make(map[string]interface{})
		return nil
	}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return err
	}

	f.data = data
	return nil
}
