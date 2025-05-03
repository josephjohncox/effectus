package common

import (
	"fmt"
	"strings"

	"github.com/effectus/effectus-go/ast"
)

// CompileArgs resolves any variable references and fact paths in arguments
func CompileArgs(args []*ast.StepArg, bindings map[string]interface{}) (map[string]interface{}, error) {
	compiledArgs := make(map[string]interface{})
	for _, arg := range args {
		var value interface{}

		if arg.Value != nil {
			if arg.Value.VarRef != "" && bindings != nil {
				// It's a variable reference like $varName
				varName := strings.TrimPrefix(arg.Value.VarRef, "$")
				if boundValue, exists := bindings[varName]; exists {
					value = boundValue
				} else {
					return nil, fmt.Errorf("undefined variable reference: %s", arg.Value.VarRef)
				}
			} else if arg.Value.PathExpr != nil {
				// It's a fact path like customer.email
				// This gets resolved at execution time, so we just pass it as a string
				value = arg.Value.PathExpr.GetFullPath()
			} else if arg.Value.Literal != nil {
				// It's a literal value
				value = CompileLiteral(arg.Value.Literal)
			}
		}

		compiledArgs[arg.Name] = value
	}

	return compiledArgs, nil
}
