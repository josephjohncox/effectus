package types

import (
	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/pathutil"
)

// TypedProvider is a fact provider that includes type information
type TypedProvider struct {
	// provider is the underlying fact provider
	provider pathutil.FactProvider

	// typeSystem contains type information
	typeSystem *TypeSystem
}

// NewTypedProvider creates a new typed provider
func NewTypedProvider(provider pathutil.FactProvider, typeSystem *TypeSystem) *TypedProvider {
	return &TypedProvider{
		provider:   provider,
		typeSystem: typeSystem,
	}
}

// Get retrieves a value using a structured path
func (p *TypedProvider) Get(path pathutil.Path) (interface{}, bool) {
	return p.provider.Get(path)
}

// GetWithContext retrieves a value with detailed resolution information including type
func (p *TypedProvider) GetWithContext(path pathutil.Path) (interface{}, *pathutil.ResolutionResult) {
	// Get value from underlying provider
	value, result := p.provider.GetWithContext(path)

	// If path doesn't exist, just return the result
	if !result.Exists || result.Error != nil {
		return value, result
	}

	// Try to get type information
	typ, err := p.typeSystem.GetFactType(path)
	if err == nil && typ != nil {
		// Add type information to result
		result.Type = typ
	}

	return value, result
}

// WithProvider creates a new typed provider with a different underlying provider
func (p *TypedProvider) WithProvider(provider pathutil.FactProvider) *TypedProvider {
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
