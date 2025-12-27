package types

import (
	"github.com/effectus/effectus-go"
)

// FactProvider represents a simple interface for retrieving facts
type FactProvider interface {
	Get(path string) (interface{}, bool)
}

// ResolutionResult provides details about path resolution
type ResolutionResult struct {
	Exists   bool
	Path     string
	Value    interface{}
	Error    error
	Depth    int
	Resolved []string
}

// AdvancedFactProvider extends FactProvider with context
type AdvancedFactProvider interface {
	FactProvider
	GetWithContext(path string) (interface{}, *ResolutionResult)
}

// TypedProvider is a fact provider that includes type information
type TypedProvider struct {
	// provider is the underlying fact provider
	provider FactProvider

	// typeSystem contains type information
	typeSystem *TypeSystem
}

// NewTypedProvider creates a new typed provider
func NewTypedProvider(provider FactProvider, typeSystem *TypeSystem) *TypedProvider {
	return &TypedProvider{
		provider:   provider,
		typeSystem: typeSystem,
	}
}

// Get retrieves a value using a structured path
func (p *TypedProvider) Get(path string) (interface{}, bool) {
	return p.provider.Get(path)
}

// GetWithContext retrieves a value with detailed resolution information including type
func (p *TypedProvider) GetWithContext(path string) (interface{}, *ResolutionResult) {
	// Get value from underlying provider
	value, exists := p.provider.Get(path)

	result := &ResolutionResult{
		Exists: exists,
		Path:   path,
		Value:  value,
	}

	// If path doesn't exist, return the result
	if !exists {
		return value, result
	}

	return value, result
}

// WithProvider creates a new typed provider with a different underlying provider
func (p *TypedProvider) WithProvider(provider FactProvider) *TypedProvider {
	return NewTypedProvider(provider, p.typeSystem)
}

// GetTypeSystem returns the underlying type system
func (p *TypedProvider) GetTypeSystem() *TypeSystem {
	return p.typeSystem
}

// EnrichWithTypes enriches all values with type information
func (p *TypedProvider) EnrichWithTypes(facts effectus.Facts) *TypedProvider {
	// Get all fact paths
	paths := p.typeSystem.GetAllFactPaths()

	// For each path with a registered type, validate and enrich the data
	for _, path := range paths {
		value, exists := facts.Get(path)
		if exists {
			// Here we could add runtime type validation
			// or transform values based on their expected types
			// For now, we'll just ensure the type is registered
			_, err := p.typeSystem.GetFactType(path)
			if err != nil {
				// If not already registered, infer and register
				inferredType := InferTypeFromInterface(value)
				p.typeSystem.RegisterFactType(path, inferredType)
			}
		}
	}

	return p
}
