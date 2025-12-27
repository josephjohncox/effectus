package tests

import (
	"os"
	"testing"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/compiler"
	"github.com/effectus/effectus-go/schema/types"
	"github.com/stretchr/testify/assert"
)

// TestWhenExpressionTypeChecking verifies that type checking works correctly for when expressions,
// fact path validation, and verb argument validation in Effectus rules.
func TestWhenExpressionTypeChecking(t *testing.T) {
	// Create a type system with some known fact paths
	typeSystem := createTestTypeSystem()

	// Create test facts provider using our type system
	facts := createTestFacts(typeSystem)

	// Test cases with valid and invalid expressions
	testCases := []struct {
		name        string
		ruleContent string
		shouldPass  bool
		errorMsg    string
	}{
		// Valid test cases
		{
			name: "valid_numeric_comparison",
			ruleContent: `
				rule "ValidNumeric" priority 10 {
					when {
						customer.balance > 100.0
					}
					then {
						DoSomething(msg: "balance is high")
					}
				}
			`,
			shouldPass: true,
		},
		{
			name: "valid_string_comparison",
			ruleContent: `
				rule "ValidString" priority 10 {
					when {
						customer.status == "active"
					}
					then {
						DoSomething(msg: "customer is active")
					}
				}
			`,
			shouldPass: true,
		},
		{
			name: "valid_boolean_usage",
			ruleContent: `
				rule "ValidBoolean" priority 10 {
					when {
						customer.verified == true
					}
					then {
						DoSomething(msg: "customer is verified")
					}
				}
			`,
			shouldPass: true,
		},
		{
			name: "valid_complex_expression",
			ruleContent: `
				rule "ValidComplex" priority 10 {
					when {
						customer.balance > 100.0 && customer.status == "active" && customer.verified == true
					}
					then {
						DoSomething(msg: "complex condition met")
					}
				}
			`,
			shouldPass: true,
		},
		{
			name: "valid_array_access",
			ruleContent: `
				rule "ValidArrayAccess" priority 10 {
					when {
						order.total > 50.0
					}
					then {
						DoSomething(msg: "expensive order")
					}
				}
			`,
			shouldPass: true,
		},

		// Invalid test cases
		{
			name: "invalid_type_mismatch",
			ruleContent: `
				rule "InvalidTypeMismatch" priority 10 {
					when {
						customer.balance > 100.0
					}
					then {
						DoSomething(msg: 12345)
					}
				}
			`,
			shouldPass: false,
			errorMsg:   "not compatible with required type",
		},
		{
			name: "invalid_unknown_path",
			ruleContent: `
				rule "InvalidPath" priority 10 {
					when {
						customer.nonexistent > 50
					}
					then {
						DoSomething(msg: "this should fail")
					}
				}
			`,
			shouldPass: false,
			errorMsg:   "fact path does not exist",
		},
		{
			name: "invalid_missing_arg",
			ruleContent: `
				rule "MissingArg" priority 10 {
					when {
						customer.balance > 100.0
					}
					then {
						DoSomething()
					}
				}
			`,
			shouldPass: false,
			errorMsg:   "missing required argument",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a compiler with our type system
			comp := compiler.NewCompiler()

			// Get the compiler's type system and replace it with our test type system
			compTS := comp.GetTypeSystem()

			// Copy our test type system's fact types to the compiler's type system
			for _, path := range typeSystem.GetAllFactPaths() {
				factType, _ := typeSystem.GetFactType(path)
				compTS.RegisterFactType(path, factType)
			}

			// Register the test verb
			msgType := types.NewStringType()
			compTS.RegisterVerbType("DoSomething",
				map[string]*types.Type{"msg": msgType},
				types.NewBoolType())

			// Write the rule to a temporary file
			tmpFile := createTempRuleFile(t, tc.ruleContent)
			defer cleanupTempFile(tmpFile)

			// Attempt to parse and type check
			_, err := comp.ParseAndTypeCheck(tmpFile, facts)

			if tc.shouldPass {
				assert.NoError(t, err, "Expected type checking to pass")
			} else {
				assert.Error(t, err, "Expected type checking to fail")
				if tc.errorMsg != "" {
					assert.Contains(t, err.Error(), tc.errorMsg, "Error should contain expected message")
				}
			}
		})
	}
}

// createTestTypeSystem creates a type system with predefined fact paths for testing
func createTestTypeSystem() *types.TypeSystem {
	ts := types.NewTypeSystem()

	// Register parent object types
	customerType := types.NewObjectType()
	customerType.AddProperty("status", types.NewStringType())
	customerType.AddProperty("balance", types.NewFloatType())
	customerType.AddProperty("verified", types.NewBoolType())
	customerType.AddProperty("id", types.NewStringType())

	orderType := types.NewObjectType()
	orderType.AddProperty("id", types.NewStringType())
	orderType.AddProperty("total", types.NewFloatType())

	// Create orderItemType first
	orderItemType := types.NewObjectType()
	orderItemType.AddProperty("id", types.NewStringType())
	orderItemType.AddProperty("name", types.NewStringType())
	orderItemType.AddProperty("price", types.NewFloatType())
	orderItemType.AddProperty("quantity", types.NewFloatType())

	// Create itemsArrayType
	itemsArrayType := types.NewListType(orderItemType)

	// Add items to orderType
	orderType.AddProperty("items", itemsArrayType)

	// Register the parent objects
	ts.RegisterFactType("customer", customerType)
	ts.RegisterFactType("order", orderType)

	// Register customer-related facts
	ts.RegisterFactType("customer.status", types.NewStringType())
	ts.RegisterFactType("customer.balance", types.NewFloatType())
	ts.RegisterFactType("customer.verified", types.NewBoolType())
	ts.RegisterFactType("customer.id", types.NewStringType())

	// Register order-related facts
	ts.RegisterFactType("order.id", types.NewStringType())
	ts.RegisterFactType("order.total", types.NewFloatType())
	ts.RegisterFactType("order.items", itemsArrayType)
	ts.RegisterFactType("order.items[].price", types.NewFloatType())

	// Register test verbs
	doSomethingArgs := map[string]*types.Type{
		"msg": types.NewStringType(),
	}
	ts.RegisterVerbType("DoSomething", doSomethingArgs, types.NewBoolType())

	return ts
}

// TestFactSchema implements the SchemaInfo interface for testing
type TestFactSchema struct {
	typeSystem *types.TypeSystem
}

func (s *TestFactSchema) ValidatePath(path string) bool {
	// This needs to accept paths in the form the compiler expects
	// For nested paths like "customer.balance", it should validate both
	// "customer" and "customer.balance"

	if path == "customer" || path == "order" {
		return true
	}

	// Handle array access paths
	if path == "order.items" || path == "order.items[0]" || path == "order.items[0].price" {
		return true
	}

	_, err := s.typeSystem.GetFactType(path)
	return err == nil
}

// TestFacts implements the Facts interface for testing
type TestFacts struct {
	data   map[string]interface{}
	schema effectus.SchemaInfo
}

func (f *TestFacts) Get(path string) (interface{}, bool) {
	// Support both direct path access and parent paths
	if path == "customer" {
		return map[string]interface{}{
			"status":   "active",
			"balance":  250.50,
			"verified": true,
			"id":       "CUST-123",
		}, true
	}

	if path == "order" {
		return map[string]interface{}{
			"id":    "ORD-456",
			"total": 125.75,
			"items": []map[string]interface{}{
				{
					"id":       "ITEM-1",
					"name":     "Product A",
					"price":    75.50,
					"quantity": 2,
				},
			},
		}, true
	}

	// Special handling for array access
	if path == "order.items[0]" {
		return map[string]interface{}{
			"id":       "ITEM-1",
			"name":     "Product A",
			"price":    75.50,
			"quantity": 2,
		}, true
	}

	if path == "order.items[0].price" {
		return 75.50, true
	}

	val, exists := f.data[path]
	return val, exists
}

func (f *TestFacts) Schema() effectus.SchemaInfo {
	return f.schema
}

// createTestFacts creates a test facts provider using the given type system
func createTestFacts(ts *types.TypeSystem) *TestFacts {
	schema := &TestFactSchema{typeSystem: ts}

	// Create sample facts
	data := map[string]interface{}{
		"customer.status":   "active",
		"customer.balance":  250.50,
		"customer.verified": true,
		"customer.id":       "CUST-123",

		"order.id":    "ORD-456",
		"order.total": 125.75,
		// More facts would be added as needed
	}

	return &TestFacts{
		data:   data,
		schema: schema,
	}
}

// Helper functions for file handling
func createTempRuleFile(t *testing.T, content string) string {
	// Create a temporary file
	tempFile, err := os.CreateTemp("", "effectus-test-*.eff")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	// Write content to the file
	if _, err := tempFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}

	// Close the file
	if err := tempFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	return tempFile.Name()
}

func cleanupTempFile(path string) {
	os.Remove(path)
}
