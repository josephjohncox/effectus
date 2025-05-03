package eval

import (
	"fmt"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/ast"
	"github.com/effectus/effectus-go/common"
)

// CompileLogicalExpression compiles a logical expression into predicates
// This is exported for use by other packages
func CompileLogicalExpression(expr *ast.LogicalExpression, schema effectus.SchemaInfo) ([]*Predicate, map[string]struct{}, error) {
	predicates := []*Predicate{}
	factPaths := make(map[string]struct{})

	if expr == nil {
		return predicates, factPaths, nil
	}

	// Process left side
	if expr.Left != nil {
		if expr.Left.Predicate != nil {
			pred := expr.Left.Predicate
			if pred.PathExpr == nil {
				return nil, nil, fmt.Errorf("predicate has no path expression")
			}

			path := pred.PathExpr.Path

			// Validate path against schema
			if !schema.ValidatePath(path) {
				return nil, nil, fmt.Errorf("invalid path: %s", path)
			}

			// Save path for later fact requirements
			factPaths[path.String()] = struct{}{}

			// Create compiled predicate
			compiledPred := &Predicate{
				Path: path,
				Op:   pred.Op,
				Lit:  common.CompileLiteral(&pred.Lit),
			}
			predicates = append(predicates, compiledPred)
		} else if expr.Left.SubExpr != nil {
			// Recursive call for sub-expression
			subPredicates, subPaths, err := CompileLogicalExpression(expr.Left.SubExpr, schema)
			if err != nil {
				return nil, nil, err
			}

			predicates = append(predicates, subPredicates...)

			// Merge fact paths
			for path := range subPaths {
				factPaths[path] = struct{}{}
			}
		}
	}

	// Process right side if it exists
	if expr.Right != nil {
		rightPredicates, rightPaths, err := CompileLogicalExpression(expr.Right, schema)
		if err != nil {
			return nil, nil, err
		}

		predicates = append(predicates, rightPredicates...)

		// Merge fact paths
		for path := range rightPaths {
			factPaths[path] = struct{}{}
		}
	}

	return predicates, factPaths, nil
}
