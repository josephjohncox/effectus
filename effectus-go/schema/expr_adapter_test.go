package schema

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/effectus/effectus-go/pathutil"
	"github.com/effectus/effectus-go/schema/registry"
	"github.com/effectus/effectus-go/schema/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExprRegistryIntegration(t *testing.T) {
	// Create a temporary directory for test files
	tempDir, err := os.MkdirTemp("", "expr-registry-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a test schema file
	schemaContent := `{
		"types": {
			"Customer": {
				"type": "object",
				"properties": {
					"name": {"type": "string"},
					"active": {"type": "boolean"},
					"age": {"type": "integer"},
					"balance": {"type": "number"}
				}
			},
			"Order": {
				"type": "object",
				"properties": {
					"id": {"type": "string"},
					"amount": {"type": "number"},
					"items": {
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"product": {"type": "string"},
								"quantity": {"type": "integer"},
								"price": {"type": "number"}
							}
						}
					}
				}
			}
		},
		"facts": {
			"customer": {"$ref": "Customer"},
			"orders": {
				"type": "array",
				"items": {"$ref": "Order"}
			}
		}
	}`

	schemaPath := filepath.Join(tempDir, "schema.json")
	if err := os.WriteFile(schemaPath, []byte(schemaContent), 0644); err != nil {
		t.Fatalf("Failed to write schema file: %v", err)
	}

	// Create a test data file
	dataContent := `{
		"customer": {
			"name": "Acme Corp",
			"active": true,
			"age": 10,
			"balance": 5000.50
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
		]
	}`

	dataPath := filepath.Join(tempDir, "data.json")
	if err := os.WriteFile(dataPath, []byte(dataContent), 0644); err != nil {
		t.Fatalf("Failed to write data file: %v", err)
	}

	// Test with the registry adapter
	t.Run("RegistryAdapter", func(t *testing.T) {
		// Create a type system
		typeSystem := types.NewTypeSystem()

		// Create the adapter
		adapter := registry.NewExprFactsAdapter(typeSystem)

		// Load the schema
		err := adapter.Load(schemaPath, typeSystem)
		require.NoError(t, err, "Loading schema should succeed")

		// Create a fact provider from the data
		factProvider, err := adapter.CreateFactProvider(dataPath)
		require.NoError(t, err, "Creating fact provider should succeed")

		// Validate the fact provider
		value, exists := factProvider.Get("customer.name")
		assert.True(t, exists, "Path should exist")
		assert.Equal(t, "Acme Corp", value, "Value should match")

		// Check type information
		customerType, err := typeSystem.GetFactType("customer")
		assert.NoError(t, err, "Customer type should be registered")
		assert.Equal(t, types.TypeObject, customerType.PrimType, "Customer should be an object type")

		// Verify property types
		nameType := customerType.Properties["name"]
		assert.NotNil(t, nameType, "Name property should exist")
		assert.Equal(t, types.TypeString, nameType.PrimType, "Name should be a string type")

		// Verify order list type
		ordersType, err := typeSystem.GetFactType("orders")
		assert.NoError(t, err, "Orders type should be registered")
		assert.Equal(t, types.TypeList, ordersType.PrimType, "Orders should be a list type")

		// Verify order element type
		orderElemType := ordersType.ElementType
		assert.NotNil(t, orderElemType, "Order element type should exist")
		assert.Equal(t, types.TypeObject, orderElemType.PrimType, "Order element should be an object type")
	})

	// Test with the ExprSchema
	t.Run("ExprSchema", func(t *testing.T) {
		// Create the schema
		schema := NewExprSchema("")

		// Load the data
		err := schema.LoadFromFile(dataPath)
		require.NoError(t, err, "Loading data should succeed")

		// Verify basic facts
		value, exists := schema.Get("customer.name")
		assert.True(t, exists, "Path should exist")
		assert.Equal(t, "Acme Corp", value, "Value should match")

		// Verify type information
		_, typeName, exists := schema.GetWithType("customer.name")
		assert.True(t, exists, "Type should exist")
		assert.Equal(t, "string", typeName, "Type should be string")

		// Test expression evaluation
		result, err := schema.EvaluateExpr("sum(map(orders, {#.amount}))")
		require.NoError(t, err, "Expression should evaluate")
		assert.Equal(t, 1299.99+699.99, result, "Expression result should match")

		// Test boolean expression
		boolResult, err := schema.EvaluateExprBool("customer.active && customer.balance > 1000")
		require.NoError(t, err, "Boolean expression should evaluate")
		assert.True(t, boolResult, "Boolean expression should be true")

		// Test nested property access
		value, exists = schema.Get("orders[0].items[1].product")
		assert.True(t, exists, "Nested path should exist")
		assert.Equal(t, "Mouse", value, "Nested value should match")
	})
}

// TestComparisonWithGjson compares the new expr-based implementation with the old gjson implementation
func TestComparisonWithGjson(t *testing.T) {
	// Skip this test if we're not in a full test environment
	// This test is for demonstration purposes only
	t.Skip("This test is for demonstration only")

	// Sample data for comparison
	sampleData := map[string]interface{}{
		"customer": map[string]interface{}{
			"name":    "Acme Corp",
			"active":  true,
			"balance": 5000.50,
		},
		"orders": []interface{}{
			map[string]interface{}{
				"id":     "ORD-001",
				"amount": 1299.99,
			},
		},
	}

	// Convert to JSON
	jsonData, _ := json.Marshal(sampleData)

	// Create expr-based provider
	jsonLoader := pathutil.NewJSONLoader(jsonData)
	exprFacts, _ := jsonLoader.LoadIntoTypedFacts()

	// Access data through expr
	t.Run("ExprBasedAccess", func(t *testing.T) {
		// Basic path access
		nameValue, exists := exprFacts.Get("customer.name")
		assert.True(t, exists)
		assert.Equal(t, "Acme Corp", nameValue)

		// Array access
		orderID, exists := exprFacts.Get("orders[0].id")
		assert.True(t, exists)
		assert.Equal(t, "ORD-001", orderID)

		// Expression evaluation
		result, err := exprFacts.GetUnderlyingFacts().EvaluateExpr("customer.active && customer.balance > 1000")
		assert.NoError(t, err)
		assert.Equal(t, true, result)
	})

	// For demonstration purposes, we would create a gjson provider and compare,
	// but for now we'll just note the differences:
	/*
		Advantages of expr-based approach:
		1. No gjson dependency
		2. Unified data loading from different sources
		3. Type system integration
		4. More powerful expressions (filtering, mapping)
		5. Better interoperability with Go structs
		6. Type checking capabilities
	*/
}
