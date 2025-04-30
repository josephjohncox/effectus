package schema

import (
	"fmt"
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
	// Create test data with different types and structures
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
				map[string]interface{}{
					"id":     "O2",
					"amount": 250.0,
				},
			},
		},
	}

	facts := NewJSONFacts(data)

	tests := []struct {
		path          string
		expectedValue interface{}
		shouldExist   bool
	}{
		{"customer.id", "C123", true},
		{"customer.active", true, true},
		{"customer.balance", 1000.50, true},
		{"customer.orders[0].id", "O1", true},
		{"customer.orders[1].amount", 250.0, true},
		{"customer.nonexistent", nil, false},
		{"nonexistent.field", nil, false},
		{"customer.orders[5]", nil, false}, // Out of bounds index
		{"", nil, false},                   // Empty path
		{"customer", nil, false},           // Just namespace
	}

	for _, test := range tests {
		value, exists := facts.Get(test.path)
		if exists != test.shouldExist {
			t.Errorf("Path %s: expected exists=%v, got %v", test.path, test.shouldExist, exists)
		}

		if exists && value != test.expectedValue {
			t.Errorf("Path %s: expected value=%v, got %v", test.path, test.expectedValue, value)
		}
	}
}

func TestJSONSchemaValidation(t *testing.T) {
	data := map[string]interface{}{
		"customer": map[string]interface{}{
			"id": "C123",
			"orders": []interface{}{
				map[string]interface{}{"id": "O1"},
				map[string]interface{}{"id": "O2"},
			},
		},
	}

	schema := &JSONSchema{data: data}

	validPaths := []string{
		"customer.id",
		"customer.orders[0].id",
		"customer.orders[1].id",
	}

	invalidPaths := []string{
		"",                      // Empty
		"customer",              // Just namespace
		"product.id",            // Nonexistent namespace
		"customer.nonexistent",  // Nonexistent field
		"customer.orders[5].id", // Out of bounds
		"customer..id",          // Double dot
		".customer.id",          // Starts with dot
		"customer.id.",          // Ends with dot
	}

	for _, path := range validPaths {
		if !schema.ValidatePath(path) {
			t.Errorf("Path %s should be valid but was rejected", path)
		}
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

	// Create a resolver
	resolver := CreateJSONFactPathResolver(ts)

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

	// Create a type system
	ts := NewTypeSystem()

	// Register types from the facts
	for ns := range facts.data {
		if nsData, ok := facts.data[ns].(map[string]interface{}); ok {
			RegisterJSONTypes(nsData, ts, ns)
		}
	}

	// Register string type for the path we're testing
	ts.RegisterFactType("customer.profile.name", &Type{PrimType: TypeString})

	// Create a fact path resolver
	resolver := CreateJSONFactPathResolver(ts)

	// Test fact retrieval through path resolution
	path := NewFactPathFromStrings("customer", nil, "profile", "name")
	value, exists := resolver.Resolve(facts, path)

	if !exists {
		t.Fatal("Expected to find customer.profile.name but didn't")
	}

	if name, ok := value.(string); !ok || name != "Acme Corp" {
		t.Errorf("Expected 'Acme Corp', got %v", value)
	}

	// Test array access
	// Create a path with array index
	arrPath, _ := ParseFactPath("customer.profile.tags[0]")
	arrValue, arrExists := facts.Get(arrPath.String())

	if !arrExists {
		t.Fatal("Expected to find customer.profile.tags[0] but didn't")
	}

	if tag, ok := arrValue.(string); !ok || tag != "enterprise" {
		t.Errorf("Expected 'enterprise', got %v", arrValue)
	}

	// Test type checking
	nameType := resolver.Type(path)
	if nameType.PrimType != TypeString {
		t.Errorf("Expected name to be string type, got %v", nameType.PrimType)
	}

	// Test schema validation
	if !facts.Schema().ValidatePath("customer.profile.name") {
		t.Error("Expected customer.profile.name to be valid path")
	}

	if facts.Schema().ValidatePath("customer.nonexistent") {
		t.Error("Expected customer.nonexistent to be invalid path")
	}
}
