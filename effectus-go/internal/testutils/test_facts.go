package testutils

import (
	"github.com/effectus/effectus-go"
)

// TestSchemaInfo is a simple schema implementation for testing
type TestSchemaInfo struct{}

// ValidatePath implements the SchemaInfo interface
func (s *TestSchemaInfo) ValidatePath(path string) bool {
	// Simple implementation that accepts all paths for testing
	return true
}

// TestFacts is a simple in-memory implementation of Facts for testing
type TestFacts struct {
	data   map[string]interface{}
	schema effectus.SchemaInfo
}

// NewTestFacts creates new test facts with the given data
func NewTestFacts(data map[string]interface{}) *TestFacts {
	return &TestFacts{
		data:   data,
		schema: &TestSchemaInfo{},
	}
}

// Get implements the Facts interface
func (f *TestFacts) Get(path string) (interface{}, bool) {
	val, ok := f.data[path]
	return val, ok
}

// Schema implements the Facts interface
func (f *TestFacts) Schema() effectus.SchemaInfo {
	return f.schema
}
