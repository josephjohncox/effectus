package schema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExprSchema(t *testing.T) {
	// Create a temporary directory for test files
	tempDir, err := os.MkdirTemp("", "expr-schema-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a test JSON file
	jsonContent := `{
		"customer": {
			"name": "Acme Corp",
			"active": true,
			"contact": {
				"email": "info@acme.com",
				"phone": "555-1234"
			},
			"orders": [
				{
					"id": "ORD-001",
					"amount": 1299.99,
					"items": [
						{
							"product": "Laptop",
							"quantity": 1,
							"price": 1199.99
						},
						{
							"product": "Mouse",
							"quantity": 2,
							"price": 49.99
						}
					]
				},
				{
					"id": "ORD-002",
					"amount": 699.99,
					"items": [
						{
							"product": "Tablet",
							"quantity": 1,
							"price": 699.99
						}
					]
				}
			],
			"metrics": {
				"lifetime_value": 1999.98,
				"order_count": 2
			}
		}
	}`

	jsonPath := filepath.Join(tempDir, "customer.json")
	if err := os.WriteFile(jsonPath, []byte(jsonContent), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Create the ExprSchema
	schema := NewExprSchema("")

	// Test loading a directory
	err = schema.LoadFromDirectory(tempDir)
	assert.NoError(t, err, "Loading directory should succeed")

	// Test basic fact retrieval
	t.Run("GetBasicFacts", func(t *testing.T) {
		tests := []struct {
			path     string
			expected interface{}
			exists   bool
		}{
			{"customer.name", "Acme Corp", true},
			{"customer.active", true, true},
			{"customer.contact.email", "info@acme.com", true},
			{"customer.orders[0].id", "ORD-001", true},
			{"customer.orders[0].items[0].product", "Laptop", true},
			{"customer.metrics.order_count", float64(2), true}, // JSON numbers are parsed as float64
			{"nonexistent.path", nil, false},
		}

		for _, tt := range tests {
			value, exists := schema.Get(tt.path)
			assert.Equal(t, tt.exists, exists, "Existence check for path %s", tt.path)
			if tt.exists {
				assert.Equal(t, tt.expected, value, "Value for path %s", tt.path)
			}
		}
	})

	// Test type information
	t.Run("TypeInformation", func(t *testing.T) {
		tests := []struct {
			path     string
			expected string
		}{
			{"customer.name", "string"},
			{"customer.active", "boolean"},
			{"customer.orders", "array"},
			{"customer.orders[0]", "map"},
			{"customer.orders[0].amount", "float"},
			{"customer.metrics", "map"},
		}

		for _, tt := range tests {
			_, typeName, exists := schema.GetWithType(tt.path)
			assert.True(t, exists, "Path should exist: %s", tt.path)
			assert.Equal(t, tt.expected, typeName, "Type for path %s", tt.path)
		}
	})

	// Test expression evaluation
	t.Run("ExpressionEvaluation", func(t *testing.T) {
		tests := []struct {
			expr     string
			expected interface{}
		}{
			// Simple expressions
			{"customer.name == 'Acme Corp'", true},
			{"customer.orders[0].amount > 1000", true},
			{"len(customer.orders)", 2},

			// More complex expressions
			{"sum(map(customer.orders, {#.amount}))", 1299.99 + 699.99},
			{"any(customer.orders, {#.amount > 1000})", true},
			{"all(customer.orders, {#.amount > 1000})", false},
			{"filter(customer.orders, {#.amount > 1000})[0].id", "ORD-001"},

			// Aggregate expressions
			{"customer.orders[0].items[0].price * customer.orders[0].items[0].quantity", 1199.99},
			{"sum(map(customer.orders[0].items, {#.price * #.quantity}))", 1199.99 + (49.99 * 2)},
		}

		for _, tt := range tests {
			result, err := schema.EvaluateExpr(tt.expr)
			assert.NoError(t, err, "Expression should evaluate without error: %s", tt.expr)
			assert.Equal(t, tt.expected, result, "Result for expression: %s", tt.expr)
		}
	})

	// Test boolean expression evaluation
	t.Run("BooleanExpressions", func(t *testing.T) {
		tests := []struct {
			expr     string
			expected bool
		}{
			{"customer.active", true},
			{"customer.name startsWith 'Acme'", true},
			{"customer.metrics.order_count > 1", true},
			{"customer.metrics.lifetime_value > 2000", false},
			{"any(customer.orders, {len(#.items) > 1})", true},
			{"all(customer.orders, {#.amount > 500})", true},
		}

		for _, tt := range tests {
			result, err := schema.EvaluateExprBool(tt.expr)
			assert.NoError(t, err, "Boolean expression should evaluate without error: %s", tt.expr)
			assert.Equal(t, tt.expected, result, "Result for boolean expression: %s", tt.expr)
		}
	})

	// Test type checking
	t.Run("TypeChecking", func(t *testing.T) {
		// Valid expressions
		validExprs := []string{
			"customer.name",
			"customer.active",
			"customer.orders[0].amount + customer.orders[1].amount",
			"filter(customer.orders, {#.amount > 1000})",
		}

		for _, expr := range validExprs {
			err := schema.TypeCheckExpr(expr)
			assert.NoError(t, err, "Type check should succeed for valid expression: %s", expr)
		}

		// Invalid expressions would be tested if the TypeCheckExpr implementation was more strict
		// Currently, expr library's type checking with AllowUndefinedVariables is quite permissive
	})
}
