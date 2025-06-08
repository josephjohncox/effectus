package eval

import (
	"fmt"
	"time"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/schema"
)

// Predicate represents a compiled predicate expression
type Predicate struct {
	Expression string
	registry   *schema.Registry
}

// NewPredicate creates a new predicate using the registry
func NewPredicate(expression string, registry *schema.Registry) (*Predicate, error) {
	// Validate the expression
	if err := registry.TypeCheckExpression(expression); err != nil {
		return nil, fmt.Errorf("invalid predicate expression: %w", err)
	}

	return &Predicate{
		Expression: expression,
		registry:   registry,
	}, nil
}

// Evaluate evaluates the predicate
func (p *Predicate) Evaluate() (bool, error) {
	return p.registry.EvaluateBoolean(p.Expression)
}

// EvaluatePredicates evaluates multiple predicates (all must be true)
func EvaluatePredicates(predicates []*Predicate, facts effectus.Facts) bool {
	if len(predicates) == 0 {
		return true
	}

	// Create a temporary registry and load facts
	registry := schema.NewRegistry()

	// Load temporal functions
	registerTemporalFunctions(registry)

	// Load facts into registry
	loadFactsIntoRegistry(registry, facts)

	// Evaluate each predicate
	for _, predicate := range predicates {
		// Create a new predicate with the loaded registry
		newPred := &Predicate{
			Expression: predicate.Expression,
			registry:   registry,
		}

		result, err := newPred.Evaluate()
		if err != nil || !result {
			return false
		}
	}

	return true
}

// CompileLogicalExpression compiles a logical expression and extracts fact paths
func CompileLogicalExpression(expression string, schemaInfo effectus.SchemaInfo) ([]*Predicate, map[string]struct{}, error) {
	// Create a registry for compilation
	registry := schema.NewRegistry()
	registerTemporalFunctions(registry)

	// Create predicate
	predicate, err := NewPredicate(expression, registry)
	if err != nil {
		return nil, nil, err
	}

	// Extract referenced paths (simplified)
	paths := extractPathsFromExpression(expression)
	pathsMap := make(map[string]struct{})
	for _, path := range paths {
		pathsMap[path] = struct{}{}
	}

	return []*Predicate{predicate}, pathsMap, nil
}

// registerTemporalFunctions registers basic time-based functions (users can add more)
func registerTemporalFunctions(registry *schema.Registry) {
	// Keep minimal - let users register their own business logic
	registry.RegisterFunction("now", func() time.Time {
		return time.Now()
	})
}

// loadFactsIntoRegistry loads facts from effectus.Facts into the registry
func loadFactsIntoRegistry(registry *schema.Registry, facts effectus.Facts) {
	// Try to get all data
	if allData, exists := facts.Get(""); exists {
		if dataMap, ok := allData.(map[string]interface{}); ok {
			registry.LoadFromMap(dataMap)
		}
	}
}

// extractPathsFromExpression extracts fact paths from an expression (simplified)
func extractPathsFromExpression(expression string) []string {
	var paths []string
	words := tokenizeExpression(expression)

	for _, word := range words {
		if isFactPath(word) {
			paths = append(paths, word)
		}
	}

	return paths
}

// tokenizeExpression provides basic tokenization
func tokenizeExpression(expression string) []string {
	var tokens []string
	current := ""

	for _, char := range expression {
		switch char {
		case ' ', '\t', '\n', '(', ')', '[', ']', '{', '}', ',', ';':
			if current != "" {
				tokens = append(tokens, current)
				current = ""
			}
		case '=', '!', '<', '>', '&', '|', '+', '-', '*', '/', '%':
			if current != "" {
				tokens = append(tokens, current)
				current = ""
			}
		default:
			current += string(char)
		}
	}

	if current != "" {
		tokens = append(tokens, current)
	}

	return tokens
}

// isFactPath determines if a token looks like a fact path
func isFactPath(token string) bool {
	if len(token) == 0 {
		return false
	}

	// Must start with a letter or underscore
	first := token[0]
	if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z') || first == '_') {
		return false
	}

	// Must contain at least one dot (to be a path)
	containsDot := false
	for _, char := range token {
		if char == '.' {
			containsDot = true
			break
		}
	}

	return containsDot
}
