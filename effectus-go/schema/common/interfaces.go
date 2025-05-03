package common

import "fmt"

// ResolutionResult contains information about path resolution
type ResolutionResult struct {
	// Whether the path exists
	Exists bool

	// Value at the path (if exists)
	Value interface{}

	// Error encountered during resolution (if any)
	Error error

	// The path that was resolved
	Path string

	// Type information about the value
	ValueType *Type
}

// Facts defines the core interface for accessing facts
type Facts interface {
	// Get retrieves a fact value by path
	// Returns the value and true if found, or nil and false if not found
	Get(path string) (interface{}, bool)

	// GetWithContext retrieves a fact with detailed resolution information
	// Returns the resolved value and result details including type information
	GetWithContext(path string) (interface{}, *ResolutionResult)

	// HasPath checks if a path exists
	HasPath(path string) bool

	// Schema returns schema information for these facts
	Schema() SchemaInfo
}

// SchemaInfo defines the interface for schema information
type SchemaInfo interface {
	// GetPathType returns the type information for a path
	GetPathType(path string) *Type

	// RegisterPathType registers type information for a path
	RegisterPathType(path string, typ *Type)
}

// TypeSystem defines the interface for the type system
type TypeSystem interface {
	// RegisterFactType registers a fact path with its type
	RegisterFactType(path string, typ *Type) error

	// GetFactType retrieves the type for a fact path
	GetFactType(path string) (*Type, error)

	// RegisterVerbType registers a verb with its input and output types
	RegisterVerbType(verbName string, inputType, outputType *Type) error

	// TypeCheckPredicate evaluates if a predicate path has valid typing
	TypeCheckPredicate(path string) error

	// TypeCheckEffect evaluates if an effect is type-compatible
	TypeCheckEffect(verbName string, payload interface{}) error
}

// TypeCheckResult represents the result of a type check operation
type TypeCheckResult struct {
	// Whether the check passed
	Valid bool

	// Error message if validation failed
	Error error
}

// NewTypeError creates a new type error with formatted message
func NewTypeError(format string, args ...interface{}) error {
	return fmt.Errorf("type error: "+format, args...)
}
