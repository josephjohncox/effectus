package schema

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sync"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

// Registry provides expression evaluation with extensible data and functions
type Registry struct {
	mu        sync.RWMutex
	data      map[string]interface{}
	functions map[string]interface{}
	programs  map[string]*vm.Program // Compiled expressions cache
}

// NewRegistry creates a new empty registry
func NewRegistry() *Registry {
	return &Registry{
		data:      make(map[string]interface{}),
		functions: make(map[string]interface{}),
		programs:  make(map[string]*vm.Program),
	}
}

// Set stores a value at the given path
func (r *Registry) Set(path string, value interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[path] = value
}

// Get retrieves a value by path
func (r *Registry) Get(path string) (interface{}, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, exists := r.data[path]
	return value, exists
}

// RegisterFunction registers a function for use in expressions
func (r *Registry) RegisterFunction(name string, fn interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.functions[name] = fn
}

// EvaluateExpression evaluates an expression and returns the result
func (r *Registry) EvaluateExpression(expression string) (interface{}, error) {
	r.mu.RLock()
	env := r.buildEnvironment()
	r.mu.RUnlock()

	return expr.Eval(expression, env)
}

// EvaluateBoolean evaluates an expression expecting a boolean result
func (r *Registry) EvaluateBoolean(expression string) (bool, error) {
	result, err := r.EvaluateExpression(expression)
	if err != nil {
		return false, err
	}

	if b, ok := result.(bool); ok {
		return b, nil
	}

	return false, fmt.Errorf("expression did not return boolean, got %T", result)
}

// CompileExpression compiles an expression for faster repeated evaluation
func (r *Registry) CompileExpression(expression string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	env := r.buildEnvironment()
	program, err := expr.Compile(expression, expr.Env(env))
	if err != nil {
		return fmt.Errorf("compiling expression: %w", err)
	}

	r.programs[expression] = program
	return nil
}

// EvaluateCompiled evaluates a pre-compiled expression
func (r *Registry) EvaluateCompiled(expression string) (interface{}, error) {
	r.mu.RLock()
	program, exists := r.programs[expression]
	if !exists {
		r.mu.RUnlock()
		// Compile if not found
		if err := r.CompileExpression(expression); err != nil {
			return nil, err
		}
		r.mu.RLock()
		program = r.programs[expression]
	}
	env := r.buildEnvironment()
	r.mu.RUnlock()

	return expr.Run(program, env)
}

// TypeCheckExpression validates an expression without evaluating it
func (r *Registry) TypeCheckExpression(expression string) error {
	r.mu.RLock()
	env := r.buildEnvironment()
	r.mu.RUnlock()

	_, err := expr.Compile(expression, expr.Env(env))
	return err
}

// GetPathsWithPrefix returns all data paths that start with the given prefix
func (r *Registry) GetPathsWithPrefix(prefix string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var paths []string
	for path := range r.data {
		if len(path) >= len(prefix) && path[:len(prefix)] == prefix {
			paths = append(paths, path)
		}
	}
	return paths
}

// GetType returns type information for a path (basic reflection)
func (r *Registry) GetType(path string) (interface{}, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if value, exists := r.data[path]; exists {
		return reflect.TypeOf(value).String(), true
	}
	return nil, false
}

// Merge combines another registry's data and functions into this one
func (r *Registry) Merge(other *Registry) {
	if other == nil {
		return
	}

	r.mu.Lock()
	other.mu.RLock()

	// Merge data
	for k, v := range other.data {
		r.data[k] = v
	}

	// Merge functions
	for k, v := range other.functions {
		r.functions[k] = v
	}

	other.mu.RUnlock()
	r.mu.Unlock()
}

// LoadFromMap loads data from a map, flattening nested structures
func (r *Registry) LoadFromMap(data map[string]interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for k, v := range data {
		r.loadValue(k, v)
	}
}

// loadValue recursively loads nested data with dot notation paths
func (r *Registry) loadValue(prefix string, value interface{}) {
	if value == nil {
		r.data[prefix] = nil
		return
	}

	switch v := value.(type) {
	case map[string]interface{}:
		// Store the map itself
		r.data[prefix] = v
		// Also store flattened paths
		for key, subValue := range v {
			newPrefix := prefix
			if newPrefix != "" {
				newPrefix += "."
			}
			newPrefix += key
			r.loadValue(newPrefix, subValue)
		}
	case []interface{}:
		// Store the array itself
		r.data[prefix] = v
		// Also store indexed paths
		for i, item := range v {
			newPrefix := fmt.Sprintf("%s[%d]", prefix, i)
			r.loadValue(newPrefix, item)
		}
	default:
		// Store primitive values
		r.data[prefix] = v
	}
}

// LoadFromJSON loads data from JSON bytes
func (r *Registry) LoadFromJSON(jsonData []byte) error {
	var data map[string]interface{}
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return fmt.Errorf("parsing JSON: %w", err)
	}

	r.LoadFromMap(data)
	return nil
}

// buildEnvironment creates the environment for expr evaluation
func (r *Registry) buildEnvironment() map[string]interface{} {
	env := make(map[string]interface{})

	// Add all data
	for k, v := range r.data {
		env[k] = v
	}

	// Add all functions
	for k, v := range r.functions {
		env[k] = v
	}

	return env
}

// Clear removes all data and compiled programs (keeps functions)
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.data = make(map[string]interface{})
	r.programs = make(map[string]*vm.Program)
}

// ClearAll removes everything including functions
func (r *Registry) ClearAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.data = make(map[string]interface{})
	r.functions = make(map[string]interface{})
	r.programs = make(map[string]*vm.Program)
}
