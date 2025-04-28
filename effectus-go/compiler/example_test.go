package compiler

import (
	"fmt"
	"os"
	"testing"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/schema"
)

// We need to ensure SimpleFacts properly implements the Facts interface
// This type adapter ensures the interface compatibility
type testFacts struct {
	*schema.SimpleFacts
}

func (f *testFacts) Schema() effectus.SchemaInfo {
	return f.SimpleFacts.Schema()
}

func (f *testFacts) Get(path string) (interface{}, bool) {
	return f.SimpleFacts.Get(path)
}

// Create test facts with proper interface implementation
func createTestFacts(data map[string]interface{}) effectus.Facts {
	schemaInfo := &schema.SimpleSchema{}
	simpleFacts := schema.NewSimpleFacts(data, schemaInfo)
	return &testFacts{SimpleFacts: simpleFacts}
}

func TestCompilerWithTypeChecking(t *testing.T) {
	// Create a temporary rule file
	ruleContent := `
rule "HighValueOrderEmail" priority 10 {
  when {
    order.total > 1000.0
    customer.vip == true
  }
  then {
    SendEmail to: customer.email subject: "VIP Order" body: "Thank you for your high-value order!"
    LogOrder order_id: order.id total: order.total
  }
}

flow "NewCustomerFlow" priority 5 {
  when {
    customer.isNew == true
  }
  steps {
    CreateOrder customer_id: customer.id items: customer.cart -> order
    SendEmail to: customer.email subject: "Welcome" body: "Thank you for your first order!"
  }
}
`
	// Write to a temporary file
	tmpFile, err := os.CreateTemp("", "example-rule-*.effx")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(ruleContent)); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}

	// Create a compiler
	compiler := NewCompiler()

	// Create test facts
	data := map[string]interface{}{
		"customer": map[string]interface{}{
			"id":    "cust-123",
			"email": "test@example.com",
			"vip":   true,
			"isNew": true,
			"cart": []interface{}{
				map[string]interface{}{
					"id":       "product-1",
					"quantity": 2,
					"price":    49.99,
				},
			},
		},
		"order": map[string]interface{}{
			"id":    "order-456",
			"total": 1500.0,
		},
	}
	facts := createTestFacts(data)

	// Parse and type check the file
	file, err := compiler.ParseAndTypeCheck(tmpFile.Name(), facts)
	if err != nil {
		t.Fatalf("Parse and type check failed: %v", err)
	}

	// Verify the parsed file
	if len(file.Rules) != 1 {
		t.Errorf("Expected 1 rule, got %d", len(file.Rules))
	}
	if len(file.Flows) != 1 {
		t.Errorf("Expected 1 flow, got %d", len(file.Flows))
	}

	// Generate a type report
	typeReport := compiler.GenerateTypeReport()
	fmt.Println("Type Report:\n", typeReport)

	// Verify some inferred types
	checker := compiler.typeChecker
	if orderTotal, exists := checker.GetFactType("order.total"); !exists || orderTotal.PrimType != schema.TypeFloat {
		t.Errorf("Expected order.total to be a float")
	}
}

func ExampleCompiler_ParseAndTypeCheck() {
	// Create a compiler
	compiler := NewCompiler()

	// Register some types directly for demonstration
	compiler.registerDefaultVerbTypes()

	// Generate a type report
	typeReport := compiler.GenerateTypeReport()
	fmt.Println("Type Report:")
	fmt.Println(typeReport)

	// Output:
	// Type Report:
	// # Effectus Type Report
	//
	// ## Fact Types
	//
	// No fact types inferred or registered.
	//
	// ## Verb Types
	//
	// ### SendEmail
	// Arguments:
	// - `to`: string
	// - `subject`: string
	// - `body`: string
	// Return type: bool
	//
	// ### LogOrder
	// Arguments:
	// - `order_id`: string
	// - `total`: float
	// Return type: bool
	//
	// ### CreateOrder
	// Arguments:
	// - `customer_id`: string
	// - `items`: []OrderItem
	// Return type: Order
}
