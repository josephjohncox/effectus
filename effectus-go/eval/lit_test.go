package eval

import (
	"reflect"
	"testing"

	"github.com/effectus/effectus-go/ast"
)

func TestCompileLiteral(t *testing.T) {
	// Helper functions to create pointers to literals for AST values
	stringPtr := func(s string) *string { return &s }
	intPtr := func(i int) *int { return &i }
	float64Ptr := func(f float64) *float64 { return &f }
	boolPtr := func(b bool) *bool { return &b }

	tests := []struct {
		name     string
		literal  ast.Literal
		expected interface{}
	}{
		{
			name: "string literal",
			literal: ast.Literal{
				String: stringPtr("test string"),
			},
			expected: "test string",
		},
		{
			name: "int literal",
			literal: ast.Literal{
				Int: intPtr(42),
			},
			expected: 42,
		},
		{
			name: "float literal",
			literal: ast.Literal{
				Float: float64Ptr(3.14159),
			},
			expected: 3.14159,
		},
		{
			name: "bool literal - true",
			literal: ast.Literal{
				Bool: boolPtr(true),
			},
			expected: true,
		},
		{
			name: "bool literal - false",
			literal: ast.Literal{
				Bool: boolPtr(false),
			},
			expected: false,
		},
		{
			name: "empty list literal",
			literal: ast.Literal{
				List: []ast.Literal{},
			},
			expected: []interface{}{},
		},
		{
			name: "list with mixed values",
			literal: ast.Literal{
				List: []ast.Literal{
					{String: stringPtr("first")},
					{Int: intPtr(2)},
					{Bool: boolPtr(true)},
				},
			},
			expected: []interface{}{"first", 2, true},
		},
		{
			name: "empty map literal",
			literal: ast.Literal{
				Map: []*ast.MapEntry{},
			},
			expected: map[string]interface{}{},
		},
		{
			name: "map with mixed values",
			literal: ast.Literal{
				Map: []*ast.MapEntry{
					{
						Key:   "name",
						Value: ast.Literal{String: stringPtr("Alice")},
					},
					{
						Key:   "age",
						Value: ast.Literal{Int: intPtr(30)},
					},
					{
						Key:   "active",
						Value: ast.Literal{Bool: boolPtr(true)},
					},
				},
			},
			expected: map[string]interface{}{
				"name":   "Alice",
				"age":    30,
				"active": true,
			},
		},
		{
			name: "nested structures",
			literal: ast.Literal{
				Map: []*ast.MapEntry{
					{
						Key: "person",
						Value: ast.Literal{
							Map: []*ast.MapEntry{
								{
									Key:   "name",
									Value: ast.Literal{String: stringPtr("Bob")},
								},
								{
									Key: "scores",
									Value: ast.Literal{
										List: []ast.Literal{
											{Int: intPtr(85)},
											{Int: intPtr(90)},
											{Int: intPtr(95)},
										},
									},
								},
							},
						},
					},
				},
			},
			expected: map[string]interface{}{
				"person": map[string]interface{}{
					"name":   "Bob",
					"scores": []interface{}{85, 90, 95},
				},
			},
		},
		{
			name:     "nil literal",
			literal:  ast.Literal{},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CompileLiteral(&tt.literal)

			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("CompileLiteral() = %v, want %v", result, tt.expected)
			}
		})
	}
}
