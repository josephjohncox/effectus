package tests

import (
	"reflect"
	"testing"

	"github.com/effectus/effectus-go/ast"
	"github.com/effectus/effectus-go/common"
)

func TestCompileArgs(t *testing.T) {
	// Helper functions to create literals
	stringPtr := func(s string) *string { return &s }
	intPtr := func(i int) *int { return &i }
	float64Ptr := func(f float64) *float64 { return &f }
	boolPtr := func(b bool) *bool { return &b }

	tests := []struct {
		name     string
		args     []*ast.StepArg
		bindings map[string]interface{}
		expected map[string]interface{}
		wantErr  bool
	}{
		{
			name:     "Empty args",
			args:     []*ast.StepArg{},
			expected: map[string]interface{}{},
			wantErr:  false,
		},
		{
			name: "Literal values",
			args: []*ast.StepArg{
				{
					Name: "string_arg",
					Value: &ast.ArgValue{
						Literal: &ast.Literal{
							String: stringPtr("test string"),
						},
					},
				},
				{
					Name: "int_arg",
					Value: &ast.ArgValue{
						Literal: &ast.Literal{
							Int: intPtr(42),
						},
					},
				},
				{
					Name: "float_arg",
					Value: &ast.ArgValue{
						Literal: &ast.Literal{
							Float: float64Ptr(3.14),
						},
					},
				},
				{
					Name: "bool_arg",
					Value: &ast.ArgValue{
						Literal: &ast.Literal{
							Bool: boolPtr(true),
						},
					},
				},
			},
			expected: map[string]interface{}{
				"string_arg": "test string",
				"int_arg":    42,
				"float_arg":  3.14,
				"bool_arg":   true,
			},
			wantErr: false,
		},
		{
			name: "Fact paths",
			args: []*ast.StepArg{
				{
					Name: "customer_id",
					Value: &ast.ArgValue{
						PathExpr: &ast.PathExpression{
							Path: "customer.id",
						},
					},
				},
				{
					Name: "order_total",
					Value: &ast.ArgValue{
						PathExpr: &ast.PathExpression{
							Path: "order.total",
						},
					},
				},
			},
			expected: map[string]interface{}{
				"customer_id": "customer.id",
				"order_total": "order.total",
			},
			wantErr: false,
		},
		{
			name: "Variable references with bindings",
			args: []*ast.StepArg{
				{
					Name: "result",
					Value: &ast.ArgValue{
						VarRef: "$prev_result",
					},
				},
				{
					Name: "calculated",
					Value: &ast.ArgValue{
						VarRef: "$calculated_value",
					},
				},
			},
			bindings: map[string]interface{}{
				"prev_result":      "success",
				"calculated_value": 100.5,
			},
			expected: map[string]interface{}{
				"result":     "success",
				"calculated": 100.5,
			},
			wantErr: false,
		},
		{
			name: "Variable reference without binding",
			args: []*ast.StepArg{
				{
					Name: "missing_var",
					Value: &ast.ArgValue{
						VarRef: "$nonexistent",
					},
				},
			},
			bindings: map[string]interface{}{
				"other_var": "exists",
			},
			expected: nil,
			wantErr:  true,
		},
		{
			name: "Mixed argument types",
			args: []*ast.StepArg{
				{
					Name: "literal",
					Value: &ast.ArgValue{
						Literal: &ast.Literal{
							String: stringPtr("some text"),
						},
					},
				},
				{
					Name: "path",
					Value: &ast.ArgValue{
						PathExpr: &ast.PathExpression{
							Path: "customer.email",
						},
					},
				},
				{
					Name: "variable",
					Value: &ast.ArgValue{
						VarRef: "$user_name",
					},
				},
			},
			bindings: map[string]interface{}{
				"user_name": "Alice",
			},
			expected: map[string]interface{}{
				"literal":  "some text",
				"path":     "customer.email",
				"variable": "Alice",
			},
			wantErr: false,
		},
		{
			name: "Nil value",
			args: []*ast.StepArg{
				{
					Name:  "empty_arg",
					Value: nil,
				},
			},
			expected: map[string]interface{}{
				"empty_arg": nil,
			},
			wantErr: false,
		},
		{
			name: "Complex literals",
			args: []*ast.StepArg{
				{
					Name: "list_arg",
					Value: &ast.ArgValue{
						Literal: &ast.Literal{
							List: []ast.Literal{
								{String: stringPtr("item1")},
								{Int: intPtr(2)},
							},
						},
					},
				},
				{
					Name: "map_arg",
					Value: &ast.ArgValue{
						Literal: &ast.Literal{
							Map: []*ast.MapEntry{
								{
									Key:   "name",
									Value: ast.Literal{String: stringPtr("test")},
								},
								{
									Key:   "value",
									Value: ast.Literal{Int: intPtr(10)},
								},
							},
						},
					},
				},
			},
			expected: map[string]interface{}{
				"list_arg": []interface{}{"item1", 2},
				"map_arg": map[string]interface{}{
					"name":  "test",
					"value": 10,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := common.CompileArgs(tt.args, tt.bindings)

			if tt.wantErr && err == nil {
				t.Errorf("CompileArgs() expected error but got none")
				return
			}

			if !tt.wantErr && err != nil {
				t.Errorf("CompileArgs() unexpected error: %v", err)
				return
			}

			if tt.wantErr {
				return // Skip further checks if we expected an error
			}

			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("CompileArgs() = %v, want %v", result, tt.expected)
			}
		})
	}
}
