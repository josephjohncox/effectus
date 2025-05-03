package ast

import (
	"github.com/effectus/effectus-go/pathutil"
)

// ResolvePathExpressions resolves all path expressions in a parsed file
func ResolvePathExpressions(file *File) error {
	// Process rules
	for _, rule := range file.Rules {
		for _, block := range rule.Blocks {
			if block.When != nil && block.When.Expression != nil {
				if err := resolveLogicalExpression(block.When.Expression); err != nil {
					return err
				}
			}
		}
	}

	// Process flows
	for _, flow := range file.Flows {
		if flow.When != nil && flow.When.Expression != nil {
			if err := resolveLogicalExpression(flow.When.Expression); err != nil {
				return err
			}
		}

		if flow.Steps != nil {
			for _, step := range flow.Steps.Steps {
				for _, arg := range step.Args {
					if arg.Value != nil && arg.Value.PathExpr != nil {
						if err := resolvePathExpression(arg.Value.PathExpr); err != nil {
							return err
						}
					}
				}
			}
		}
	}

	return nil
}

// resolveLogicalExpression resolves paths in a logical expression
func resolveLogicalExpression(expr *LogicalExpression) error {
	if expr == nil {
		return nil
	}

	// Resolve left side
	if expr.Left != nil {
		if expr.Left.Predicate != nil && expr.Left.Predicate.PathExpr != nil {
			if err := resolvePathExpression(expr.Left.Predicate.PathExpr); err != nil {
				return err
			}
		} else if expr.Left.SubExpr != nil {
			if err := resolveLogicalExpression(expr.Left.SubExpr); err != nil {
				return err
			}
		}
	}

	// Resolve right side if it exists
	if expr.Right != nil {
		if err := resolveLogicalExpression(expr.Right); err != nil {
			return err
		}
	}

	return nil
}

// resolvePathExpression resolves a single path expression
func resolvePathExpression(pathExpr *PathExpression) error {
	if pathExpr == nil || pathExpr.Raw == "" {
		return nil
	}

	// Use the pathutil package to parse the path
	parsedPath, err := pathutil.ParseString(pathExpr.Raw)
	if err != nil {
		return err
	}

	// Set the Path field directly with the parsed path
	pathExpr.Path = parsedPath

	return nil
}
