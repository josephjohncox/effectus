package facts

import (
	"encoding/json"
	"fmt"
	"sync"
)

// SimpleSchema is a basic schema implementation
type SimpleSchema struct {
	pathTypes map[string]interface{}
	mu        sync.RWMutex
}

// NewSimpleSchema creates a new simple schema
func NewSimpleSchema() *SimpleSchema {
	return &SimpleSchema{
		pathTypes: make(map[string]interface{}),
	}
}

// ValidatePath checks if a path is valid according to the schema
func (s *SimpleSchema) ValidatePath(path string) bool {
	// Simple implementation just checks if the path syntax is valid
	return true
}

// GetPathType returns the type information for a path
func (s *SimpleSchema) GetPathType(path string) interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.pathTypes[path]
}

// RegisterPathType registers a type for a path
func (s *SimpleSchema) RegisterPathType(path string, typeInfo interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pathTypes[path] = typeInfo
}

// SimpleFacts is a basic implementation of Facts using a map
type SimpleFacts struct {
	data   map[string]interface{}
	schema SchemaInfo
	mu     sync.RWMutex
}

// NewSimpleFacts creates a new SimpleFacts instance
func NewSimpleFacts(data map[string]interface{}, schema SchemaInfo) *SimpleFacts {
	if data == nil {
		data = make(map[string]interface{})
	}

	if schema == nil {
		schema = NewSimpleSchema()
	}

	return &SimpleFacts{
		data:   data,
		schema: schema,
	}
}

// Get retrieves a fact value by path
func (f *SimpleFacts) Get(path string) (interface{}, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// For this simple implementation, path is just a direct map key
	value, exists := f.data[path]
	return value, exists
}

// GetWithContext retrieves a fact with detailed resolution information
func (f *SimpleFacts) GetWithContext(path string) (interface{}, *ResolutionResult) {
	value, exists := f.Get(path)

	result := &ResolutionResult{
		Found: exists,
		Value: value,
		Path:  path,
		Type:  f.schema.GetPathType(path),
	}

	return value, result
}

// Schema returns the schema information
func (f *SimpleFacts) Schema() SchemaInfo {
	return f.schema
}

// HasPath checks if a path exists
func (f *SimpleFacts) HasPath(path string) bool {
	_, exists := f.Get(path)
	return exists
}

// Set sets a fact value
func (f *SimpleFacts) Set(path string, value interface{}) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.data[path] = value
	return nil
}

// Delete removes a fact
func (f *SimpleFacts) Delete(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	delete(f.data, path)
	return nil
}

// ToJSON converts the facts to a JSON string
func (f *SimpleFacts) ToJSON() (string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	bytes, err := json.MarshalIndent(f.data, "", "  ")
	if err != nil {
		return "", fmt.Errorf("error marshaling facts to JSON: %w", err)
	}

	return string(bytes), nil
}

// FromJSON populates facts from a JSON string
func (f *SimpleFacts) FromJSON(jsonStr string) error {
	var data map[string]interface{}

	err := json.Unmarshal([]byte(jsonStr), &data)
	if err != nil {
		return fmt.Errorf("error unmarshaling JSON to facts: %w", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.data = data
	return nil
}

// ResolutionResult contains information about fact resolution
type ResolutionResult struct {
	Found bool
	Value interface{}
	Path  string
	Type  interface{}
}
