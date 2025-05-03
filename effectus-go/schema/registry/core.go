// Package registry provides schema management for Effectus
package registry

import (
	"github.com/effectus/effectus-go/schema/types"
)

// TypeRegistry provides extended registry functionality for types
type TypeRegistry interface {
	RegisterFactType(path string, typ *types.Type)
	GetFactType(path string) *types.Type
	GetAllFactPaths() []string
}

// Registry implements the TypeRegistry interface
type Registry struct {
	// Type registry maps paths to types
	types map[string]*types.Type

	// Maintain a list of all registered paths
	paths []string
}

// NewRegistry creates a new type registry
func NewRegistry() *Registry {
	return &Registry{
		types: make(map[string]*types.Type),
		paths: make([]string, 0),
	}
}

// RegisterFactType registers a type for a fact path
func (r *Registry) RegisterFactType(path string, typ *types.Type) {
	if r.types == nil {
		r.types = make(map[string]*types.Type)
	}

	// Add to types map
	r.types[path] = typ

	// Add to paths list if not already present
	alreadyRegistered := false
	for _, p := range r.paths {
		if p == path {
			alreadyRegistered = true
			break
		}
	}

	if !alreadyRegistered {
		r.paths = append(r.paths, path)
	}
}

// GetFactType gets the type for a fact path
func (r *Registry) GetFactType(path string) *types.Type {
	if r.types == nil {
		return nil
	}
	return r.types[path]
}

// GetAllFactPaths gets all registered fact paths
func (r *Registry) GetAllFactPaths() []string {
	return r.paths
}

// GetFactPrimitiveType gets the primitive type of a fact
func (r *Registry) GetFactPrimitiveType(path string) types.PrimitiveType {
	typ := r.GetFactType(path)
	if typ == nil {
		return types.TypeUnknown
	}
	return typ.PrimType
}

// ValidatePath checks if a path is valid according to registered types
// If no types are registered, returns true (permissive mode)
func (r *Registry) ValidatePath(path string) bool {
	if len(r.types) == 0 {
		return true
	}
	_, exists := r.types[path]
	return exists
}

// ValidatePathAsType checks if a path is valid and of the expected type
func (r *Registry) ValidatePathAsType(path string, primType types.PrimitiveType) bool {
	if len(r.types) == 0 {
		return true
	}
	typ, exists := r.types[path]
	if !exists {
		return false
	}
	return typ.PrimType == primType
}

// HasBooleanFact checks if a path is a boolean fact
func (r *Registry) HasBooleanFact(path string) bool {
	return r.ValidatePathAsType(path, types.TypeBool)
}

// HasNumericFact checks if a path is a numeric fact
func (r *Registry) HasNumericFact(path string) bool {
	return r.ValidatePathAsType(path, types.TypeInt) ||
		r.ValidatePathAsType(path, types.TypeFloat)
}

// HasStringFact checks if a path is a string fact
func (r *Registry) HasStringFact(path string) bool {
	return r.ValidatePathAsType(path, types.TypeString)
}
