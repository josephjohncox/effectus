package eval

import (
	"github.com/effectus/effectus-go/ast"
)

// CompileLiteral converts an AST literal to a runtime value
func CompileLiteral(lit *ast.Literal) interface{} {
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
			list = append(list, CompileLiteral(&item))
		}
		return list
	}
	if lit.Map != nil {
		m := make(map[string]interface{}, len(lit.Map))
		for _, entry := range lit.Map {
			m[entry.Key] = CompileLiteral(&entry.Value)
		}
		return m
	}
	return nil
}