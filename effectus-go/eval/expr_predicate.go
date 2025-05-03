package eval

import (
	"context"
	"fmt"
	"strings"

	"github.com/effectus/effectus-go"
	effectusast "github.com/effectus/effectus-go/ast" // Alias the effectus ast
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast" // Import ast package from expr
	"github.com/expr-lang/expr/conf"
	"github.com/expr-lang/expr/parser"
	exprvm "github.com/expr-lang/expr/vm" // Alias to avoid collision with vm package
)

// PredicateEvaluator is the interface for evaluating predicates
type PredicateEvaluator interface {
	// Evaluate evaluates a predicate block against a facts provider
	Evaluate(ctx context.Context, predicate *effectusast.PredicateBlock, facts effectus.Facts) (bool, error)
}

// ExprEvaluator handles expr-based predicate evaluation
type ExprEvaluator struct {
	program *exprvm.Program
	env     map[string]interface{}
	vars    []string // Store variables from the expression
}

// NewExprEvaluator creates a new expr-based evaluator
func NewExprEvaluator(expression string) (*ExprEvaluator, error) {
	// Parse the expression to extract variable names
	config := &conf.Config{
		Strict: false,
	}
	parsed, err := parser.ParseWithConfig(expression, config)
	if err != nil {
		return nil, fmt.Errorf("parsing expression: %w", err)
	}

	// Collect variables from the parsed expression
	vars := extractVars(parsed.Node)

	// Compile the expression
	program, err := expr.Compile(expression, expr.AllowUndefinedVariables())
	if err != nil {
		return nil, fmt.Errorf("compiling expression: %w", err)
	}

	return &ExprEvaluator{
		program: program,
		env:     make(map[string]interface{}),
		vars:    vars,
	}, nil
}

// extractVars recursively extracts variable names from the parsed expression tree
func extractVars(node ast.Node) []string {
	if node == nil {
		return nil
	}

	var vars []string

	// Handle different node types
	switch n := node.(type) {
	case *ast.IdentifierNode:
		vars = append(vars, n.Value)
	case *ast.MemberNode:
		if id, ok := n.Node.(*ast.IdentifierNode); ok {
			// Extract property name based on the type
			var propName string
			if strNode, ok := n.Property.(*ast.StringNode); ok {
				propName = strNode.Value
			} else {
				propName = n.Property.String()
			}
			vars = append(vars, id.Value+"."+propName)
		}
		vars = append(vars, extractVars(n.Node)...)
		vars = append(vars, extractVars(n.Property)...)
	case *ast.BinaryNode:
		vars = append(vars, extractVars(n.Left)...)
		vars = append(vars, extractVars(n.Right)...)
	case *ast.UnaryNode:
		vars = append(vars, extractVars(n.Node)...)
	case *ast.CallNode:
		vars = append(vars, extractVars(n.Callee)...)
		for _, arg := range n.Arguments {
			vars = append(vars, extractVars(arg)...)
		}
	case *ast.BuiltinNode:
		for _, arg := range n.Arguments {
			vars = append(vars, extractVars(arg)...)
		}
	case *ast.ArrayNode:
		for _, item := range n.Nodes {
			vars = append(vars, extractVars(item)...)
		}
	case *ast.MapNode:
		for _, pair := range n.Pairs {
			vars = append(vars, extractVars(pair)...)
		}
	case *ast.PairNode:
		vars = append(vars, extractVars(n.Key)...)
		vars = append(vars, extractVars(n.Value)...)
	case *ast.ConditionalNode:
		vars = append(vars, extractVars(n.Cond)...)
		vars = append(vars, extractVars(n.Exp1)...)
		vars = append(vars, extractVars(n.Exp2)...)
	default:
		// Other node types don't contain variables
	}

	return vars
}

// EvaluateWithFacts evaluates the expression using the provided facts
func (e *ExprEvaluator) EvaluateWithFacts(facts effectus.Facts) (bool, error) {
	// Extract values from facts and update the environment
	e.updateEnvFromFacts(facts)

	// Run the program
	result, err := expr.Run(e.program, e.env)
	if err != nil {
		return false, fmt.Errorf("running expression: %w", err)
	}

	// Convert result to boolean
	boolResult, ok := result.(bool)
	if !ok {
		return false, fmt.Errorf("expression did not evaluate to a boolean: %v", result)
	}

	return boolResult, nil
}

// updateEnvFromFacts updates the environment with fact values
func (e *ExprEvaluator) updateEnvFromFacts(facts effectus.Facts) {
	// Get required facts from the program
	for _, variable := range e.vars {
		// Split dotted paths
		parts := strings.Split(variable, ".")
		if len(parts) == 0 {
			continue
		}

		// Handle top-level namespaces
		namespace := parts[0]
		if _, exists := e.env[namespace]; !exists {
			e.env[namespace] = make(map[string]interface{})
		}

		// Get the value from facts
		value, exists := facts.Get(variable)
		if !exists {
			continue
		}

		// Update env structure recursively
		updateNestedMap(e.env, parts, value)
	}
}

// updateNestedMap updates a nested map structure with a value
func updateNestedMap(m map[string]interface{}, path []string, value interface{}) {
	if len(path) == 1 {
		m[path[0]] = value
		return
	}

	// Create nested map if needed
	key := path[0]
	if _, ok := m[key]; !ok {
		m[key] = make(map[string]interface{})
	}

	// Recursively set value in nested map
	if nested, ok := m[key].(map[string]interface{}); ok {
		updateNestedMap(nested, path[1:], value)
	}
}

// ExprPredicateEvaluator implements PredicateEvaluator using expr language
type ExprPredicateEvaluator struct{}

// NewExprPredicateEvaluator creates a new expr-based predicate evaluator
func NewExprPredicateEvaluator() *ExprPredicateEvaluator {
	return &ExprPredicateEvaluator{}
}

// Evaluate evaluates a predicate block against a facts provider using expr
func (e *ExprPredicateEvaluator) Evaluate(ctx context.Context, predicate *effectusast.PredicateBlock, facts effectus.Facts) (bool, error) {
	if predicate == nil {
		return true, nil // Empty predicate evaluates to true
	}

	// Use the processed expression string from GetExpression
	expression := predicate.GetExpression()
	if expression == "" {
		return false, nil // Empty expression evaluates to false
	}

	// Create expr evaluator
	evaluator, err := NewExprEvaluator(expression)
	if err != nil {
		return false, fmt.Errorf("creating expr evaluator: %w", err)
	}

	// Evaluate with facts
	result, err := evaluator.EvaluateWithFacts(facts)
	if err != nil {
		return false, fmt.Errorf("evaluating predicate: %w", err)
	}

	return result, nil
}

// DefaultPredicateEvaluator returns the default predicate evaluator implementation
func DefaultPredicateEvaluator() PredicateEvaluator {
	return NewExprPredicateEvaluator()
}

// Predicate represents a predicate that can be evaluated against facts
type Predicate struct {
	// Use the raw expression string for predicates
	Expression string
}

// ExtractFactPaths extracts paths from an expression using expr parser
func ExtractFactPaths(expression string) (map[string]struct{}, error) {
	paths := make(map[string]struct{})

	if expression == "" {
		return paths, nil
	}

	// Parse the expression to extract variable names
	config := &conf.Config{
		Strict: false,
	}
	parsed, err := parser.ParseWithConfig(expression, config)
	if err != nil {
		return nil, fmt.Errorf("parsing expression: %w", err)
	}

	// Collect variables from the parsed expression
	vars := extractVars(parsed.Node)

	// Add all variables as paths
	for _, v := range vars {
		paths[v] = struct{}{}
	}

	return paths, nil
}

// EvaluatePredicates evaluates a list of predicates against facts
func EvaluatePredicates(predicates []*Predicate, facts effectus.Facts) bool {
	if len(predicates) == 0 {
		return true // Empty predicates evaluates to true
	}

	for _, pred := range predicates {
		if pred == nil || pred.Expression == "" {
			continue // Skip empty predicates
		}

		// Create a new evaluator for this predicate
		evaluator, err := NewExprEvaluator(pred.Expression)
		if err != nil {
			fmt.Printf("Error creating expr evaluator: %v\n", err)
			return false
		}

		// Evaluate with facts
		result, err := evaluator.EvaluateWithFacts(facts)
		if err != nil {
			fmt.Printf("Error evaluating predicate: %v\n", err)
			return false
		}

		if !result {
			return false
		}
	}

	return true
}

// CompileLogicalExpression extracts variables from an expression and validates paths
func CompileLogicalExpression(expr string, schema effectus.SchemaInfo) ([]*Predicate, map[string]struct{}, error) {
	if expr == "" {
		return nil, nil, nil
	}

	// Extract fact paths
	paths, err := ExtractFactPaths(expr)
	if err != nil {
		return nil, nil, fmt.Errorf("extracting fact paths: %w", err)
	}

	// Validate paths if schema is provided
	if schema != nil {
		for path := range paths {
			if !schema.ValidatePath(path) {
				return nil, nil, fmt.Errorf("invalid path: %s", path)
			}
		}
	}

	// Create a single predicate with the expression
	predicate := &Predicate{
		Expression: expr,
	}

	return []*Predicate{predicate}, paths, nil
}
