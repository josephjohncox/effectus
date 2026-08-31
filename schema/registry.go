package schema

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/expr-lang/expr"
	exprast "github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
	"github.com/expr-lang/expr/vm"
	"github.com/josephjohncox/effectus"
	"github.com/josephjohncox/effectus/schema/types"
)

var (
	defaultClockMu sync.RWMutex
	defaultClock   func() time.Time = time.Now
)

// SetDefaultClock overrides the default clock used by new registries.
func SetDefaultClock(clock func() time.Time) {
	defaultClockMu.Lock()
	defer defaultClockMu.Unlock()
	defaultClock = clock
}

// SetFixedTime pins registry time functions to a fixed timestamp.
func SetFixedTime(now time.Time) {
	SetDefaultClock(func() time.Time { return now })
}

// ResetDefaultClock restores the default clock to time.Now.
func ResetDefaultClock() {
	SetDefaultClock(time.Now)
}

func getDefaultClock() func() time.Time {
	defaultClockMu.RLock()
	clock := defaultClock
	defaultClockMu.RUnlock()
	if clock == nil {
		return time.Now
	}
	return clock
}

// Registry provides expression evaluation with extensible data and functions
type Registry struct {
	mu        sync.RWMutex
	data      map[string]interface{}
	functions map[string]interface{}
	programs  map[string]*vm.Program // Compiled expressions cache

	predicatePrograms map[string]*vm.Program
	clock             func() time.Time
}

// NewRegistry creates a new empty registry
func NewRegistry() *Registry {
	clock := getDefaultClock()
	registry := &Registry{
		data:              make(map[string]interface{}),
		functions:         make(map[string]interface{}),
		programs:          make(map[string]*vm.Program),
		predicatePrograms: make(map[string]*vm.Program),
		clock:             clock,
	}

	for _, spec := range types.StandardLibrary() {
		if spec != nil {
			registry.functions[spec.Name] = spec.Func
		}
	}

	// Register default temporal functions
	registry.registerTemporalFunctions()

	return registry
}

// registerTemporalFunctions registers basic time-based functions
func (r *Registry) registerTemporalFunctions() {
	r.functions["now"] = func() time.Time {
		if r.clock == nil {
			return time.Now()
		}
		return r.clock()
	}
	r.functions["nowUTC"] = func() time.Time {
		if r.clock == nil {
			return time.Now().UTC()
		}
		return r.clock().UTC()
	}
}

// SetClock overrides the time source for temporal functions like now().
func (r *Registry) SetClock(clock func() time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clock = clock
}

// SetNow sets a fixed timestamp for temporal functions.
func (r *Registry) SetNow(now time.Time) {
	r.SetClock(func() time.Time { return now })
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
	if err := r.CompileExpression(expression); err != nil {
		return nil, err
	}
	return r.EvaluateCompiled(expression)
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

	env := r.buildEnvironmentLocked()
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
	env := r.buildEnvironmentLocked()
	r.mu.RUnlock()

	return expr.Run(program, env)
}

// TypeCheckExpression validates an expression without evaluating it
func (r *Registry) TypeCheckExpression(expression string) error {
	env := r.environmentSnapshot()

	_, err := expr.Compile(expression, expr.Env(env), expr.AllowUndefinedVariables())
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

// LoadFromFacts loads facts from effectus.Facts into the registry
func (r *Registry) LoadFromFacts(facts effectus.Facts) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Try to get all data
	if allData, exists := facts.Get(""); exists {
		if dataMap, ok := allData.(map[string]interface{}); ok {
			for k, v := range dataMap {
				r.loadValue(k, v)
			}
		}
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
		rv := reflect.ValueOf(value)
		if !rv.IsValid() {
			r.data[prefix] = nil
			return
		}

		switch rv.Kind() {
		case reflect.Map:
			if rv.Type().Key().Kind() != reflect.String {
				r.data[prefix] = v
				return
			}

			converted := make(map[string]interface{}, rv.Len())
			iter := rv.MapRange()
			for iter.Next() {
				key := iter.Key().String()
				converted[key] = iter.Value().Interface()
			}

			r.data[prefix] = converted
			for key, subValue := range converted {
				newPrefix := prefix
				if newPrefix != "" {
					newPrefix += "."
				}
				newPrefix += key
				r.loadValue(newPrefix, subValue)
			}
		case reflect.Slice, reflect.Array:
			length := rv.Len()
			items := make([]interface{}, length)
			for i := 0; i < length; i++ {
				item := rv.Index(i).Interface()
				items[i] = item
				newPrefix := fmt.Sprintf("%s[%d]", prefix, i)
				r.loadValue(newPrefix, item)
			}
			r.data[prefix] = items
		default:
			// Store primitive values
			r.data[prefix] = v
		}
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

// environmentSnapshot copies the expression environment while holding the
// registry read lock. Callers can evaluate against the returned top-level map
// without racing with Set or RegisterFunction.
func (r *Registry) environmentSnapshot() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.buildEnvironmentLocked()
}

// buildEnvironmentLocked creates an environment while the caller holds r.mu.
func (r *Registry) buildEnvironmentLocked() map[string]interface{} {
	env := make(map[string]interface{}, len(r.data)+len(r.functions))

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
	r.predicatePrograms = make(map[string]*vm.Program)
}

// ClearAll removes everything including functions
func (r *Registry) ClearAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.data = make(map[string]interface{})
	r.functions = make(map[string]interface{})
	r.programs = make(map[string]*vm.Program)
	r.predicatePrograms = make(map[string]*vm.Program)
}

// === Predicate Functionality (simplified to use expr directly) ===

// Predicate represents a compiled predicate expression
type Predicate struct {
	Expression string
	registry   *Registry
	program    *vm.Program
	constant   *bool
}

// NewPredicate creates a new predicate using the registry
func (r *Registry) NewPredicate(expression string) (*Predicate, error) {
	// Validate the expression using expr's parser
	if err := r.TypeCheckExpression(expression); err != nil {
		return nil, fmt.Errorf("invalid predicate expression: %w", err)
	}

	if constant, ok := constantBool(expression); ok {
		return &Predicate{
			Expression: expression,
			registry:   r,
			constant:   &constant,
		}, nil
	}

	r.mu.RLock()
	if program, exists := r.predicatePrograms[expression]; exists {
		r.mu.RUnlock()
		return &Predicate{
			Expression: expression,
			registry:   r,
			program:    program,
		}, nil
	}
	env := r.buildEnvironmentLocked()
	r.mu.RUnlock()
	program, err := expr.Compile(expression, expr.Env(env), expr.AllowUndefinedVariables())
	if err != nil {
		return nil, fmt.Errorf("compiling predicate expression: %w", err)
	}

	r.mu.Lock()
	r.predicatePrograms[expression] = program
	r.mu.Unlock()

	return &Predicate{
		Expression: expression,
		registry:   r,
		program:    program,
	}, nil
}

// Evaluate evaluates the predicate against the registry that compiled it.
func (p *Predicate) Evaluate() (bool, error) {
	return p.EvaluateWithRegistry(p.registry)
}

// EvaluateWithRegistry evaluates a compiled predicate without mutating it.
func (p *Predicate) EvaluateWithRegistry(registry *Registry) (bool, error) {
	if registry == nil {
		return false, fmt.Errorf("predicate registry is nil")
	}

	if p.constant != nil {
		return *p.constant, nil
	}

	if p.program != nil {
		result, err := expr.Run(p.program, registry.environmentSnapshot())
		if err != nil {
			return false, err
		}
		if b, ok := result.(bool); ok {
			return b, nil
		}
		return false, fmt.Errorf("expression did not return boolean, got %T", result)
	}

	return registry.EvaluateBoolean(p.Expression)
}

// EvaluatePredicates evaluates multiple predicates against a request-local registry.
func (r *Registry) EvaluatePredicates(predicates []*Predicate, facts effectus.Facts) bool {
	result, _ := evaluatePredicates(predicates, facts, r)
	return result
}

// EvaluatePredicatesWithFacts evaluates predicates without mutating compiled state.
func EvaluatePredicatesWithFacts(predicates []*Predicate, facts effectus.Facts) bool {
	result, _ := EvaluatePredicatesWithFactsE(predicates, facts)
	return result
}

// EvaluatePredicatesWithFactsE distinguishes a false predicate from an evaluation error.
func EvaluatePredicatesWithFactsE(predicates []*Predicate, facts effectus.Facts) (bool, error) {
	var base *Registry
	if len(predicates) > 0 {
		base = predicates[0].registry
	}
	return evaluatePredicates(predicates, facts, base)
}

func evaluatePredicates(predicates []*Predicate, facts effectus.Facts, base *Registry) (bool, error) {
	if len(predicates) == 0 {
		return true, nil
	}

	registry := NewRegistry()
	if base != nil {
		base.mu.RLock()
		for name, function := range base.functions {
			registry.functions[name] = function
		}
		base.mu.RUnlock()
	}
	registry.LoadFromFacts(facts)

	for _, predicate := range predicates {
		result, err := predicate.EvaluateWithRegistry(registry)
		if err != nil {
			return false, err
		}
		if !result {
			return false, nil
		}
	}
	return true, nil
}

// CompileLogicalExpression compiles a logical expression and extracts fact paths
// Note: expr handles path resolution automatically, so we don't need custom parsing
func (r *Registry) CompileLogicalExpression(expression string, schemaInfo effectus.SchemaInfo) ([]*Predicate, map[string]struct{}, error) {
	// Create predicate
	predicate, err := r.NewPredicate(expression)
	if err != nil {
		return nil, nil, err
	}

	pathsMap := ExtractFactPaths(expression)

	return []*Predicate{predicate}, pathsMap, nil
}

func constantBool(expression string) (bool, bool) {
	tree, err := parser.Parse(expression)
	if err != nil {
		return false, false
	}

	visitor := &variableVisitor{}
	node := tree.Node
	exprast.Walk(&node, visitor)
	if visitor.hasVariables {
		return false, false
	}

	result, err := expr.Eval(expression, map[string]interface{}{})
	if err != nil {
		return false, false
	}
	value, ok := result.(bool)
	return value, ok
}

type variableVisitor struct {
	hasVariables bool
}

func (v *variableVisitor) Visit(node *exprast.Node) {
	switch (*node).(type) {
	case *exprast.IdentifierNode, *exprast.MemberNode, *exprast.PointerNode, *exprast.VariableDeclaratorNode:
		v.hasVariables = true
	}
}
