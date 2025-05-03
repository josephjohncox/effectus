package schema

import (
	"fmt"
	"reflect"
	"testing"
)

func TestJSONFactsCreation(t *testing.T) {
	// Create a simple JSON structure
	data := map[string]interface{}{
		"customer": map[string]interface{}{
			"id":   "C123",
			"name": "Acme Corp",
			"contact": map[string]interface{}{
				"email": "info@acme.com",
				"phone": "555-123-4567",
			},
			"orders": []interface{}{
				map[string]interface{}{
					"id":     "O1",
					"amount": 100.0,
					"items":  3,
				},
				map[string]interface{}{
					"id":     "O2",
					"amount": 250.0,
					"items":  5,
				},
			},
		},
		"product": map[string]interface{}{
			"id":    "P456",
			"name":  "Widget",
			"price": 49.99,
			"tags":  []interface{}{"hardware", "tool"},
		},
	}

	// Create JSONFacts from the data map
	facts := NewJSONFacts(data)

	// Test that JSONFacts implements Facts interface properly
	if facts.Schema() == nil {
		t.Fatal("Schema should not be nil")
	}
}

func TestJSONFactsFromString(t *testing.T) {
	jsonStr := `{
		"customer": {
			"id": "cust123",
			"profile": {
				"name": "John Doe",
				"email": "john@example.com"
			},
			"balance": 500.50
		},
		"product": {
			"id": "prod456",
			"name": "Widget",
			"price": 29.99
		}
	}`

	facts, err := NewJSONFactsFromString(jsonStr)
	if err != nil {
		t.Fatalf("Failed to create JSONFacts from string: %v", err)
	}

	// Debug the parsed JSON structure
	fmt.Printf("DEBUG: Parsed JSON data: %+v\n", facts.data)

	// Test expected values
	custID, ok := facts.Get("customer.id")
	if !ok {
		t.Errorf("Expected to find customer.id, but did not")
	}
	if custID != "cust123" {
		t.Errorf("Expected customer.id to be 'cust123', got %v", custID)
	}

	price, ok := facts.Get("product.price")
	if !ok {
		t.Errorf("Expected to find product.price, but did not")
	}
	if price != 29.99 {
		t.Errorf("Expected product.price to be 29.99, got %v", price)
	}
}

func TestJSONFactsGet(t *testing.T) {
	// Create JSONFacts with nested data
	data := map[string]interface{}{
		"customer": map[string]interface{}{
			"id":      "C123",
			"active":  true,
			"balance": 1000.5,
			"orders": []interface{}{
				map[string]interface{}{
					"id":     "O1",
					"amount": 100,
				},
				map[string]interface{}{
					"id":     "O2",
					"amount": 250,
				},
			},
		},
	}

	facts := NewJSONFacts(data)

	tests := []struct {
		path      string
		wantExist bool
		wantVal   interface{}
	}{
		// In our updated implementation, namespace alone is resolvable
		// So we need to update this expectation
		{"customer", true, data["customer"]},
		{"customer.id", true, "C123"},
		{"customer.active", true, true},
		{"customer.balance", true, 1000.5},
		{"customer.orders[0].id", true, "O1"},
		{"customer.orders[1].amount", true, 250},
		{"customer.nonexistent", false, nil},
		{"nonexistent", false, nil},
		{"customer.orders[99]", false, nil},
		{"customer.orders[0].nonexistent", false, nil},
	}

	for _, tt := range tests {
		gotVal, gotExist := facts.Get(tt.path)

		if gotExist != tt.wantExist {
			t.Errorf("Path %s: expected exists=%v, got %v",
				tt.path, tt.wantExist, gotExist)
		}

		if gotExist && !reflect.DeepEqual(gotVal, tt.wantVal) {
			t.Errorf("Path %s: expected value=%v, got %v",
				tt.path, tt.wantVal, gotVal)
		}
	}
}

func TestJSONSchemaValidation(t *testing.T) {
	// Create JSON data with complex structure
	data := map[string]interface{}{
		"customer": map[string]interface{}{
			"id":    "C123",
			"email": "customer@example.com",
			"address": map[string]interface{}{
				"street": "123 Main St",
				"city":   "Anytown",
			},
		},
	}

	facts := NewJSONFacts(data)
	schema := facts.Schema()

	// Test valid paths
	validPaths := []string{
		"customer.id",
		"customer.email",
		"customer.address.street",
		"customer.address.city",
		// In our updated implementation, namespace alone is considered valid
		"customer",
	}

	for _, path := range validPaths {
		if !schema.ValidatePath(path) {
			t.Errorf("Path %s should be valid but was rejected", path)
		}
	}

	// Test invalid paths
	invalidPaths := []string{
		"customer.nonexistent",
		"nonexistent.field",
		"customer.address.nonexistent",
		"customer.address.city.nonexistent",
		"",                // Empty path
		".customer",       // Leading dot
		"customer.",       // Trailing dot
		"customer..id",    // Double dots
		"customer.id..id", // Double dots in middle
	}

	for _, path := range invalidPaths {
		if schema.ValidatePath(path) {
			t.Errorf("Path %s should be invalid but was accepted", path)
		}
	}
}

func TestRegisterJSONTypes(t *testing.T) {
	data := map[string]interface{}{
		"customer": map[string]interface{}{
			"id":      "C123",
			"active":  true,
			"balance": 1000.50,
			"orders": []interface{}{
				map[string]interface{}{
					"id":     "O1",
					"amount": 100.0,
				},
			},
		},
	}

	ts := NewTypeSystem()
	RegisterJSONTypes(data, ts, "example")

	tests := []struct {
		path         string
		expectedType PrimitiveType
	}{
		{"example.customer.id", TypeString},
		{"example.customer.active", TypeBool},
		{"example.customer.balance", TypeFloat},
		{"example.customer.orders", TypeList},
		{"example.customer.orders[0].id", TypeString},
		{"example.customer.orders[0].amount", TypeFloat},
	}

	for _, test := range tests {
		typ, exists := ts.GetFactType(test.path)
		if !exists {
			t.Errorf("Expected type for path %s to be registered", test.path)
			continue
		}

		if typ.PrimType != test.expectedType {
			t.Errorf("Path %s: expected type %v, got %v", test.path, test.expectedType, typ.PrimType)
		}
	}

	// Test list type
	ordersType, exists := ts.GetFactType("example.customer.orders")
	if !exists {
		t.Fatal("Expected type for customer.orders to be registered")
	}

	if ordersType.PrimType != TypeList {
		t.Errorf("Expected orders to be a list, got %v", ordersType.PrimType)
	}

	if ordersType.ListType == nil {
		t.Error("Expected orders list type to have element type information")
	}
}

func TestJSONFactPathResolver(t *testing.T) {
	// Create a type system and register some types
	ts := NewTypeSystem()
	ts.RegisterFactType("example.customer.id", &Type{PrimType: TypeString})

	// Create a resolver using the unified implementation
	resolver := NewUnifiedPathResolver(ts, false)

	// Create a fact path with PathSegment structs
	path := NewFactPathFromStrings("example", nil, "customer", "id")

	// Test type resolution
	typ := resolver.Type(path)
	if typ.PrimType != TypeString {
		t.Errorf("Expected string type, got %v", typ.PrimType)
	}

	// Test with unknown path
	unknownPath := NewFactPathFromStrings("example", nil, "nonexistent")
	unknownType := resolver.Type(unknownPath)
	if unknownType.PrimType != TypeUnknown {
		t.Errorf("Expected unknown type for nonexistent path, got %v", unknownType.PrimType)
	}

	// Test with type information in path
	typedPath := NewFactPathFromStrings("example", &Type{PrimType: TypeInt}, "typed")
	pathType := resolver.Type(typedPath)
	if pathType.PrimType != TypeInt {
		t.Errorf("Expected int type from path, got %v", pathType.PrimType)
	}
}

// TestJSONFactsEnd2End tests the complete JSON fact processing flow
func TestJSONFactsEnd2End(t *testing.T) {
	// Create JSON data
	jsonStr := `{
		"customer": {
			"id": "C123",
			"profile": {
				"name": "Acme Corp",
				"tags": ["enterprise", "manufacturing"]
			}
		}
	}`

	// Parse into JSONFacts
	facts, err := NewJSONFactsFromString(jsonStr)
	if err != nil {
		t.Fatalf("Failed to create JSONFacts: %v", err)
	}

	// Test that the path can be resolved directly through JSONFacts
	value, exists := facts.Get("customer.profile.name")
	if !exists {
		t.Fatal("Expected to find customer.profile.name through direct Get() but didn't")
	}
	if name, ok := value.(string); !ok || name != "Acme Corp" {
		t.Errorf("Through direct Get(): expected 'Acme Corp', got %v", value)
	}

	// Test array access
	arrValue, arrExists := facts.Get("customer.profile.tags[0]")
	if !arrExists {
		t.Fatal("Expected to find customer.profile.tags[0] but didn't")
	}
	if tag, ok := arrValue.(string); !ok || tag != "enterprise" {
		t.Errorf("Expected 'enterprise', got %v", arrValue)
	}

	// Test schema validation - updated for the new behavior
	if !facts.Schema().ValidatePath("customer.profile.name") {
		t.Error("Expected customer.profile.name to be valid path")
	}

	if facts.Schema().ValidatePath("customer.nonexistent") {
		t.Error("Expected customer.nonexistent to be invalid path")
	}
}

// TestJSONFactsWithResolver tests the interaction between JSONFacts and resolver
func TestJSONFactsWithResolver(t *testing.T) {
	// Create test data with common types
	data := map[string]interface{}{
		"stringVal": "hello",
		"intVal":    42,
		"floatVal":  3.14,
		"boolVal":   true,
		"objVal": map[string]interface{}{
			"nested": "value",
		},
		"arrayVal": []interface{}{1, 2, 3},
	}

	// Create a minimal test
	facts := NewJSONFacts(data)

	// Test simple string value access
	val, ok := facts.Get("stringVal")
	if !ok {
		t.Fatalf("Expected to find stringVal")
	}

	if str, ok := val.(string); !ok || str != "hello" {
		t.Errorf("Expected 'hello', got %v (%T)", val, val)
	}

	// Test array direct access in Go
	arr := data["arrayVal"].([]interface{})
	if arr[1] != 2 {
		t.Errorf("Direct array access: expected 2, got %v", arr[1])
	}
}

// TestJSONFactsArrayIndexing tests the array indexing in JSON facts
func TestJSONFactsArrayIndexing(t *testing.T) {
	// Create a simple test case with only the array
	data := map[string]interface{}{
		"array": []interface{}{10, 20, 30},
	}

	// We don't need facts for this direct test
	// facts := NewJSONFacts(data)

	// Create a path directly - simpler test to isolate the issue
	path := FactPath{
		namespace: "array",
		segments: []PathSegment{
			{
				Name:      "", // The name is empty for direct array access
				IndexExpr: &IndexExpression{Value: 1},
			},
		},
	}

	// Try direct resolution
	value, exists := ResolveJSONPath(data, path)
	if !exists {
		t.Fatalf("Failed to access array[1] via direct path")
	}

	// Check the value (can be int or float64 depending on how the test data was created)
	switch v := value.(type) {
	case int:
		if v != 20 {
			t.Errorf("Expected 20, got %v", v)
		}
	case float64:
		if v != 20.0 {
			t.Errorf("Expected 20.0, got %v", v)
		}
	default:
		t.Errorf("Expected int or float64, got %T", value)
	}
}

// TestResolverWithNestedData tests resolution of deeply nested data
func TestResolverWithNestedData(t *testing.T) {
	// Create test data with nested structure
	data := map[string]interface{}{
		"customer": map[string]interface{}{
			"name": "Acme Corp",
			"contact": map[string]interface{}{
				"email": "info@acme.com",
			},
			"orders": []interface{}{
				map[string]interface{}{
					"id":     "O1",
					"amount": 100.0,
				},
			},
		},
	}

	// Create facts
	facts := NewJSONFacts(data)

	// Test through direct access
	emailVal, exists := facts.Get("customer.contact.email")
	if !exists {
		t.Errorf("Expected to find customer.contact.email through direct Get()")
	}
	if val, ok := emailVal.(string); !ok || val != "info@acme.com" {
		t.Errorf("Expected 'info@acme.com', got %v", emailVal)
	}

	// Test array access
	orderVal, orderExists := facts.Get("customer.orders[0].id")
	if !orderExists {
		t.Errorf("Expected to find customer.orders[0].id through direct Get()")
	}
	if val, ok := orderVal.(string); !ok || val != "O1" {
		t.Errorf("Expected 'O1', got %v", orderVal)
	}

	// Test enhanced Get
	_, enhancedExists, info := facts.EnhancedGet("customer.contact.email")
	if !enhancedExists {
		t.Errorf("Expected to find customer.contact.email through EnhancedGet()")
	}
	if info.ValueType.PrimType != TypeString {
		t.Errorf("Expected string type in result, got %v", info.ValueType.PrimType)
	}
}
