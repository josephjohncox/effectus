package types

import (
	"fmt"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/ast"
	"github.com/effectus/effectus-go/eval"
)

// This file provides utilities for working with the expr-based predicate system
// which uses raw expression strings instead of complex AST structures.

// typeCheckLogicalExpression checks a string expression against facts
func (ts *TypeSystem) typeCheckLogicalExpression(expr string, facts effectus.Facts) error {
	if expr == "" {
		return nil
	}

	// Extract fact paths from the expression
	paths, err := eval.ExtractFactPaths(expr)
	if err != nil {
		return fmt.Errorf("invalid expression syntax: %w", err)
	}

	// Validate each fact path
	for path := range paths {
		_, exists := facts.Get(path)
		if !exists {
			return fmt.Errorf("fact path does not exist: %s", path)
		}

		// Also check type compatibility if possible
		_, err := ts.GetFactType(path)
		if err != nil {
			return fmt.Errorf("unknown fact type: %s", err)
		}
	}

	return nil
}

// TypeCheckLogicalExpressionAST checks a string expression for type safety
func (ts *TypeSystem) TypeCheckLogicalExpressionAST(expr string) error {
	if expr == "" {
		return nil
	}

	// Parse the expression and extract paths for validation
	paths, err := eval.ExtractFactPaths(expr)
	if err != nil {
		return fmt.Errorf("invalid expression syntax: %w", err)
	}

	// Validate each path
	for path := range paths {
		_, err := ts.GetFactType(path)
		if err != nil {
			return fmt.Errorf("unknown fact path in expression: %s", path)
		}
	}

	return nil
}

// TypeCheckPredicateAST checks a string expression for type safety
func (ts *TypeSystem) TypeCheckPredicateAST(expr string) error {
	return ts.TypeCheckLogicalExpressionAST(expr)
}

// typeCheckRulePredicates checks all predicates in a rule
func (ts *TypeSystem) typeCheckRulePredicates(rule *ast.Rule, facts effectus.Facts) error {
	for _, block := range rule.Blocks {
		if block.When != nil && block.When.Expression != "" {
			if err := ts.typeCheckLogicalExpression(block.When.Expression, facts); err != nil {
				return fmt.Errorf("predicate error: %w", err)
			}
		}
	}
	return nil
}

// typeCheckFlowPredicates checks the predicate in a flow
func (ts *TypeSystem) typeCheckFlowPredicates(flow *ast.Flow, facts effectus.Facts) error {
	if flow.When != nil && flow.When.Expression != "" {
		if err := ts.typeCheckLogicalExpression(flow.When.Expression, facts); err != nil {
			return fmt.Errorf("predicate error: %w", err)
		}
	}
	return nil
}
