package list

import (
	"fmt"
	"path/filepath"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/ast"
	"github.com/effectus/effectus-go/common"
	"github.com/effectus/effectus-go/schema"
)

// Compiler implements the Compiler interface for list-style rules
type Compiler struct{}

// CompileFile compiles a rule file into a list-style spec
func (c *Compiler) CompileFile(path string, schemaInfo effectus.SchemaInfo) (effectus.Spec, error) {
	// Ensure the file has the correct extension
	ext := filepath.Ext(path)
	if ext != ".eff" {
		return nil, fmt.Errorf("list compiler can only compile .eff files, got: %s", path)
	}

	// Parse the file
	// For now, return an error indicating that file parsing needs to be implemented
	return nil, fmt.Errorf("list compiler file parsing not yet implemented for %s", path)
}

// compileRule compiles a single rule into a CompiledRule
func compileRule(rule *ast.Rule, schemaInfo effectus.SchemaInfo) (*CompiledRule, error) {
	compiledRule := &CompiledRule{
		Name:     rule.Name,
		Priority: rule.Priority,
	}

	// Compile logical expressions from the rule blocks
	if len(rule.Blocks) > 0 {
		predicates := []*schema.Predicate{}
		factPaths := make(map[string]struct{})

		// Go through each block and extract predicates
		for _, block := range rule.Blocks {
			if block.When != nil && block.When.Expression != "" {
				// Create a registry for compilation
				registry := schema.NewRegistry()
				preds, paths, err := registry.CompileLogicalExpression(block.When.Expression, schemaInfo)
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

// Convert string paths to pathutil.Path objects
// For path validation and Predicate creation, use pathutil.ParseString to create Path objects
