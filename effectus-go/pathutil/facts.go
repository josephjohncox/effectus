// Package pathutil provides utilities for working with paths
package pathutil

// FactProvider is an interface for accessing data through structured paths
type FactProvider interface {
	// Get retrieves a value using a structured path
	Get(path Path) (interface{}, bool)

	// GetWithContext retrieves a value with detailed resolution information
	GetWithContext(path Path) (interface{}, *ResolutionResult)
}

// Registry maintains a mapping from namespaces to fact providers
type Registry struct {
	providers map[string]FactProvider
}

// NewRegistry creates a new fact provider registry
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]FactProvider),
	}
}

// Register adds a provider for a specific namespace
func (r *Registry) Register(namespace string, provider FactProvider) {
	r.providers[namespace] = provider
}

// Get retrieves a value by routing the request to the appropriate provider
func (r *Registry) Get(path Path) (interface{}, bool) {
	if provider, ok := r.providers[path.Namespace]; ok {
		return provider.Get(path)
	}
	return nil, false
}

// GetWithContext retrieves a value with context by routing to the appropriate provider
func (r *Registry) GetWithContext(path Path) (interface{}, *ResolutionResult) {
	if provider, ok := r.providers[path.Namespace]; ok {
		return provider.GetWithContext(path)
	}
	return nil, &ResolutionResult{
		Path:   path,
		Exists: false,
		Error:  nil,
	}
}
