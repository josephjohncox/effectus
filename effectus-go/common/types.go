// Package common contains shared type definitions used across Effectus packages
package common

import (
	"github.com/effectus/effectus-go/pathutil"
	"github.com/effectus/effectus-go/schema/types"
)

// ResolutionResult contains the result of path resolution
type ResolutionResult struct {
	// Path is the path that was resolved
	Path pathutil.Path

	// Value is the resolved value
	Value interface{}

	// Exists indicates if the path exists in the data
	Exists bool

	// Error is any error encountered during resolution
	Error error

	// Type is the type information for the resolved value
	Type *types.Type
}

// SchemaInfo defines the interface for schema information providers
type SchemaInfo interface {
	// ValidatePath validates a path
	ValidatePath(path pathutil.Path) bool

	// GetPathType gets the type for a path
	GetPathType(path pathutil.Path) *types.Type

	// RegisterPathType registers a type for a path
	RegisterPathType(path pathutil.Path, typ *types.Type)
}

// Facts defines the interface for immutable fact providers
type Facts interface {
	// Get gets a value by path
	Get(path pathutil.Path) (interface{}, bool)

	// GetWithContext gets a value with resolution information
	GetWithContext(path pathutil.Path) (interface{}, *ResolutionResult)

	// Schema gets the schema information
	Schema() SchemaInfo

	// HasPath checks if a path exists
	HasPath(path pathutil.Path) bool
}
