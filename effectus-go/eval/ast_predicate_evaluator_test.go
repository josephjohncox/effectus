package eval

import (
	"testing"

	"github.com/effectus/effectus-go/ast"
	"github.com/effectus/effectus-go/common"
	"github.com/effectus/effectus-go/pathutil"
)

// TestFactsImpl implements common.Facts for testing
type TestFactsImpl struct {
	data map[string]interface{}
}

func (f *TestFactsImpl) Get(path pathutil.Path) (interface{}, bool) {
	val, ok := f.data[path.String()]
	return val, ok
}

func (f *TestFactsImpl) GetWithContext(path pathutil.Path) (interface{}, *common.ResolutionResult) {
	val, ok := f.Get(path)

	if !ok {
		return nil, &common.ResolutionResult{
			Path:   path,
			Exists: false,
		}
	}

	return val, &common.ResolutionResult{
		Path:   path,
		Value:  val,
		Exists: true,
	}
}

func (f *TestFactsImpl) Schema() common.SchemaInfo {
	return nil
}

func (f *TestFactsImpl) HasPath(path pathutil.Path) bool {
	_, ok := f.data[path.String()]
	return ok
}

// StructuredFactsProvider implements pathutil.FactsProvider for testing
type StructuredFactsProvider struct {
	facts *TestFactsImpl
}

func NewStructuredFactsProvider(facts *TestFactsImpl) *StructuredFactsProvider {
	return &StructuredFactsProvider{facts: facts}
}

func (p *StructuredFactsProvider) GetByPath(path pathutil.Path) (interface{}, bool) {
	return p.facts.Get(path)
}

func (p *StructuredFactsProvider) GetByPathWithContext(path pathutil.Path) (interface{}, *pathutil.ResolutionResult) {
	val, res := p.facts.GetWithContext(path)

	if res == nil {
		return nil, &pathutil.ResolutionResult{
			Path:   path,
			Exists: false,
		}
	}

	return val, &pathutil.ResolutionResult{
		Path:   path,
		Value:  res.Value,
		Exists: res.Exists,
		Error:  res.Error,
	}
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
	facts := &TestFactsImpl{data: factsData}

	// Create a resolver
	resolver := pathutil.NewPathResolver(false)

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
					Raw: "customer.id",
					Path: func() pathutil.Path {
						path, _ := pathutil.ParseString("customer.id")
						return path
					}(),
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
					Raw: "customer.name",
					Path: func() pathutil.Path {
						path, _ := pathutil.ParseString("customer.name")
						return path
					}(),
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
					Raw: "customer.active",
					Path: func() pathutil.Path {
						path, _ := pathutil.ParseString("customer.active")
						return path
					}(),
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
					Raw: "order.total",
					Path: func() pathutil.Path {
						path, _ := pathutil.ParseString("order.total")
						return path
					}(),
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
					Raw: "order.total",
					Path: func() pathutil.Path {
						path, _ := pathutil.ParseString("order.total")
						return path
					}(),
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
					Raw: "order.items",
					Path: func() pathutil.Path {
						path, _ := pathutil.ParseString("order.items")
						return path
					}(),
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
					Raw: "nonexistent.path",
					Path: func() pathutil.Path {
						path, _ := pathutil.ParseString("nonexistent.path")
						return path
					}(),
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
				Float: floatPtr(3.14),
			},
			expected: 3.14,
		},
		{
			name: "Bool literal",
			literal: &ast.Literal{
				Bool: boolPtr(true),
			},
			expected: true,
		},
		{
			name: "List literal",
			literal: &ast.Literal{
				List: []ast.Literal{
					{Int: intPtr(1)},
					{Int: intPtr(2)},
					{Int: intPtr(3)},
				},
			},
			expected: []interface{}{1, 2, 3},
		},
		{
			name: "Map literal",
			literal: &ast.Literal{
				Map: []*ast.MapEntry{
					{Key: "key1", Value: ast.Literal{String: stringPtr("value1")}},
					{Key: "key2", Value: ast.Literal{Int: intPtr(42)}},
				},
			},
			expected: map[string]interface{}{
				"key1": "value1",
				"key2": 42,
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
			if !compareValues(result, tt.expected) {
				t.Errorf("convertAstLiteral() = %v (%T), want %v (%T)", result, result, tt.expected, tt.expected)
			}
		})
	}
}

// compareValues compares two values for equality (handles slices and maps specially)
func compareValues(a, b interface{}) bool {
	// Handle nil case
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	switch aVal := a.(type) {
	case []interface{}:
		bVal, ok := b.([]interface{})
		if !ok || len(aVal) != len(bVal) {
			return false
		}
		for i := range aVal {
			if !compareValues(aVal[i], bVal[i]) {
				return false
			}
		}
		return true
	case map[string]interface{}:
		bVal, ok := b.(map[string]interface{})
		if !ok || len(aVal) != len(bVal) {
			return false
		}
		for k, v := range aVal {
			bv, ok := bVal[k]
			if !ok || !compareValues(v, bv) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}
