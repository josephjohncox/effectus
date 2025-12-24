package types

import (
	"fmt"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/ast"
	exprPkg "github.com/expr-lang/expr"
)

// This file provides utilities for working with the expr-based predicate system
// which uses raw expression strings and leverages expr's built-in path resolution.

// typeCheckLogicalExpression checks a string expression against facts
func (ts *TypeSystem) typeCheckLogicalExpression(expr string, facts effectus.Facts) error {
	if expr == "" {
		return nil
	}

	// Create environment with fact data for expr validation
	env := make(map[string]interface{})

	// Load facts into environment for expr compilation
	// expr will handle path resolution automatically
	if allData, exists := facts.Get(""); exists {
		if dataMap, ok := allData.(map[string]interface{}); ok {
			for k, v := range dataMap {
				env[k] = v
			}
		}
	}

	// Validate the expression syntax - expr will handle path resolution
	_, err := exprPkg.Compile(expr, exprPkg.Env(env))
	if err != nil {
		return fmt.Errorf("invalid expression syntax: %w", err)
	}

	return nil
}

// TypeCheckLogicalExpressionAST checks a string expression for type safety
func (ts *TypeSystem) TypeCheckLogicalExpressionAST(expr string) error {
	if expr == "" {
		return nil
	}

	// Create environment with type-compatible dummy values for validation
	env := ts.buildTypeEnvironment()

	// Validate expression syntax - expr handles paths automatically
	_, err := exprPkg.Compile(expr, exprPkg.Env(env))
	if err != nil {
		return fmt.Errorf("invalid expression syntax: %w", err)
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

// buildTypeEnvironment creates an environment for expr type checking
func (ts *TypeSystem) buildTypeEnvironment() map[string]interface{} {
	env := make(map[string]interface{})

	// Load all known fact types into the environment with dummy values
	// This allows expr to validate path access during compilation
	for path, factType := range ts.factTypes {
		// Create dummy values of the correct type for validation
		switch factType.PrimType {
		case TypeString:
			env[path] = ""
		case TypeInt:
			env[path] = 0
		case TypeFloat:
			env[path] = 0.0
		case TypeBool:
			env[path] = false
		case TypeTime:
			env[path] = "2023-01-01T00:00:00Z"
		default:
			// For complex types, set a placeholder map
			env[path] = map[string]interface{}{}
		}
	}

	return env
}
