// Package pathutil provides simplified path-based data access using gjson
package pathutil

// FactProvider is an interface for accessing data through paths
type FactProvider interface {
	// Get retrieves a value using a path
	Get(path string) (interface{}, bool)

	// GetWithContext retrieves a value with detailed resolution information
	GetWithContext(path string) (interface{}, *ResolutionResult)
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
func (r *Registry) Get(path string) (interface{}, bool) {
	// Extract namespace from the path
	namespace := getNamespace(path)

	if provider, ok := r.providers[namespace]; ok {
		// Extract the rest of the path without the namespace
		restPath := extractPathWithoutNamespace(path, namespace)
		return provider.Get(restPath)
	}
	return nil, false
}

// GetWithContext retrieves a value with context by routing to the appropriate provider
func (r *Registry) GetWithContext(path string) (interface{}, *ResolutionResult) {
	// Extract namespace from the path
	namespace := getNamespace(path)

	if provider, ok := r.providers[namespace]; ok {
		// Extract the rest of the path without the namespace
		restPath := extractPathWithoutNamespace(path, namespace)
		return provider.GetWithContext(restPath)
	}
	return nil, &ResolutionResult{
		Path:   path,
		Exists: false,
		Error:  nil,
	}
}

// getNamespace extracts the namespace from a path string (everything before the first dot)
func getNamespace(path string) string {
	for i, c := range path {
		if c == '.' || c == '[' {
			return path[:i]
		}
	}
	return path
}

// extractPathWithoutNamespace removes the namespace from the path and returns the rest
func extractPathWithoutNamespace(path, namespace string) string {
	// If path is the same as namespace, there's nothing after it
	if path == namespace {
		return ""
	}

	// Check what follows the namespace
	restPath := path[len(namespace):]
	if restPath[0] == '.' {
		// Skip the dot
		return restPath[1:]
	}

	// If it's not a dot (e.g., it's a bracket), keep it
	return restPath
}
