// Package facts provides implementations of fact sources for Effectus
package facts

import (
	"github.com/effectus/effectus-go/schema/path"
)

// SchemaInfo defines the interface for schema information providers
type SchemaInfo interface {
	// ValidatePath checks if a path is valid according to the schema
	ValidatePath(path string) bool

	// GetPathType returns the type information for a path
	GetPathType(path string) interface{}
}

// Facts defines the interface for fact providers
type Facts interface {
	// Get retrieves a fact value by path
	Get(path string) (interface{}, bool)

	// GetWithContext retrieves a fact with detailed resolution information
	GetWithContext(path string) (interface{}, *path.ResolutionResult)

	// Schema returns the schema information for these facts
	Schema() SchemaInfo

	// HasPath checks if a path exists in the facts
	HasPath(path string) bool
}

// MutableFacts extends Facts with mutation capabilities
type MutableFacts interface {
	Facts

	// Set sets a fact value at the specified path
	Set(path string, value interface{}) error

	// Delete removes a fact at the specified path
	Delete(path string) error
}

// FactSnapshot represents a point-in-time snapshot of facts
type FactSnapshot struct {
	// Data contains the raw fact data
	Data interface{}

	// Schema is the schema information for the facts
	Schema SchemaInfo
}

// NewFactSnapshot creates a new fact snapshot
func NewFactSnapshot(data interface{}, schema SchemaInfo) *FactSnapshot {
	return &FactSnapshot{
		Data:   data,
		Schema: schema,
	}
}

// QueryOptions defines options for fact queries
type QueryOptions struct {
	// IncludeMissing determines whether missing paths should be included in results
	IncludeMissing bool

	// DefaultValue is the value to use for missing paths if IncludeMissing is true
	DefaultValue interface{}
}

// DefaultQueryOptions returns the default query options
func DefaultQueryOptions() *QueryOptions {
	return &QueryOptions{
		IncludeMissing: false,
		DefaultValue:   nil,
	}
}

// WithMissingValues returns query options that include missing values
func WithMissingValues(defaultValue interface{}) *QueryOptions {
	return &QueryOptions{
		IncludeMissing: true,
		DefaultValue:   defaultValue,
	}
}
