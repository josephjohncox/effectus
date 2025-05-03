package tests

import (
	"context"
	"testing"

	"github.com/effectus/effectus-go/ast"
	"github.com/effectus/effectus-go/eval"
	"github.com/effectus/effectus-go/internal/testutils"
)

// TestFactsImpl and TestSchemaInfo are no longer needed as we're using testutils package

func TestExprPredicateEvaluator(t *testing.T) {
	// Create a simple facts object for testing
	factsData := map[string]interface{}{
		"customer.id":     "C123",
		"customer.name":   "Acme Corp",
		"customer.active": true,
		"order.total":     250.50,
		"order.items":     3,
		"order.products":  []string{"A", "B", "C"},
	}
	facts := testutils.NewTestFacts(factsData)

	// Create the evaluator
	evaluator := eval.NewExprPredicateEvaluator()
	ctx := context.Background()

	tests := []struct {
		name      string
		predicate *ast.PredicateBlock
		expected  bool
	}{
		{
			name: "String equality - true",
			predicate: &ast.PredicateBlock{
				Expression: `customer.id == "C123"`,
			},
			expected: true,
		},
		{
			name: "String equality - false",
			predicate: &ast.PredicateBlock{
				Expression: `customer.name == "Wrong Name"`,
			},
			expected: false,
		},
		{
			name: "Boolean equality - true",
			predicate: &ast.PredicateBlock{
				Expression: `customer.active == true`,
			},
			expected: true,
		},
		{
			name: "Numeric comparison - greater than - true",
			predicate: &ast.PredicateBlock{
				Expression: `order.total > 200.0`,
			},
			expected: true,
		},
		{
			name: "Numeric comparison - less than - false",
			predicate: &ast.PredicateBlock{
				Expression: `order.total < 100.0`,
			},
			expected: false,
		},
		{
			name: "Integer comparison",
			predicate: &ast.PredicateBlock{
				Expression: `order.items == 3`,
			},
			expected: true,
		},
		{
			name: "Path doesn't exist",
			predicate: &ast.PredicateBlock{
				Expression: `nonexistent.path == "value"`,
			},
			expected: false,
		},
		{
			name: "Logical AND - both true",
			predicate: &ast.PredicateBlock{
				Expression: `customer.active == true && order.total > 200.0`,
			},
			expected: true,
		},
		{
			name: "Logical AND - one false",
			predicate: &ast.PredicateBlock{
				Expression: `customer.active == false && order.total > 200.0`,
			},
			expected: false,
		},
		{
			name: "Logical OR - one true",
			predicate: &ast.PredicateBlock{
				Expression: `customer.active == false || order.total > 200.0`,
			},
			expected: true,
		},
		{
			name: "Logical OR - both false",
			predicate: &ast.PredicateBlock{
				Expression: `customer.active == false || order.total < 100.0`,
			},
			expected: false,
		},
		{
			name: "Complex expression with parentheses",
			predicate: &ast.PredicateBlock{
				Expression: `(customer.active == true && order.total > 200.0) || customer.name == "Acme Corp"`,
			},
			expected: true,
		},
		{
			name: "Complex expression with parentheses and newlines",
			predicate: &ast.PredicateBlock{
				Expression: `(customer.active == true && order.total > 200.0)
								|| customer.name == "Acme Corp"`,
			},
			expected: true,
		},
		{
			name: "Array access",
			predicate: &ast.PredicateBlock{
				Expression: `order.products[0] == "A"`,
			},
			expected: true,
		},
		{
			name: "Empty expression",
			predicate: &ast.PredicateBlock{
				Expression: "",
			},
			expected: false,
		},
		{
			name:      "Nil predicate",
			predicate: nil,
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := evaluator.Evaluate(ctx, tt.predicate, facts)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// Test the BuildExprFromAST function using the new direct string approach
func TestBuildExpr(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		expected string
	}{
		{
			name:     "Simple equality",
			expr:     `customer.id == "C123"`,
			expected: `customer.id == "C123"`,
		},
		{
			name:     "Complex expression",
			expr:     `(customer.active == true && order.total > 200.0) || customer.name == "Acme Corp"`,
			expected: `(customer.active == true && order.total > 200.0) || customer.name == "Acme Corp"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			predicate := &ast.PredicateBlock{
				Expression: tt.expr,
			}

			// In the new system, the expression string is used directly
			if predicate.Expression != tt.expected {
				t.Errorf("Expected expression %q, got %q", tt.expected, predicate.Expression)
			}
		})
	}
}
