package pathutil

import (
	"encoding/json"
	"fmt"

	"github.com/effectus/effectus-go/schema"
)

// RegistryFactProvider adapts the Registry to provide pathutil functionality
type RegistryFactProvider struct {
	registry *schema.Registry
}

// NewRegistryFactProvider creates a new fact provider using the registry
func NewRegistryFactProvider() *RegistryFactProvider {
	registry := schema.NewRegistry()

	return &RegistryFactProvider{
		registry: registry,
	}
}

// NewRegistryFactProviderWithRegistry creates a fact provider with an existing registry
func NewRegistryFactProviderWithRegistry(registry *schema.Registry) *RegistryFactProvider {
	return &RegistryFactProvider{
		registry: registry,
	}
}

// Get retrieves a value by path
func (rfp *RegistryFactProvider) Get(path string) (interface{}, bool) {
	return rfp.registry.Get(path)
}

// Set stores a value at a path
func (rfp *RegistryFactProvider) Set(path string, value interface{}) {
	rfp.registry.Set(path, value)
}

// GetWithContext retrieves a value with resolution context
func (rfp *RegistryFactProvider) GetWithContext(path string) (interface{}, *ResolutionResult) {
	value, exists := rfp.registry.Get(path)

	if !exists {
		return nil, &ResolutionResult{
			Path:   path,
			Exists: false,
			Error:  fmt.Errorf("path not found: %s", path),
		}
	}

	return value, &ResolutionResult{
		Path:   path,
		Value:  value,
		Exists: true,
	}
}

// HasPath checks if a path exists
func (rfp *RegistryFactProvider) HasPath(path string) bool {
	_, exists := rfp.registry.Get(path)
	return exists
}

// EvaluateExpression evaluates an expression
func (rfp *RegistryFactProvider) EvaluateExpression(expression string) (interface{}, error) {
	return rfp.registry.EvaluateExpression(expression)
}

// EvaluateExpr is a convenience method for evaluating expressions
func (rfp *RegistryFactProvider) EvaluateExpr(expression string) (interface{}, error) {
	return rfp.registry.EvaluateExpression(expression)
}

// EvaluateExprBool evaluates an expression and returns a boolean result
func (rfp *RegistryFactProvider) EvaluateExprBool(expression string) (bool, error) {
	result, err := rfp.registry.EvaluateExpression(expression)
	if err != nil {
		return false, err
	}

	if boolResult, ok := result.(bool); ok {
		return boolResult, nil
	}

	return false, fmt.Errorf("expression did not evaluate to boolean: %v", result)
}

// GetRegistry returns the underlying registry
func (rfp *RegistryFactProvider) GetRegistry() *schema.Registry {
	return rfp.registry
}

// ExprFacts provides compatibility with the old ExprFacts interface
type ExprFacts struct {
	provider *RegistryFactProvider
	data     map[string]interface{} // For compatibility
}

// NewExprFactsFromData creates ExprFacts from raw data
func NewExprFactsFromData(data map[string]interface{}) *ExprFacts {
	provider := NewRegistryFactProvider()
	provider.registry.LoadFromMap(data)
	return &ExprFacts{
		provider: provider,
		data:     data,
	}
}

// Get retrieves a value by path
func (ef *ExprFacts) Get(path string) (interface{}, bool) {
	if ef.provider != nil {
		return ef.provider.Get(path)
	}
	// Fallback to data map
	value, exists := ef.data[path]
	return value, exists
}

// TypedExprFacts provides typed fact access using the registry
type TypedExprFacts struct {
	provider *RegistryFactProvider
}

// NewTypedExprFacts creates a new typed facts provider
func NewTypedExprFacts(provider *RegistryFactProvider) *TypedExprFacts {
	return &TypedExprFacts{
		provider: provider,
	}
}

// Get retrieves a value by path
func (tef *TypedExprFacts) Get(path string) (interface{}, bool) {
	return tef.provider.Get(path)
}

// GetTypeInfo returns type information for a path
func (tef *TypedExprFacts) GetTypeInfo(path string) string {
	if typeInfo, exists := tef.provider.registry.GetType(path); exists {
		if typeStr, ok := typeInfo.(string); ok {
			return typeStr
		}
	}
	return "unknown"
}

// TypeCheck validates an expression
func (tef *TypedExprFacts) TypeCheck(expression string) error {
	return tef.provider.registry.TypeCheckExpression(expression)
}

// GetUnderlyingFacts returns the underlying fact provider
func (tef *TypedExprFacts) GetUnderlyingFacts() *RegistryFactProvider {
	return tef.provider
}

// GetPathsByPrefix returns paths with a given prefix
func (tef *TypedExprFacts) GetPathsByPrefix(prefix string) []string {
	return tef.provider.registry.GetPathsWithPrefix(prefix)
}

// RegisterNestedTypes is a no-op for compatibility with examples
func (tef *TypedExprFacts) RegisterNestedTypes() {
	// This is now handled automatically by the registry
}

// EvaluateExprWithType evaluates an expression with type checking
func (tef *TypedExprFacts) EvaluateExprWithType(expression string) (interface{}, string, error) {
	// First type check
	if err := tef.TypeCheck(expression); err != nil {
		return nil, "", fmt.Errorf("type check failed: %w", err)
	}

	// Then evaluate
	result, err := tef.provider.EvaluateExpression(expression)
	if err != nil {
		return nil, "", err
	}

	// Determine the type of the result
	resultType := "unknown"
	if result != nil {
		switch result.(type) {
		case bool:
			resultType = "bool"
		case int, int8, int16, int32, int64:
			resultType = "int"
		case uint, uint8, uint16, uint32, uint64:
			resultType = "uint"
		case float32, float64:
			resultType = "float"
		case string:
			resultType = "string"
		case []interface{}:
			resultType = "array"
		case map[string]interface{}:
			resultType = "object"
		default:
			resultType = fmt.Sprintf("%T", result)
		}
	}

	return result, resultType, nil
}

// MergeTypedFacts merges another TypedExprFacts
func (tef *TypedExprFacts) MergeTypedFacts(other *TypedExprFacts) {
	if other != nil && other.provider != nil {
		tef.provider.registry.Merge(other.provider.registry)
	}
}

// === Factory Functions ===

// NewFactProviderFromJSON creates a FactProvider from a JSON string for compatibility
func NewFactProviderFromJSON(jsonData string) *RegistryFactProvider {
	// Parse JSON into a map
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		// If we can't parse JSON, return an empty provider
		return NewExprFacts(make(map[string]interface{}))
	}
	return NewExprFacts(data)
}

// NewFactProviderFromStruct creates a FactProvider from a struct for compatibility
func NewFactProviderFromStruct(structData interface{}) (*RegistryFactProvider, error) {
	// Use the struct loader
	loader := NewStructLoader(structData)
	facts, err := loader.LoadIntoFacts()
	if err != nil {
		return nil, err
	}

	// Return the underlying provider from the ExprFacts
	return facts.provider, nil
}

// NewExprFacts creates a fact provider from a data map
func NewExprFacts(data map[string]interface{}) *RegistryFactProvider {
	provider := NewRegistryFactProvider()
	provider.registry.LoadFromMap(data)
	return provider
}

// LoaderFromRegistry creates a loader from a registry
func LoaderFromRegistry(registry *schema.Registry) FactLoader {
	return &registryLoader{registry: registry}
}

// registryLoader implements FactLoader using the registry
type registryLoader struct {
	registry *schema.Registry
}

// LoadIntoFacts loads data into ExprFacts (using registry provider as adapter)
func (rl *registryLoader) LoadIntoFacts() (*ExprFacts, error) {
	provider := NewRegistryFactProviderWithRegistry(rl.registry)
	// Create an ExprFacts-compatible wrapper
	return &ExprFacts{
		provider: provider,
		data:     make(map[string]interface{}), // Empty fallback
	}, nil
}

// LoadIntoTypedFacts loads data into typed facts
func (rl *registryLoader) LoadIntoTypedFacts() (*TypedExprFacts, error) {
	provider := NewRegistryFactProviderWithRegistry(rl.registry)
	return NewTypedExprFacts(provider), nil
}
