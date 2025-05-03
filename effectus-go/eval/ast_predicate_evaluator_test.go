package eval

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/ast"
	"github.com/effectus/effectus-go/schema/path"
	"github.com/effectus/effectus-go/schema/types"
)

// Mock implementation of FactPathResolver for testing
type MockFactPathResolver struct {
	typeSystem *types.TypeSystem
}

func (r *MockFactPathResolver) Resolve(facts effectus.Facts, path path.FactPath) (interface{}, bool) {
	// Simple implementation that just uses Facts.Get
	return facts.Get(path.String())
}

// ResolveWithContext implements the FactPathResolver interface
func (r *MockFactPathResolver) ResolveWithContext(facts effectus.Facts, path path.FactPath) (interface{}, *path.PathResolutionResult) {
	// Create a basic resolution result
	result := &path.PathResolutionResult{
		Path: path.String(),
	}

	// Use the Resolve method to get the value
	value, exists := r.Resolve(facts, path)
	result.Exists = exists

	if exists {
		result.Value = value
		// Set a simple type based on the Go type
		if value != nil {
			result.ValueType = inferTypeFromGoValue(value)
		}
	} else {
		result.Error = fmt.Errorf("path not found: %s", path.String())
	}

	return value, result
}

// inferTypeFromGoValue infers a schema.Type from a Go value
func inferTypeFromGoValue(value interface{}) *types.Type {
	switch value.(type) {
	case string:
		return &types.Type{PrimType: types.TypeString}
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return &types.Type{PrimType: types.TypeInt}
	case float32, float64:
		return &types.Type{PrimType: types.TypeFloat}
	case bool:
		return &types.Type{PrimType: types.TypeBool}
	default:
		return &types.Type{PrimType: types.TypeUnknown}
	}
}

func (r *MockFactPathResolver) Type(path path.FactPath) *types.Type {
	if r.typeSystem != nil {
		if typ, exists := r.typeSystem.GetFactType(path.String()); exists {
			return typ
		}
	}
	return &types.Type{PrimType: types.TypeUnknown}
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
