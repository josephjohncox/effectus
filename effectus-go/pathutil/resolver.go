// Package pathutil provides utilities for working with paths
package pathutil

import (
	"fmt"
)

// ResolutionResult contains the result of path resolution
type ResolutionResult struct {
	// Path is the path that was resolved
	Path Path

	// Value is the resolved value
	Value interface{}

	// Exists indicates if the path exists in the data
	Exists bool

	// Error is any error encountered during resolution
	Error error

	// Type is the type information for the resolved value (optional)
	Type interface{}
}

// PathResolver provides path resolution functionality
type PathResolver struct {
	// StrictMode determines if undefined paths cause errors
	StrictMode bool
}

// NewPathResolver creates a new PathResolver
func NewPathResolver(strictMode bool) *PathResolver {
	return &PathResolver{
		StrictMode: strictMode,
	}
}

// Resolve resolves a structured path against a fact provider
func (r *PathResolver) Resolve(provider FactProvider, path Path) (interface{}, bool) {
	return provider.Get(path)
}

// ResolveWithContext resolves a path with detailed resolution information
func (r *PathResolver) ResolveWithContext(provider FactProvider, path Path) (interface{}, *ResolutionResult) {
	value, result := provider.GetWithContext(path)

	// If using strict mode and the path doesn't exist, return an error
	if r.StrictMode && !result.Exists && result.Error == nil {
		result.Error = fmt.Errorf("path not found (strict mode): %s", path.String())
	}

	return value, result
}

// ParseAndResolve is a convenience function that parses a path string and resolves it
func (r *PathResolver) ParseAndResolve(provider FactProvider, pathStr string) (interface{}, bool, error) {
	path, err := ParseString(pathStr)
	if err != nil {
		return nil, false, err
	}

	value, exists := r.Resolve(provider, path)
	return value, exists, nil
}

// ParseAndResolveWithContext is a convenience function for parsing and resolving with context
func (r *PathResolver) ParseAndResolveWithContext(provider FactProvider, pathStr string) (interface{}, *ResolutionResult, error) {
	path, err := ParseString(pathStr)
	if err != nil {
		return nil, nil, err
	}

	value, result := r.ResolveWithContext(provider, path)
	return value, result, nil
}
