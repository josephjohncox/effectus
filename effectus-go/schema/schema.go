package schema

import (
	"strings"

	"github.com/effectus/effectus-go"
)

// SimpleSchema is a basic implementation of SchemaInfo for development and testing
type SimpleSchema struct {
	// In a real implementation, this would be backed by protobuf descriptors
}

// ValidatePath checks if a path is valid according to a simple dotted notation schema
func (s *SimpleSchema) ValidatePath(path string) bool {
	// In this simple implementation, we just check that the path:
	// 1. Is not empty
	// 2. Uses only valid characters (letters, numbers, dots, underscores)
	// 3. Doesn't have consecutive dots
	// 4. Doesn't start or end with a dot

	if path == "" {
		return false
	}

	if strings.HasPrefix(path, ".") || strings.HasSuffix(path, ".") {
		return false
	}

	if strings.Contains(path, "..") {
		return false
	}

	for _, r := range path {
		if !(r == '.' || r == '_' || ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9')) {
			return false
		}
	}

	return true
}

// SimpleFacts is a basic implementation of Facts for testing
type SimpleFacts struct {
	data   map[string]interface{}
	schema effectus.SchemaInfo
}

// NewSimpleFacts creates a new SimpleFacts instance
func NewSimpleFacts(data map[string]interface{}, schema effectus.SchemaInfo) *SimpleFacts {
	return &SimpleFacts{
		data:   data,
		schema: schema,
	}
}

// Get returns the value at the given path, or false if not found
func (f *SimpleFacts) Get(path string) (interface{}, bool) {
	// Split path by dots
	parts := strings.Split(path, ".")
	if len(parts) < 2 {
		return nil, false
	}

	// Navigate through the nested maps
	var current interface{} = f.data
	for i, part := range parts {
		// For the last part, return the value
		if i == len(parts)-1 {
			if m, ok := current.(map[string]interface{}); ok {
				val, exists := m[part]
				return val, exists
			}
			return nil, false
		}

		// For intermediate parts, navigate deeper
		if m, ok := current.(map[string]interface{}); ok {
			var exists bool
			current, exists = m[part]
			if !exists {
				return nil, false
			}
		} else {
			return nil, false
		}
	}

	return nil, false
}

// Schema returns the schema information
func (f *SimpleFacts) Schema() effectus.SchemaInfo {
	return f.schema
}
