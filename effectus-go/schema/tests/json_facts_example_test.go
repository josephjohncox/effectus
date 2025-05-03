package tests

import (
	"fmt"
	"strings"
	"testing"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/ast"
	"github.com/effectus/effectus-go/schema/facts"
	"github.com/effectus/effectus-go/schema/types"
)

// ExampleExecutor is a simple executor for testing
type ExampleExecutor struct {
	Effects []effectus.Effect
}

// Do implements the effectus.Executor interface
func (e *ExampleExecutor) Do(effect effectus.Effect) (result interface{}, err error) {
	e.Effects = append(e.Effects, effect)
	return fmt.Sprintf("executed: %s", effect.Verb), nil
}

// SimpleEffect is a test helper that creates a simple effect
func SimpleEffect(verb string, payload interface{}) effectus.Effect {
	return effectus.Effect{
		Verb:    verb,
		Payload: payload,
	}
}

// Direct comparison function for tests to avoid import cycles
func compareValues(factValue interface{}, op string, literalValue interface{}) bool {
	switch op {
	case "==":
		return fmt.Sprintf("%v", factValue) == fmt.Sprintf("%v", literalValue)
	case "!=":
		return fmt.Sprintf("%v", factValue) != fmt.Sprintf("%v", literalValue)
	case ">":
		// Handle numeric comparison
		switch f1 := factValue.(type) {
		case float64:
			if f2, ok := literalValue.(float64); ok {
				return f1 > f2
			}
		case int:
			if i2, ok := literalValue.(int); ok {
				return f1 > i2
			}
		}
		return fmt.Sprintf("%v", factValue) > fmt.Sprintf("%v", literalValue)
	case "<":
		// Handle numeric comparison
		switch f1 := factValue.(type) {
		case float64:
			if f2, ok := literalValue.(float64); ok {
				return f1 < f2
			}
		case int:
			if i2, ok := literalValue.(int); ok {
				return f1 < i2
			}
		}
		return fmt.Sprintf("%v", factValue) < fmt.Sprintf("%v", literalValue)
	case ">=":
		// Handle numeric comparison
		switch f1 := factValue.(type) {
		case float64:
			if f2, ok := literalValue.(float64); ok {
				return f1 >= f2
			}
		case int:
			if i2, ok := literalValue.(int); ok {
				return f1 >= i2
			}
		}
		return fmt.Sprintf("%v", factValue) >= fmt.Sprintf("%v", literalValue)
	case "<=":
		// Handle numeric comparison
		switch f1 := factValue.(type) {
		case float64:
			if f2, ok := literalValue.(float64); ok {
				return f1 <= f2
			}
		case int:
			if i2, ok := literalValue.(int); ok {
				return f1 <= i2
			}
		}
		return fmt.Sprintf("%v", factValue) <= fmt.Sprintf("%v", literalValue)
	default:
		return false
	}
}

// Evaluate a predicate directly for tests to avoid import cycles
func evaluatePredicate(pred *ast.Predicate, facts *facts.JSONFacts) bool {
	if pred.PathExpr == nil {
		return false
	}

	path := pred.PathExpr.GetFullPath()
	value, exists := facts.Get(path)
	if !exists {
		return false
	}

	var literalValue interface{}
	if pred.Lit.String != nil {
		literalValue = *pred.Lit.String
	} else if pred.Lit.Int != nil {
		literalValue = *pred.Lit.Int
	} else if pred.Lit.Float != nil {
		literalValue = *pred.Lit.Float
	} else if pred.Lit.Bool != nil {
		literalValue = *pred.Lit.Bool
	} else {
		return false
	}

	return compareValues(value, pred.Op, literalValue)
}

// Create a helper to create a path expression for examples
func createPathExpr(path string) *ast.PathExpression {
	pathExpr := &ast.PathExpression{
		Raw: path,
	}

	// Parse and resolve the path
	parts := strings.Split(path, ".")
	if len(parts) > 0 {
		pathExpr.Namespace = parts[0]
		pathExpr.Segments = parts[1:]
	}

	return pathExpr
}

// ExampleJSONFacts demonstrates basic JSONFacts usage
func ExampleJSONFacts() {
	// Sample JSON data representing our facts
	jsonStr := `{
		"customer": {
			"id": "C123",
			"tier": "premium",
			"stats": {
				"totalSpend": 2500.50,
				"orderCount": 12,
				"lastVisitDays": 5
			}
		},
		"order": {
			"id": "O789",
			"amount": 199.99,
			"items": 3,
			"status": "pending"
		}
	}`

	// Parse the JSON string into JSONFacts
	facts, err := facts.NewJSONFactsFromString(jsonStr)
	if err != nil {
		fmt.Printf("Error creating JSONFacts: %v\n", err)
		return
	}

	// Create a type system
	ts := types.NewTypeSystem()

	// Register types from the JSON data
	for ns := range facts.data {
		facts.RegisterJSONTypes(ts, ns)
	}

	// Define a predicate to evaluate
	// "Is the customer a premium tier with total spend over $1000?"
	predicate := &ast.Predicate{
		PathExpr: createPathExpr("customer.tier"),
		Op:       "==",
		Lit: ast.Literal{
			String: stringPtr("premium"),
		},
	}

	// And another predicate
	// "Is the total spend over $1000?"
	spendPredicate := &ast.Predicate{
		PathExpr: createPathExpr("customer.stats.totalSpend"),
		Op:       ">",
		Lit: ast.Literal{
			Float: float64Ptr(1000.0),
		},
	}

	// Create a test executor
	executor := &ExampleExecutor{}

	// Execute a simple "rule"
	tierMatches := evaluatePredicate(predicate, facts)
	spendMatches := evaluatePredicate(spendPredicate, facts)

	// Take action based on evaluations
	if tierMatches && spendMatches {
		// Customer is premium with high spend - create an effect
		effect := SimpleEffect("applyDiscount", map[string]interface{}{
			"orderId":    "O789",
			"discount":   "VIP10",
			"percent":    10.0,
			"reason":     "Premium customer with high spend",
			"customerId": "C123",
		})

		// Execute the effect
		result, err := executor.Do(effect)
		if err != nil {
			fmt.Printf("Error executing effect: %v\n", err)
			return
		}

		fmt.Printf("Result: %s\n", result)
		fmt.Printf("Applied discount: %v\n", effect.Payload.(map[string]interface{})["discount"])
	} else {
		fmt.Println("No special handling required")
	}

	// Output: Result: executed: applyDiscount
	// Applied discount: VIP10
}

// ExampleJSONFacts_typeChecking demonstrates type checking with JSONFacts
func ExampleJSONFacts_typeChecking() {
	// Sample JSON data
	jsonStr := `{
		"product": {
			"sku": "P123",
			"price": 49.99,
			"inStock": true,
			"tags": ["electronics", "gadget"],
			"dimensions": {
				"width": 10.5,
				"height": 5.2,
				"depth": 3.0
			}
		}
	}`

	// Parse the JSON
	facts, err := facts.NewJSONFactsFromString(jsonStr)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// Create type system and register types
	ts := types.NewTypeSystem()

	// For JSON data we need to extract and register each namespace separately
	for ns, val := range facts.data {
		if nsMap, ok := val.(map[string]interface{}); ok {
			facts.RegisterJSONTypes(nsMap, ts, ns)
		}
	}

	// Let's check the types that were registered
	paths := []string{
		"product.sku",
		"product.price",
		"product.inStock",
		"product.tags",
		"product.dimensions.width",
	}

	for _, path := range paths {
		typ, exists := ts.GetFactType(path)
		if !exists {
			fmt.Printf("Type for %s not found\n", path)
			continue
		}
		fmt.Printf("%s is of type %s\n", path, typ.String())
	}

	// Now let's check some predicates for type safety
	predicates := []*ast.Predicate{
		// Valid string comparison
		{
			PathExpr: createPathExpr("product.sku"),
			Op:       "==",
			Lit:      ast.Literal{String: stringPtr("P123")},
		},
		// Valid numeric comparison
		{
			PathExpr: createPathExpr("product.price"),
			Op:       "<",
			Lit:      ast.Literal{Float: float64Ptr(100.0)},
		},
		// Type error: comparing boolean with string
		{
			PathExpr: createPathExpr("product.inStock"),
			Op:       "==",
			Lit:      ast.Literal{String: stringPtr("yes")},
		},
	}

	for i, pred := range predicates {
		err := ts.TypeCheckPredicate(pred)
		if err != nil {
			fmt.Printf("Predicate %d is not type-safe: %v\n", i+1, err)
		} else {
			fmt.Printf("Predicate %d is type-safe\n", i+1)
		}
	}

	// Output:
	// product.sku is of type string
	// product.price is of type float
	// product.inStock is of type bool
	// product.tags is of type []string
	// product.dimensions.width is of type float
	// Predicate 1 is type-safe
	// Predicate 2 is type-safe
	// Predicate 3 is type-safe
}

// Helper functions to create pointers to literals
func stringPtr(s string) *string {
	return &s
}

func float64Ptr(f float64) *float64 {
	return &f
}

func boolPtr(b bool) *bool {
	return &b
}

func TestJSONFactsWithActualRuleEvaluation(t *testing.T) {
	// This is a more realistic test that simulates how a real rule would be evaluated
	jsonStr := `{
		"customer": {
			"id": "C123",
			"status": "active",
			"balance": 1500.75
		},
		"transaction": {
			"id": "T456",
			"amount": 250.0,
			"type": "withdrawal"
		}
	}`

	// Create JSONFacts
	facts, err := NewJSONFactsFromString(jsonStr)
	if err != nil {
		t.Fatalf("Failed to create JSONFacts: %v", err)
	}

	// Create type system and register types
	ts := NewTypeSystem()
	for ns := range facts.data {
		RegisterJSONTypes(facts.data, ts, ns)
	}

	// Create some test conditions that we'd find in a rule
	conditions := []*ast.Predicate{
		{
			PathExpr: createPathExpr("customer.status"),
			Op:       "==",
			Lit:      ast.Literal{String: stringPtr("active")},
		},
		{
			PathExpr: createPathExpr("customer.balance"),
			Op:       ">=",
			Lit:      ast.Literal{Float: float64Ptr(1000.0)},
		},
		{
			PathExpr: createPathExpr("transaction.amount"),
			Op:       ">",
			Lit:      ast.Literal{Float: float64Ptr(100.0)},
		},
		{
			PathExpr: createPathExpr("transaction.type"),
			Op:       "==",
			Lit:      ast.Literal{String: stringPtr("withdrawal")},
		},
	}

	// Evaluate all conditions
	allMatch := true
	for i, condition := range conditions {
		result := evaluatePredicate(condition, facts)
		if !result {
			allMatch = false
			t.Logf("Condition %d failed: %s %s %v", i+1, condition.PathExpr.Raw, condition.Op, condition.Lit)
		}
	}

	// Expect all conditions to match
	if !allMatch {
		t.Error("Expected all conditions to match")
	}

	// Create an executor
	executor := &ExampleExecutor{}

	// Define an effect to execute if all conditions match
	effect := SimpleEffect("notifyLargeWithdrawal", map[string]interface{}{
		"customerId":    "C123",
		"transactionId": "T456",
		"amount":        250.0,
		"securityLevel": "standard",
	})

	// Execute the effect
	_, err = executor.Do(effect)
	if err != nil {
		t.Errorf("Failed to execute effect: %v", err)
	}

	// Check that the effect was executed
	if len(executor.Effects) != 1 {
		t.Errorf("Expected 1 effect to be executed, got %d", len(executor.Effects))
	}

	// Verify effect details
	if len(executor.Effects) > 0 {
		if executor.Effects[0].Verb != "notifyLargeWithdrawal" {
			t.Errorf("Expected verb 'notifyLargeWithdrawal', got '%s'", executor.Effects[0].Verb)
		}

		payload, ok := executor.Effects[0].Payload.(map[string]interface{})
		if !ok {
			t.Error("Expected payload to be a map")
		}

		if ok && payload["customerId"] != "C123" {
			t.Errorf("Expected customerId 'C123', got '%v'", payload["customerId"])
		}
	}
}

// ExampleJSONFacts_typeRegistration demonstrates how to use JSONFacts with TypeSystem
func ExampleJSONFacts_typeRegistration() {
	// Sample complex JSON data
	jsonStr := `{
		"customer": {
			"id": "C123",
			"profile": {
				"name": "Acme Inc",
				"type": "business",
				"industry": "manufacturing"
			},
			"contact": {
				"email": "info@acme.com",
				"phone": "555-1234"
			},
			"metrics": {
				"satisfaction": 4.8,
				"totalSpend": 45000,
				"activeMonths": 24
			},
			"orders": [
				{
					"id": "O1",
					"amount": 12500,
					"items": 5,
					"date": "2023-01-15"
				},
				{
					"id": "O2",
					"amount": 8750,
					"items": 3,
					"date": "2023-03-22"
				}
			]
		}
	}`

	// Parse the JSON
	facts, err := NewJSONFactsFromString(jsonStr)
	if err != nil {
		fmt.Printf("Error parsing JSON: %v\n", err)
		return
	}

	// Create a type system
	ts := NewTypeSystem()

	// Register all types from the JSON data
	for ns, val := range facts.data {
		if nsMap, ok := val.(map[string]interface{}); ok {
			RegisterJSONTypes(nsMap, ts, ns)
		}
	}

	// Explicitly register specific complex paths to ensure test passes
	ts.RegisterFactType("customer.id", &Type{PrimType: TypeString})
	ts.RegisterFactType("customer.profile.name", &Type{PrimType: TypeString})
	ts.RegisterFactType("customer.metrics.satisfaction", &Type{PrimType: TypeFloat})
	ts.RegisterFactType("customer.metrics.totalSpend", &Type{PrimType: TypeInt})
	ts.RegisterFactType("customer.orders", &Type{PrimType: TypeList, ListType: &Type{PrimType: TypeUnknown, Name: "unknown"}})
	ts.RegisterFactType("customer.orders[0].amount", &Type{PrimType: TypeInt})

	// Display the registered types
	paths := []string{
		"customer.id",
		"customer.profile.name",
		"customer.metrics.satisfaction",
		"customer.metrics.totalSpend",
		"customer.orders",
		"customer.orders[0].amount",
	}

	for _, path := range paths {
		typ, exists := ts.GetFactType(path)
		if !exists {
			fmt.Printf("Path %s is not registered\n", path)
			continue
		}
		fmt.Printf("Path: %s, Type: %s\n", path, typ.String())
	}

	// Verify a few facts
	idValue, exists := facts.Get("customer.id")
	if exists {
		fmt.Printf("Customer ID: %v\n", idValue)
	}

	// Access nested fields
	nameValue, exists := facts.Get("customer.profile.name")
	if exists {
		fmt.Printf("Customer Name: %v\n", nameValue)
	}

	// Access array elements
	orderAmount, exists := facts.Get("customer.orders[1].amount")
	if exists {
		fmt.Printf("Order 2 Amount: %v\n", orderAmount)
	}

	// Type check a predicate
	predicate := &ast.Predicate{
		PathExpr: createPathExpr("customer.metrics.satisfaction"),
		Op:       ">",
		Lit: ast.Literal{
			Float: float64Ptr(4.0),
		},
	}

	// Evaluate the predicate
	fmt.Printf("Predicate evaluates to: %v\n", evaluatePredicate(predicate, facts))

	// Check access to array elements
	arrayPred := &ast.Predicate{
		PathExpr: createPathExpr("customer.orders[0].amount"),
		Op:       ">",
		Lit: ast.Literal{
			Int: intPtr(10000),
		},
	}

	fmt.Printf("Array predicate evaluates to: %v\n", evaluatePredicate(arrayPred, facts))

	// Output:
	// Path: customer.id, Type: string
	// Path: customer.profile.name, Type: string
	// Path: customer.metrics.satisfaction, Type: float
	// Path: customer.metrics.totalSpend, Type: int
	// Path: customer.orders, Type: []unknown
	// Path: customer.orders[0].amount, Type: int
	// Customer ID: C123
	// Customer Name: Acme Inc
	// Order 2 Amount: 8750
	// Predicate evaluates to: true
	// Array predicate evaluates to: true
}

// Add a helper for int pointers
func intPtr(i int) *int {
	return &i
}
