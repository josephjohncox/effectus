package types

import (
	"fmt"
	"regexp"
	"strings"

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

	if err := ts.validateFactPathsInExpression(expr); err != nil {
		return err
	}

	// Create environment with type-compatible values and overlay actual facts.
	env := ts.buildTypeEnvironment()

	if facts != nil {
		if allData, exists := facts.Get(""); exists {
			if dataMap, ok := allData.(map[string]interface{}); ok {
				for k, v := range dataMap {
					setEnvPath(env, k, v)
				}
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

	if err := ts.validateFactPathsInExpression(expr); err != nil {
		return err
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

	// Load all known fact types into the environment with dummy values.
	for path, factType := range ts.factTypes {
		setEnvPath(env, path, dummyValueForType(factType))
	}

	return env
}

func dummyValueForType(factType *Type) interface{} {
	if factType == nil {
		return map[string]interface{}{}
	}

	switch factType.PrimType {
	case TypeString:
		return ""
	case TypeInt:
		return 0
	case TypeFloat:
		return 0.0
	case TypeBool:
		return false
	case TypeTime:
		return "2023-01-01T00:00:00Z"
	case TypeList:
		if factType.ElementType != nil {
			return []interface{}{dummyValueForType(factType.ElementType)}
		}
		return []interface{}{}
	case TypeMap, TypeObject:
		return map[string]interface{}{}
	default:
		return map[string]interface{}{}
	}
}

func setEnvPath(env map[string]interface{}, path string, value interface{}) {
	if env == nil || path == "" {
		return
	}

	segments := strings.Split(path, ".")
	current := env

	for i, segment := range segments {
		name, isArray := parseArraySegment(segment)
		isLast := i == len(segments)-1

		if isArray {
			slice, ok := current[name].([]interface{})
			if !ok || len(slice) == 0 {
				slice = []interface{}{map[string]interface{}{}}
			}

			if isLast {
				slice[0] = value
				current[name] = slice
				return
			}

			elem, ok := slice[0].(map[string]interface{})
			if !ok {
				elem = map[string]interface{}{}
				slice[0] = elem
			}

			current[name] = slice
			current = elem
			continue
		}

		if isLast {
			current[segment] = value
			return
		}

		next, ok := current[segment].(map[string]interface{})
		if !ok {
			next = map[string]interface{}{}
			current[segment] = next
		}
		current = next
	}
}

func parseArraySegment(segment string) (string, bool) {
	if idx := strings.Index(segment, "["); idx != -1 {
		return segment[:idx], true
	}
	return segment, false
}

var (
	factPathPattern  = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*|\[[0-9]+\])+`)
	arrayIndexPattern = regexp.MustCompile(`\[[0-9]+\]`)
)

func (ts *TypeSystem) validateFactPathsInExpression(expr string) error {
	matches := factPathPattern.FindAllString(expr, -1)
	for _, match := range matches {
		normalized := normalizeArrayPath(match)
		if _, err := ts.GetFactType(normalized); err != nil {
			return fmt.Errorf("fact path does not exist: %s", normalized)
		}
	}
	return nil
}

func normalizeArrayPath(path string) string {
	return arrayIndexPattern.ReplaceAllString(path, "[]")
}
