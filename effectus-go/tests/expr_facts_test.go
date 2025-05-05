package tests

import (
	"testing"

	"github.com/effectus/effectus-go/pathutil"
	"github.com/expr-lang/expr"
	"github.com/stretchr/testify/assert"
)

// TestExprFactsEvaluation tests the evaluation of fact paths using expr
func TestExprFactsEvaluation(t *testing.T) {
	// Create a structured fact registry
	registry := pathutil.NewStructFactRegistry()

	// Create sample structured data
	customerData := map[string]interface{}{
		"id":            "CUST-123",
		"name":          "John Doe",
		"status":        "active",
		"balance":       1250.75,
		"premium":       true,
		"loyalty_years": 7,
		"credit_score":  750,
		"risk_level":    2,
	}

	// Create order data with nested structures
	orderItems := []map[string]interface{}{
		{
			"id":       "ITEM-1",
			"name":     "Premium Widget",
			"price":    99.99,
			"quantity": 2,
			"taxable":  true,
		},
		{
			"id":       "ITEM-2",
			"name":     "Basic Widget",
			"price":    49.99,
			"quantity": 1,
			"taxable":  true,
		},
	}

	shippingAddress := map[string]interface{}{
		"street":  "123 Main St",
		"city":    "Anytown",
		"state":   "CA",
		"country": "USA",
		"zip":     "90210",
	}

	shipping := map[string]interface{}{
		"method":  "standard",
		"cost":    12.99,
		"express": false,
		"address": shippingAddress,
	}

	orderData := map[string]interface{}{
		"id":          "ORD-456",
		"date":        "2023-04-15",
		"total":       325.50,
		"tax":         32.55,
		"items_count": 3,
		"items":       orderItems,
		"shipping":    shipping,
		"customer":    customerData,
	}

	// Register the data with the registry
	registry.Register("customer", customerData)
	registry.Register("order", orderData)

	// Get the facts provider
	facts := registry.GetFacts()

	// Test cases for expr evaluation
	testCases := []struct {
		name       string
		expression string
		expected   interface{}
		shouldPass bool
	}{
		// Simple field access
		{
			name:       "simple_field_access",
			expression: "customer.name",
			expected:   "John Doe",
			shouldPass: true,
		},
		// Numeric comparison
		{
			name:       "numeric_comparison",
			expression: "customer.balance > 1000.0",
			expected:   true,
			shouldPass: true,
		},
		// Boolean field
		{
			name:       "boolean_field",
			expression: "customer.premium",
			expected:   true,
			shouldPass: true,
		},
		// Complex boolean expression
		{
			name:       "complex_boolean_expression",
			expression: "customer.premium && customer.balance > 1000.0",
			expected:   true,
			shouldPass: true,
		},
		// Nested field access
		{
			name:       "nested_field_access",
			expression: "order.shipping.address.country",
			expected:   "USA",
			shouldPass: true,
		},
		// Array/List access
		{
			name:       "array_access",
			expression: "order.items[0].name",
			expected:   "Premium Widget",
			shouldPass: true,
		},
		// Complex nested condition
		{
			name:       "complex_nested_condition",
			expression: "(customer.premium || customer.loyalty_years > 5) && (order.total > 300.0)",
			expected:   true,
			shouldPass: true,
		},
		// Function application with len()
		{
			name:       "function_application",
			expression: "len(order.items) > 1",
			expected:   true,
			shouldPass: true,
		},
		// Math expressions
		{
			name:       "math_expression",
			expression: "order.items[0].price * order.items[0].quantity + order.items[1].price * order.items[1].quantity",
			expected:   99.99*2 + 49.99,
			shouldPass: true,
		},
		// String operations
		{
			name:       "string_operations",
			expression: "customer.status == 'active' ? 'Customer is active' : 'Customer is inactive'",
			expected:   "Customer is active",
			shouldPass: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Evaluate the expression directly
			result, err := facts.EvaluateExpr(tc.expression)

			// Check the result
			if tc.shouldPass {
				assert.NoError(t, err, "Expression should evaluate without error")
				assert.Equal(t, tc.expected, result, "Expression result should match expected value")
			} else {
				assert.Error(t, err, "Expression should fail to evaluate")
			}
		})
	}
}

// TestJSONAdapter tests using the JSON adapter
func TestJSONAdapter(t *testing.T) {
	// Create a registry
	registry := pathutil.NewStructFactRegistry()

	// Create an adapter
	adapter := pathutil.NewJSONToFactsAdapter(registry)

	// Get example JSON data
	jsonData := pathutil.CreateJSONExample()

	// Register the data
	err := adapter.RegisterJSON("customer", jsonData)
	assert.NoError(t, err, "Should register JSON data without error")

	// Get the fact provider
	facts := adapter.BuildFactProvider()

	// Check a few paths
	customerName, exists := facts.Get("customer.name")
	assert.True(t, exists, "Path customer.name should exist")
	assert.Equal(t, "John Smith", customerName)

	orderTotal, exists := facts.Get("customer.orders[0].total")
	assert.True(t, exists, "Path customer.orders[0].total should exist")
	assert.Equal(t, 89.99, orderTotal)

	shipMethod, exists := facts.Get("customer.orders[0].shipping.method")
	assert.True(t, exists, "Path customer.orders[0].shipping.method should exist")
	assert.Equal(t, "express", shipMethod)

	// Test using Expr for complex expressions
	env := make(map[string]interface{})
	env["customer"], _ = registry.GetFacts().Get("customer")

	// Create and run a simple expression
	program, err := expr.Compile("customer.orders[0].items[0].price + customer.orders[0].items[1].price", expr.Env(env))
	assert.NoError(t, err, "Should compile expression without error")

	result, err := expr.Run(program, env)
	assert.NoError(t, err, "Should run expression without error")
	assert.Equal(t, 49.99+19.99, result)
}
