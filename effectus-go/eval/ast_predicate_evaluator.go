package eval

import (
	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/ast"
	"github.com/effectus/effectus-go/schema/path"
)

// AstPredicateEvaluator evaluates AST predicates against Facts
type AstPredicateEvaluator struct{}

// NewPredicateEvaluator creates a new AstPredicateEvaluator
func NewPredicateEvaluator() *AstPredicateEvaluator {
	return &AstPredicateEvaluator{}
}

// Evaluate checks if a predicate is true for the given facts
func (e *AstPredicateEvaluator) Evaluate(pred *ast.Predicate, facts effectus.Facts, resolver path.FactPathResolver) bool {
	// Get the path from the PathExpression
	if pred.PathExpr == nil {
		return false
	}

	pathStr := pred.PathExpr.GetFullPath()

	// Parse the fact path
	path, err := path.ParseString(pathStr)
	if err != nil {
		return false
	}

	// Resolve the value
	value, exists := resolver.Resolve(facts, path)
	if !exists {
		return false
	}

	// Convert the AST literal to a comparable value
	literalValue := convertAstLiteral(&pred.Lit)

	// Compare using the operator
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
