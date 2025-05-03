package eval

import (
	"github.com/effectus/effectus-go/ast"
	"github.com/effectus/effectus-go/common"
	"github.com/effectus/effectus-go/pathutil"
)

// AstPredicateEvaluator evaluates AST predicates against Facts
type AstPredicateEvaluator struct{}

// NewPredicateEvaluator creates a new AstPredicateEvaluator
func NewPredicateEvaluator() *AstPredicateEvaluator {
	return &AstPredicateEvaluator{}
}

// Evaluate checks if a predicate is true for the given facts
func (e *AstPredicateEvaluator) Evaluate(pred *ast.Predicate, facts common.Facts, resolver *pathutil.PathResolver) bool {
	// Check if PathExpr is nil
	if pred.PathExpr == nil {
		return false
	}

	// Now we can access the pathutil.Path directly
	// Use the pathutil.Path object directly with facts
	value, exists := facts.Get(pred.PathExpr.Path)
	if !exists {
		return false
	}

	// Convert the AST literal to a comparable value
	literalValue := convertAstLiteral(&pred.Lit)

	// Use the existing CompareFact function from predicate.go
	return CompareFact(value, pred.Op, literalValue)
}

// convertAstLiteral converts an AST literal to a runtime value
func convertAstLiteral(lit *ast.Literal) interface{} {
	if lit.String != nil {
		return *lit.String
	}
	if lit.Int != nil {
		return *lit.Int
	}
	if lit.Float != nil {
		return *lit.Float
	}
	if lit.Bool != nil {
		return *lit.Bool
	}
	if lit.List != nil {
		list := make([]interface{}, len(lit.List))
		for i, item := range lit.List {
			// Create a copy of the item to pass by pointer
			itemCopy := item
			list[i] = convertAstLiteral(&itemCopy)
		}
		return list
	}
	if lit.Map != nil {
		m := make(map[string]interface{})
		for _, entry := range lit.Map {
			m[entry.Key] = convertAstLiteral(&entry.Value)
		}
		return m
	}
	return nil
}
