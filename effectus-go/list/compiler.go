package list

import (
	"fmt"
	"path/filepath"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/ast"
	"github.com/effectus/effectus-go/common"
)

// Compiler implements the Compiler interface for list-style rules
type Compiler struct{}

// CompileFile compiles a rule file into a list-style spec
func (c *Compiler) CompileFile(path string, schema effectus.SchemaInfo) (effectus.Spec, error) {
	// Ensure the file has the correct extension
	ext := filepath.Ext(path)
	if ext != ".eff" {
		return nil, fmt.Errorf("list compiler can only compile .eff files, got: %s", path)
	}

	// Parse the file
	file, err := effectus.ParseFile(path)
	if err != nil {
		return nil, err
	}

	// Ensure the file contains rules
	if len(file.Rules) == 0 {
		return nil, fmt.Errorf("no rules found in file %s", path)
	}

	// Compile each rule
	spec := &Spec{
		Rules: make([]*CompiledRule, 0, len(file.Rules)),
	}

	factPaths := make(map[string]struct{})

	for _, rule := range file.Rules {
		compiledRule, err := compileRule(rule, schema)
		if err != nil {
			return nil, fmt.Errorf("failed to compile rule %s: %w", rule.Name, err)
		}
		spec.Rules = append(spec.Rules, compiledRule)

		// Collect fact paths
		for _, path := range compiledRule.FactPaths {
			factPaths[path] = struct{}{}
		}
	}

	// Extract unique fact paths
	spec.FactPaths = make([]string, 0, len(factPaths))
	for path := range factPaths {
		spec.FactPaths = append(spec.FactPaths, path)
	}

	return spec, nil
}

// compileRule compiles a single rule into a CompiledRule
func compileRule(rule *ast.Rule, schema effectus.SchemaInfo) (*CompiledRule, error) {
	compiledRule := &CompiledRule{
		Name:     rule.Name,
		Priority: rule.Priority,
	}

	// Compile logical expressions from the rule blocks
	if len(rule.Blocks) > 0 {
		predicates := []*common.Predicate{}
		factPaths := make(map[string]struct{})

		// Go through each block and extract predicates
		for _, block := range rule.Blocks {
			if block.When != nil && block.When.Expression != nil {
				preds, paths, err := compileLogicalExpression(block.When.Expression, schema)
				if err != nil {
					return nil, err
				}

				predicates = append(predicates, preds...)

				// Collect fact paths
				for path := range paths {
					factPaths[path] = struct{}{}
				}
			}

			// Compile effects
			if block.Then != nil && block.Then.Effects != nil {
				for _, effect := range block.Then.Effects {
					compiledArgs, err := common.CompileArgs(effect.Args, nil)
					if err != nil {
						return nil, fmt.Errorf("failed to compile args: %w", err)
					}
					compiledEffect := Effect{
						Verb: effect.Verb,
						Args: compiledArgs,
					}
					compiledRule.Effects = append(compiledRule.Effects, &compiledEffect)
				}
			}
		}

		compiledRule.Predicates = predicates

		// Extract unique fact paths
		compiledRule.FactPaths = make([]string, 0, len(factPaths))
		for path := range factPaths {
			compiledRule.FactPaths = append(compiledRule.FactPaths, path)
		}
	}

	return compiledRule, nil
}

// compileLogicalExpression compiles a logical expression into predicates
func compileLogicalExpression(expr *ast.LogicalExpression, schema effectus.SchemaInfo) ([]*common.Predicate, map[string]struct{}, error) {
	predicates := []*common.Predicate{}
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

			path := pred.PathExpr.GetFullPath()

			// Validate path against schema
			if !schema.ValidatePath(path) {
				return nil, nil, fmt.Errorf("invalid path: %s", path)
			}

			// Save path for later fact requirements
			factPaths[path] = struct{}{}

			// Create compiled predicate
			compiledPred := &common.Predicate{
				Path: path,
				Op:   pred.Op,
				Lit:  common.CompileLiteral(&pred.Lit),
			}
			predicates = append(predicates, compiledPred)
		} else if expr.Left.SubExpr != nil {
			// Recursive call for sub-expression
			subPredicates, subPaths, err := compileLogicalExpression(expr.Left.SubExpr, schema)
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
		rightPredicates, rightPaths, err := compileLogicalExpression(expr.Right, schema)
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
