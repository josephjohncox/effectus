package common

import (
	"fmt"
	"strings"

	"github.com/effectus/effectus-go/ast"
)

// FactRef marks an argument value as a fact path reference.
type FactRef string

// ResultRef marks an argument value as a checked flow result-slot reference.
type ResultRef string

// CompileArgs resolves any variable references and fact paths in arguments
func CompileArgs(args []*ast.StepArg, bindings map[string]interface{}) (map[string]interface{}, error) {
	compiledArgs := make(map[string]interface{}, len(args))
	for index, arg := range args {
		if arg == nil {
			return nil, fmt.Errorf("argument %d is nil", index+1)
		}
		if strings.TrimSpace(arg.Name) == "" {
			return nil, fmt.Errorf("argument %d has an empty name", index+1)
		}
		if _, duplicate := compiledArgs[arg.Name]; duplicate {
			return nil, fmt.Errorf("duplicate argument: %s", arg.Name)
		}
		if arg.Value == nil {
			return nil, fmt.Errorf("argument %s has no value", arg.Name)
		}

		variantCount := 0
		if arg.Value.VarRef != "" {
			variantCount++
		}
		if arg.Value.PathExpr != nil {
			variantCount++
		}
		if arg.Value.Literal != nil {
			variantCount++
		}
		if variantCount != 1 {
			return nil, fmt.Errorf("argument %s must have exactly one value kind", arg.Name)
		}

		var value interface{}
		switch {
		case arg.Value.VarRef != "":
			varName := strings.TrimPrefix(arg.Value.VarRef, "$")
			if varName == "" || varName == arg.Value.VarRef {
				return nil, fmt.Errorf("invalid variable reference: %s", arg.Value.VarRef)
			}
			boundValue, exists := bindings[varName]
			if !exists {
				return nil, fmt.Errorf("undefined variable reference: %s", arg.Value.VarRef)
			}
			value = boundValue
		case arg.Value.PathExpr != nil:
			path := strings.TrimSpace(arg.Value.PathExpr.GetFullPath())
			if path == "" {
				return nil, fmt.Errorf("argument %s has an empty fact path", arg.Name)
			}
			value = FactRef(path)
		case arg.Value.Literal != nil:
			if err := validateLiteral(arg.Value.Literal); err != nil {
				return nil, fmt.Errorf("argument %s: %w", arg.Name, err)
			}
			value = CompileLiteral(arg.Value.Literal)
		}

		compiledArgs[arg.Name] = value
	}

	return compiledArgs, nil
}

func validateLiteral(literal *ast.Literal) error {
	if literal == nil {
		return fmt.Errorf("literal is nil")
	}
	variants := 0
	if literal.String != nil {
		variants++
	}
	if literal.Int != nil {
		variants++
	}
	if literal.Float != nil {
		variants++
	}
	if literal.Bool != nil {
		variants++
	}
	if literal.List != nil {
		variants++
	}
	if literal.Map != nil {
		variants++
	}
	if variants != 1 {
		return fmt.Errorf("literal must have exactly one value kind")
	}
	for index := range literal.List {
		if err := validateLiteral(&literal.List[index]); err != nil {
			return fmt.Errorf("list item %d: %w", index, err)
		}
	}
	seen := make(map[string]struct{}, len(literal.Map))
	for index, entry := range literal.Map {
		if entry == nil {
			return fmt.Errorf("map entry %d is nil", index)
		}
		if strings.TrimSpace(entry.Key) == "" {
			return fmt.Errorf("map entry %d has an empty key", index)
		}
		if _, duplicate := seen[entry.Key]; duplicate {
			return fmt.Errorf("duplicate map key: %s", entry.Key)
		}
		seen[entry.Key] = struct{}{}
		if err := validateLiteral(&entry.Value); err != nil {
			return fmt.Errorf("map entry %s: %w", entry.Key, err)
		}
	}
	return nil
}
