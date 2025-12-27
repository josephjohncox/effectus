package pathutil

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/effectus/effectus-go/schema"
)

// RegistryFactProvider adapts the schema registry to the pathutil FactProvider interface.
type RegistryFactProvider struct {
	registry *schema.Registry
}

// NewRegistryFactProvider creates a new fact provider using a fresh registry.
func NewRegistryFactProvider() *RegistryFactProvider {
	registry := schema.NewRegistry()
	return &RegistryFactProvider{registry: registry}
}

// NewRegistryFactProviderWithRegistry creates a provider with an existing registry.
func NewRegistryFactProviderWithRegistry(registry *schema.Registry) *RegistryFactProvider {
	return &RegistryFactProvider{registry: registry}
}

// NewRegistryFactProviderFromMap loads a nested map into a new registry-backed provider.
func NewRegistryFactProviderFromMap(data map[string]interface{}) *RegistryFactProvider {
	provider := NewRegistryFactProvider()
	provider.registry.LoadFromMap(data)
	return provider
}

// NewRegistryFactProviderFromJSON loads JSON data into a new registry-backed provider.
func NewRegistryFactProviderFromJSON(jsonData []byte) (*RegistryFactProvider, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return nil, err
	}
	return NewRegistryFactProviderFromMap(data), nil
}

// NewRegistryFactProviderFromStruct converts a struct to a nested map and loads it into a provider.
func NewRegistryFactProviderFromStruct(structData interface{}) (*RegistryFactProvider, error) {
	data, err := structToMap(structData)
	if err != nil {
		return nil, err
	}
	provider := NewRegistryFactProvider()
	provider.registry.LoadFromMap(data)
	return provider, nil
}

// LoadFromMap merges data into the underlying registry.
func (rfp *RegistryFactProvider) LoadFromMap(data map[string]interface{}) {
	rfp.registry.LoadFromMap(data)
}

// Get retrieves a value by path.
func (rfp *RegistryFactProvider) Get(path string) (interface{}, bool) {
	return rfp.registry.Get(path)
}

// Set stores a value at a path.
func (rfp *RegistryFactProvider) Set(path string, value interface{}) {
	rfp.registry.Set(path, value)
}

// GetWithContext retrieves a value with resolution context.
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

// HasPath checks if a path exists.
func (rfp *RegistryFactProvider) HasPath(path string) bool {
	_, exists := rfp.registry.Get(path)
	return exists
}

// EvaluateExpression evaluates an expression against the registry.
func (rfp *RegistryFactProvider) EvaluateExpression(expression string) (interface{}, error) {
	return rfp.registry.EvaluateExpression(expression)
}

// EvaluateExpr is a convenience alias for EvaluateExpression.
func (rfp *RegistryFactProvider) EvaluateExpr(expression string) (interface{}, error) {
	return rfp.registry.EvaluateExpression(expression)
}

// EvaluateExprBool evaluates an expression and expects a boolean result.
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

// GetRegistry returns the underlying registry.
func (rfp *RegistryFactProvider) GetRegistry() *schema.Registry {
	return rfp.registry
}

// NormalizeTypeName converts Go type strings into user-facing type labels.
func NormalizeTypeName(typeName string) string {
	switch typeName {
	case "string":
		return "string"
	case "bool":
		return "boolean"
	case "float32", "float64":
		return "float"
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64":
		return "integer"
	case "[]interface {}":
		return "array"
	case "map[string]interface {}":
		return "object"
	default:
		if strings.HasPrefix(typeName, "[]") {
			return "array"
		}
		if strings.HasPrefix(typeName, "map[") {
			return "object"
		}
		return typeName
	}
}
