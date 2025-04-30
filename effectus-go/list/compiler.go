package list

import (
	"fmt"
	"path/filepath"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/ast"
	"github.com/effectus/effectus-go/eval"
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

	// Compile predicates
	if rule.When != nil && rule.When.Predicates != nil {
		predicates := make([]*eval.Predicate, 0, len(rule.When.Predicates))
		factPaths := make(map[string]struct{})

		for _, pred := range rule.When.Predicates {
			// Get the path from the PathExpression
			if pred.PathExpr == nil {
				return nil, fmt.Errorf("predicate has no path expression")
			}

			path := pred.PathExpr.GetFullPath()

			// Validate path against schema
			if !schema.ValidatePath(path) {
				return nil, fmt.Errorf("invalid path: %s", path)
			}

			// Save path for later fact requirements
			factPaths[path] = struct{}{}

			// Create compiled predicate
			compiledPred := &eval.Predicate{
				Path: path,
				Op:   pred.Op,
				Lit:  compileLiteral(&pred.Lit),
			}
			predicates = append(predicates, compiledPred)
		}

		compiledRule.Predicates = predicates

		// Extract unique fact paths
		compiledRule.FactPaths = make([]string, 0, len(factPaths))
		for path := range factPaths {
			compiledRule.FactPaths = append(compiledRule.FactPaths, path)
		}
	}

	// Compile effects
	if rule.Then != nil && rule.Then.Effects != nil {
		effects := make([]effectus.Effect, 0, len(rule.Then.Effects))

		for _, effect := range rule.Then.Effects {
			compiledArgs, err := eval.CompileArgs(effect.Args, nil)
			if err != nil {
				return nil, fmt.Errorf("failed to compile args: %w", err)
			}
			compiledEffect := effectus.Effect{
				Verb:    effect.Verb,
				Payload: compiledArgs,
			}
			effects = append(effects, compiledEffect)
		}

		compiledRule.Effects = effects
	}

	return compiledRule, nil
}

// compileLiteral converts an AST literal to a runtime value
func compileLiteral(lit *ast.Literal) interface{} {
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
		list := make([]interface{}, 0, len(lit.List))
		for _, item := range lit.List {
			list = append(list, compileLiteral(&item))
		}
		return list
	}
	if lit.Map != nil {
		m := make(map[string]interface{}, len(lit.Map))
		for _, entry := range lit.Map {
			m[entry.Key] = compileLiteral(&entry.Value)
		}
		return m
	}
	return nil
}
