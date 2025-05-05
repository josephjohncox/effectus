package pathutil

import (
	"fmt"
	"sync"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

// ExprFacts implements FactProvider using Expr for evaluation
type ExprFacts struct {
	// Complete fact data as a flat map
	data map[string]interface{}

	// Expression cache to avoid recompiling
	exprCache map[string]*vm.Program

	// Mutex for thread safety
	mu sync.RWMutex
}

// NewExprFacts creates a new ExprFacts instance
func NewExprFacts(data map[string]interface{}) *ExprFacts {
	return &ExprFacts{
		data:      data,
		exprCache: make(map[string]*vm.Program),
	}
}

// Get implements FactProvider.Get to retrieve a value by path
func (f *ExprFacts) Get(path string) (interface{}, bool) {
	if path == "" {
		f.mu.RLock()
		defer f.mu.RUnlock()
		return f.data, true
	}

	// For direct access to top-level keys
	f.mu.RLock()
	if value, exists := f.data[path]; exists {
		f.mu.RUnlock()
		return value, true
	}
	f.mu.RUnlock()

	// Use Expr to evaluate the path
	result, err := f.evaluatePath(path)
	if err != nil {
		return nil, false
	}

	return result, result != nil
}

// evaluatePath evaluates a path using Expr
func (f *ExprFacts) evaluatePath(path string) (interface{}, error) {
	// Get or compile expression
	program, err := f.getCompiledExpr(path)
	if err != nil {
		return nil, err
	}

	// Run the program with our data
	f.mu.RLock()
	result, err := expr.Run(program, f.data)
	f.mu.RUnlock()

	if err != nil {
		return nil, err
	}

	return result, nil
}

// getCompiledExpr gets a cached compiled expression or compiles a new one
func (f *ExprFacts) getCompiledExpr(path string) (*vm.Program, error) {
	// Check cache first
	f.mu.RLock()
	program, exists := f.exprCache[path]
	f.mu.RUnlock()

	if exists {
		return program, nil
	}

	// Compile the expression
	program, err := expr.Compile(path, expr.Env(f.data))
	if err != nil {
		return nil, fmt.Errorf("compiling path expression: %w", err)
	}

	// Cache the compiled expression
	f.mu.Lock()
	f.exprCache[path] = program
	f.mu.Unlock()

	return program, nil
}

// GetWithContext implements FactProvider.GetWithContext
func (f *ExprFacts) GetWithContext(path string) (interface{}, *ResolutionResult) {
	value, exists := f.Get(path)

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
func (f *ExprFacts) HasPath(path string) bool {
	_, exists := f.Get(path)
	return exists
}

// UpdateData updates the fact data
func (f *ExprFacts) UpdateData(data map[string]interface{}) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Update the data
	f.data = data

	// Clear the expression cache as the data structure might have changed
	f.exprCache = make(map[string]*vm.Program)
}

// EvaluateExpr evaluates a full expression using the facts data
func (f *ExprFacts) EvaluateExpr(expression string) (interface{}, error) {
	// Compile the expression
	program, err := expr.Compile(expression, expr.Env(f.data))
	if err != nil {
		return nil, fmt.Errorf("compiling expression: %w", err)
	}

	// Run the expression
	f.mu.RLock()
	result, err := expr.Run(program, f.data)
	f.mu.RUnlock()

	if err != nil {
		return nil, fmt.Errorf("running expression: %w", err)
	}

	return result, nil
}

// EvaluateExprBool evaluates an expression and ensures it returns a boolean
func (f *ExprFacts) EvaluateExprBool(expression string) (bool, error) {
	result, err := f.EvaluateExpr(expression)
	if err != nil {
		return false, err
	}

	// Convert result to boolean
	boolResult, ok := result.(bool)
	if !ok {
		return false, fmt.Errorf("expression did not evaluate to a boolean: %v", result)
	}

	return boolResult, nil
}

// ExprFactsBuilder helps construct ExprFacts from multiple sources
type ExprFactsBuilder struct {
	data map[string]interface{}
}

// NewExprFactsBuilder creates a new builder
func NewExprFactsBuilder() *ExprFactsBuilder {
	return &ExprFactsBuilder{
		data: make(map[string]interface{}),
	}
}

// AddFact adds a single fact
func (b *ExprFactsBuilder) AddFact(path string, value interface{}) *ExprFactsBuilder {
	b.data[path] = value
	return b
}

// AddData adds multiple facts
func (b *ExprFactsBuilder) AddData(data map[string]interface{}) *ExprFactsBuilder {
	for k, v := range data {
		b.data[k] = v
	}
	return b
}

// AddStruct adds a struct as a namespace
func (b *ExprFactsBuilder) AddStruct(namespace string, structData interface{}) *ExprFactsBuilder {
	b.data[namespace] = structData
	return b
}

// Build creates the ExprFacts instance
func (b *ExprFactsBuilder) Build() *ExprFacts {
	return NewExprFacts(b.data)
}

// StructFactRegistry is a registry of fact providers
type StructFactRegistry struct {
	// Map of namespace to structured data
	data map[string]interface{}

	// Facts provider built from the data
	facts *ExprFacts

	// Mutex for thread safety
	mu sync.RWMutex
}

// NewStructFactRegistry creates a new registry
func NewStructFactRegistry() *StructFactRegistry {
	data := make(map[string]interface{})
	return &StructFactRegistry{
		data:  data,
		facts: NewExprFacts(data),
	}
}

// Register adds a namespace with structured data
func (r *StructFactRegistry) Register(namespace string, data interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.data[namespace] = data

	// Rebuild facts provider with updated data
	r.facts.UpdateData(r.data)
}

// Get passes through to the facts provider
func (r *StructFactRegistry) Get(path string) (interface{}, bool) {
	return r.facts.Get(path)
}

// GetWithContext passes through to the facts provider
func (r *StructFactRegistry) GetWithContext(path string) (interface{}, *ResolutionResult) {
	return r.facts.GetWithContext(path)
}

// GetFacts returns the facts provider
func (r *StructFactRegistry) GetFacts() *ExprFacts {
	return r.facts
}

// CreateStructFromJSON creates a struct from JSON data
// This is just a placeholder - you would implement actual JSON unmarshaling
func CreateStructFromJSON(jsonData []byte) (interface{}, error) {
	// In a real implementation, you would use json.Unmarshal to create
	// either a struct or a map[string]interface{}
	return nil, fmt.Errorf("not implemented")
}

// CreateStructFromProto creates a struct from a protobuf message
// This is just a placeholder - you would implement actual proto conversion
func CreateStructFromProto(protoMsg interface{}) (interface{}, error) {
	// In a real implementation, you would convert the proto message to a Go struct
	return nil, fmt.Errorf("not implemented")
}
