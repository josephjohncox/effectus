package pathutil

import (
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/expr-lang/expr"
)

// TypedExprFacts extends ExprFacts with type information
// It provides type metadata for paths and expressions
type TypedExprFacts struct {
	// The base ExprFacts provider
	facts *ExprFacts

	// Type information map (path -> type name)
	typeInfo map[string]string

	// Environment for expr type checking
	environment map[string]reflect.Type

	// Cache for evaluated expression types
	exprTypeCache map[string]reflect.Type

	// Mutex for thread safety
	mu sync.RWMutex
}

// NewTypedExprFacts creates a new TypedExprFacts instance
func NewTypedExprFacts(facts *ExprFacts) *TypedExprFacts {
	// Create an environment from the facts data
	env := createTypeEnv(facts.data)

	return &TypedExprFacts{
		facts:         facts,
		typeInfo:      make(map[string]string),
		environment:   env,
		exprTypeCache: make(map[string]reflect.Type),
	}
}

// Get implements FactProvider.Get
func (t *TypedExprFacts) Get(path string) (interface{}, bool) {
	return t.facts.Get(path)
}

// GetWithContext implements FactProvider.GetWithContext with added type information
func (t *TypedExprFacts) GetWithContext(path string) (interface{}, *ResolutionResult) {
	// Get the value from the underlying provider
	value, result := t.facts.GetWithContext(path)

	// If path doesn't exist, just return
	if !result.Exists {
		return value, result
	}

	return value, result
}

// TypedResolutionResult extends ResolutionResult with type information
type TypedResolutionResult struct {
	*ResolutionResult
	TypeName string
}

// EvaluateExprWithType evaluates an expression and returns the result with its type information
func (t *TypedExprFacts) EvaluateExprWithType(expression string) (interface{}, reflect.Type, error) {
	// First check the cache
	if cachedType, ok := t.exprTypeCache[expression]; ok {
		// Get the value by evaluating
		value, err := t.facts.EvaluateExpr(expression)
		if err != nil {
			return nil, nil, err
		}
		return value, cachedType, nil
	}

	// Compile the expression
	program, err := expr.Compile(expression, expr.Env(t.facts.data))
	if err != nil {
		return nil, nil, err
	}

	// Run the expression
	value, err := expr.Run(program, t.facts.data)
	if err != nil {
		return nil, nil, err
	}

	// Infer the type of the result
	var resultType reflect.Type
	if value != nil {
		resultType = reflect.TypeOf(value)
		// Cache for future use
		t.exprTypeCache[expression] = resultType
	}

	return value, resultType, nil
}

// createTypeEnv creates a type environment from facts data for type checking
func createTypeEnv(data map[string]interface{}) map[string]reflect.Type {
	env := make(map[string]reflect.Type)

	for key, value := range data {
		if value != nil {
			env[key] = reflect.TypeOf(value)
		}
	}

	return env
}

// GetTypeInfo returns the type information for a path
func (t *TypedExprFacts) GetTypeInfo(path string) string {
	if typeName, ok := t.typeInfo[path]; ok {
		return typeName
	}

	// Try to get the value and infer type
	if value, ok := t.facts.Get(path); ok && value != nil {
		typeName := getTypeNameFromValue(value)
		t.typeInfo[path] = typeName
		return typeName
	}

	return "unknown"
}

// getTypeNameFromValue returns a string representation of a value's type
func getTypeNameFromValue(value interface{}) string {
	if value == nil {
		return "nil"
	}

	t := reflect.TypeOf(value)

	switch t.Kind() {
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "float"
	case reflect.String:
		return "string"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Map:
		return "map"
	case reflect.Struct:
		return "object"
	default:
		return t.String()
	}
}

// RegisterNestedTypes recursively discovers and registers types from structured data
func (t *TypedExprFacts) RegisterNestedTypes() {
	// Process each top-level field
	for key, value := range t.facts.data {
		if value == nil {
			continue
		}

		// Register this path's type
		t.typeInfo[key] = getTypeNameFromValue(value)

		// Recursively process nested structures
		t.registerNestedTypesRecursive(key, value)
	}
}

// registerNestedTypesRecursive registers types for nested structures
func (t *TypedExprFacts) registerNestedTypesRecursive(prefix string, value interface{}) {
	if value == nil {
		return
	}

	switch v := value.(type) {
	case map[string]interface{}:
		// Process each key in the map
		for key, val := range v {
			path := prefix + "." + key
			t.typeInfo[path] = getTypeNameFromValue(val)

			// Recurse for nested values
			t.registerNestedTypesRecursive(path, val)
		}
	case []interface{}:
		// For arrays, register types of elements
		for i, val := range v {
			path := fmt.Sprintf("%s[%d]", prefix, i)
			t.typeInfo[path] = getTypeNameFromValue(val)

			// Recurse for nested values
			t.registerNestedTypesRecursive(path, val)
		}
	}
}

// TypeCheck checks if an expression is valid given the types in the environment
func (t *TypedExprFacts) TypeCheck(expression string) error {
	// Try to compile the expression
	_, err := expr.Compile(expression, expr.Env(t.facts.data), expr.AllowUndefinedVariables())
	return err
}

// GetPathType returns the reflect.Type for a path if available
func (t *TypedExprFacts) GetPathType(path string) reflect.Type {
	// First try to get directly from environment
	if pathType, ok := t.environment[path]; ok {
		return pathType
	}

	// Try to evaluate the path as an expression
	value, ok := t.facts.Get(path)
	if !ok || value == nil {
		return nil
	}

	return reflect.TypeOf(value)
}

// CreateTypedFactProvider creates a typed fact provider from structured data
func CreateTypedFactProvider(data interface{}) (*TypedExprFacts, error) {
	// Create facts provider from data
	facts := NewExprFactsFromData(data)

	// Create typed provider
	typedFacts := NewTypedExprFacts(facts)

	// Register types from the data
	typedFacts.RegisterNestedTypes()

	return typedFacts, nil
}

// NewExprFactsFromData creates an ExprFacts instance from any data type
func NewExprFactsFromData(data interface{}) *ExprFacts {
	// Convert data to a flat map
	flatMap := make(map[string]interface{})

	// Handle different types
	switch v := data.(type) {
	case map[string]interface{}:
		// Already a map, use directly
		flatMap = v
	case struct{}:
		// For structs, use reflection to get fields
		val := reflect.ValueOf(v)
		typ := reflect.TypeOf(v)

		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			// Skip unexported fields
			if field.PkgPath != "" {
				continue
			}

			fieldVal := val.Field(i).Interface()
			flatMap[field.Name] = fieldVal
		}
	case *struct{}:
		// For pointers to structs
		ptr := reflect.ValueOf(v)
		val := ptr.Elem()
		typ := val.Type()

		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			// Skip unexported fields
			if field.PkgPath != "" {
				continue
			}

			fieldVal := val.Field(i).Interface()
			flatMap[field.Name] = fieldVal
		}
	default:
		// For other types, create a single root entry
		flatMap["data"] = data
	}

	return NewExprFacts(flatMap)
}

// GetUnderlyingFacts returns the underlying ExprFacts instance
func (t *TypedExprFacts) GetUnderlyingFacts() *ExprFacts {
	return t.facts
}

// MergeTypedFacts merges data from another TypedExprFacts instance
func (t *TypedExprFacts) MergeTypedFacts(other *TypedExprFacts) {
	if other == nil {
		return
	}

	// Merge the underlying facts
	t.facts.MergeFacts(other.facts)

	// Merge type information
	t.mu.Lock()
	defer t.mu.Unlock()

	for path, typeName := range other.typeInfo {
		t.typeInfo[path] = typeName
	}

	// Rebuild environment
	t.environment = createTypeEnv(t.facts.GetData())

	// Clear cache
	t.exprTypeCache = make(map[string]reflect.Type)
}

// Validate checks if a value matches the expected type for a path
func (t *TypedExprFacts) Validate(path string, value interface{}) (bool, error) {
	// Get the expected type
	expectedType := t.GetPathType(path)
	if expectedType == nil {
		// No type information available, can't validate
		return true, nil
	}

	// Check if value is nil
	if value == nil {
		// Nil is only valid for certain types
		return expectedType.Kind() == reflect.Ptr || expectedType.Kind() == reflect.Interface || expectedType.Kind() == reflect.Map || expectedType.Kind() == reflect.Slice, nil
	}

	// Get actual type
	actualType := reflect.TypeOf(value)

	// Special case for numeric types (Go is strict about number types)
	if isNumeric(expectedType) && isNumeric(actualType) {
		return true, nil
	}

	// Check assignability
	if !actualType.AssignableTo(expectedType) {
		return false, fmt.Errorf("type mismatch for path %s: expected %s, got %s", path, expectedType, actualType)
	}

	return true, nil
}

// isNumeric checks if a type is a numeric type
func isNumeric(t reflect.Type) bool {
	if t == nil {
		return false
	}

	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

// GetPathsByPrefix returns all paths that start with the given prefix
func (t *TypedExprFacts) GetPathsByPrefix(prefix string) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var paths []string
	for path := range t.typeInfo {
		if strings.HasPrefix(path, prefix) {
			paths = append(paths, path)
		}
	}

	return paths
}

// InferType infers the type of an expression result
func (t *TypedExprFacts) InferType(expression string) (reflect.Type, error) {
	// Check the cache first
	t.mu.RLock()
	cachedType, hasCached := t.exprTypeCache[expression]
	t.mu.RUnlock()

	if hasCached {
		return cachedType, nil
	}

	// Evaluate the expression
	value, err := t.facts.EvaluateExpr(expression)
	if err != nil {
		return nil, fmt.Errorf("evaluating expression for type inference: %w", err)
	}

	var resultType reflect.Type
	if value != nil {
		resultType = reflect.TypeOf(value)

		// Cache the type
		t.mu.Lock()
		t.exprTypeCache[expression] = resultType
		t.mu.Unlock()
	}

	return resultType, nil
}

// ValidateExpression checks if an expression is valid and returns a specific type
func (t *TypedExprFacts) ValidateExpression(expression string, expectedType reflect.Type) (bool, error) {
	// Infer the expression type
	exprType, err := t.InferType(expression)
	if err != nil {
		return false, err
	}

	// If we couldn't infer a type (e.g., expression returns nil)
	if exprType == nil {
		return expectedType == nil || expectedType.Kind() == reflect.Interface, nil
	}

	// Special case for numeric types
	if isNumeric(exprType) && isNumeric(expectedType) {
		return true, nil
	}

	// Check if types are compatible
	if !exprType.AssignableTo(expectedType) {
		return false, fmt.Errorf("expression type mismatch: expected %s, got %s", expectedType, exprType)
	}

	return true, nil
}

// Update updates the fact data and refreshes type information
func (t *TypedExprFacts) Update(data map[string]interface{}) {
	// Update the underlying facts
	t.facts.UpdateData(data)

	// Rebuild environment and type info
	t.mu.Lock()
	defer t.mu.Unlock()

	t.environment = createTypeEnv(data)
	t.typeInfo = make(map[string]string)
	t.exprTypeCache = make(map[string]reflect.Type)

	// Register types
	t.RegisterNestedTypes()
}
