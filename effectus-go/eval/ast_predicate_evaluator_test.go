package eval

import (
	"reflect"
	"testing"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/ast"
	"github.com/effectus/effectus-go/schema"
)

// Mock implementation of FactPathResolver for testing
type MockFactPathResolver struct {
	typeSystem *schema.TypeSystem
}

func (r *MockFactPathResolver) Resolve(facts effectus.Facts, path schema.FactPath) (interface{}, bool) {
	// Simple implementation that just uses Facts.Get
	return facts.Get(path.String())
}

func (r *MockFactPathResolver) Type(path schema.FactPath) *schema.Type {
	if r.typeSystem != nil {
		if typ, exists := r.typeSystem.GetFactType(path.String()); exists {
			return typ
		}
	}
	return &schema.Type{PrimType: schema.TypeUnknown}
}

func TestAstPredicateEvaluator(t *testing.T) {
	// Create a simple facts object for testing
	factsData := map[string]interface{}{
		"customer.id":     "C123",
		"customer.name":   "Acme Corp",
		"customer.active": true,
		"order.total":     250.50,
		"order.items":     3,
	}
	facts := &TestFacts{data: factsData}

	// Create a resolver
	resolver := &MockFactPathResolver{}

	// Create the evaluator
	evaluator := NewPredicateEvaluator()

	// Helper functions to create pointers for AST literals
	stringPtr := func(s string) *string { return &s }
	intPtr := func(i int) *int { return &i }
	floatPtr := func(f float64) *float64 { return &f }
	boolPtr := func(b bool) *bool { return &b }

	tests := []struct {
		name      string
		predicate *ast.Predicate
		expected  bool
	}{
		{
			name: "String equality - true",
			predicate: &ast.Predicate{
				PathExpr: &ast.PathExpression{
					Raw:       "customer.id",
					Namespace: "customer",
					Segments:  []string{"id"},
				},
				Op: "==",
				Lit: ast.Literal{
					String: stringPtr("C123"),
				},
			},
			expected: true,
		},
		{
			name: "String equality - false",
			predicate: &ast.Predicate{
				PathExpr: &ast.PathExpression{
					Raw:       "customer.name",
					Namespace: "customer",
					Segments:  []string{"name"},
				},
				Op: "==",
				Lit: ast.Literal{
					String: stringPtr("Wrong Name"),
				},
			},
			expected: false,
		},
		{
			name: "Boolean equality - true",
			predicate: &ast.Predicate{
				PathExpr: &ast.PathExpression{
					Raw:       "customer.active",
					Namespace: "customer",
					Segments:  []string{"active"},
				},
				Op: "==",
				Lit: ast.Literal{
					Bool: boolPtr(true),
				},
			},
			expected: true,
		},
		{
			name: "Numeric comparison - greater than - true",
			predicate: &ast.Predicate{
				PathExpr: &ast.PathExpression{
					Raw:       "order.total",
					Namespace: "order",
					Segments:  []string{"total"},
				},
				Op: ">",
				Lit: ast.Literal{
					Float: floatPtr(200.0),
				},
			},
			expected: true,
		},
		{
			name: "Numeric comparison - less than - false",
			predicate: &ast.Predicate{
				PathExpr: &ast.PathExpression{
					Raw:       "order.total",
					Namespace: "order",
					Segments:  []string{"total"},
				},
				Op: "<",
				Lit: ast.Literal{
					Float: floatPtr(100.0),
				},
			},
			expected: false,
		},
		{
			name: "Integer comparison",
			predicate: &ast.Predicate{
				PathExpr: &ast.PathExpression{
					Raw:       "order.items",
					Namespace: "order",
					Segments:  []string{"items"},
				},
				Op: "==",
				Lit: ast.Literal{
					Int: intPtr(3),
				},
			},
			expected: true,
		},
		{
			name: "Path doesn't exist",
			predicate: &ast.Predicate{
				PathExpr: &ast.PathExpression{
					Raw:       "nonexistent.path",
					Namespace: "nonexistent",
					Segments:  []string{"path"},
				},
				Op: "==",
				Lit: ast.Literal{
					String: stringPtr("value"),
				},
			},
			expected: false,
		},
		{
			name: "Nil path expression",
			predicate: &ast.Predicate{
				PathExpr: nil,
				Op:       "==",
				Lit: ast.Literal{
					String: stringPtr("value"),
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := evaluator.Evaluate(tt.predicate, facts, resolver)
			if result != tt.expected {
				t.Errorf("Evaluate() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestConvertAstLiteral(t *testing.T) {
	// Helper functions to create pointers for AST literals
	stringPtr := func(s string) *string { return &s }
	intPtr := func(i int) *int { return &i }
	floatPtr := func(f float64) *float64 { return &f }
	boolPtr := func(b bool) *bool { return &b }

	tests := []struct {
		name     string
		literal  *ast.Literal
		expected interface{}
	}{
		{
			name: "String literal",
			literal: &ast.Literal{
				String: stringPtr("test string"),
			},
			expected: "test string",
		},
		{
			name: "Int literal",
			literal: &ast.Literal{
				Int: intPtr(42),
			},
			expected: 42,
		},
		{
			name: "Float literal",
			literal: &ast.Literal{
				Float: floatPtr(3.14159),
			},
			expected: 3.14159,
		},
		{
			name: "Bool literal - true",
			literal: &ast.Literal{
				Bool: boolPtr(true),
			},
			expected: true,
		},
		{
			name: "Bool literal - false",
			literal: &ast.Literal{
				Bool: boolPtr(false),
			},
			expected: false,
		},
		{
			name: "List literal",
			literal: &ast.Literal{
				List: []ast.Literal{
					{String: stringPtr("first")},
					{Int: intPtr(2)},
				},
			},
			expected: []interface{}{"first", 2},
		},
		{
			name: "Map literal",
			literal: &ast.Literal{
				Map: []*ast.MapEntry{
					{
						Key: "name",
						Value: ast.Literal{
							String: stringPtr("Alice"),
						},
					},
					{
						Key: "age",
						Value: ast.Literal{
							Int: intPtr(30),
						},
					},
				},
			},
			expected: map[string]interface{}{
				"name": "Alice",
				"age":  30,
			},
		},
		{
			name:     "Nil literal",
			literal:  &ast.Literal{},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertAstLiteral(tt.literal)

			// Special handling for comparison of complex types
			if !compareValues(result, tt.expected) {
				t.Errorf("convertAstLiteral() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// Helper function to compare complex values
func compareValues(a, b interface{}) bool {
	// Handle nil cases
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	// Use reflect.DeepEqual for complex comparisons
	return reflect.DeepEqual(a, b)
}
