package schema

import (
	"testing"
)

func TestEnhancedPathResolution(t *testing.T) {
	// Create test data with complex nested structure
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
					"items": []interface{}{
						map[string]interface{}{
							"sku":      "SKU001",
							"quantity": 2,
							"price":    49.99,
						},
						map[string]interface{}{
							"sku":      "SKU002",
							"quantity": 1,
							"price":    29.99,
						},
					},
				},
				map[string]interface{}{
					"id":     "O2",
					"amount": 250.0,
					"items": []interface{}{
						map[string]interface{}{
							"sku":      "SKU003",
							"quantity": 5,
							"price":    50.0,
						},
					},
				},
			},
			"attributes": map[string]interface{}{
				"type":        "enterprise",
				"sector":      "manufacturing",
				"credit_tier": "A",
			},
		},
	}

	// Create a JSONFacts object
	facts := NewJSONFacts(data)

	// Test different paths
	tests := []struct {
		name      string
		path      string
		wantValue interface{}
		wantOk    bool
	}{
		{
			name:      "Simple path",
			path:      "customer.name",
			wantValue: "Acme Corp",
			wantOk:    true,
		},
		{
			name:      "Nested path",
			path:      "customer.contact.email",
			wantValue: "info@acme.com",
			wantOk:    true,
		},
		{
			name:      "Array indexing",
			path:      "customer.orders[0].id",
			wantValue: "O1",
			wantOk:    true,
		},
		{
			name:      "Nested array",
			path:      "customer.orders[0].items[1].sku",
			wantValue: "SKU002",
			wantOk:    true,
		},
		{
			name:      "Map field access",
			path:      "customer.attributes.type",
			wantValue: "enterprise",
			wantOk:    true,
		},
		{
			name:      "Out of bounds index",
			path:      "customer.orders[5].id",
			wantValue: nil,
			wantOk:    false,
		},
		{
			name:      "Missing field",
			path:      "customer.nonexistent",
			wantValue: nil,
			wantOk:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotValue, gotOk := facts.Get(tt.path)
			if gotOk != tt.wantOk {
				t.Errorf("JSONFacts.Get() ok = %v, want %v", gotOk, tt.wantOk)
				return
			}
			if gotOk && gotValue != tt.wantValue {
				t.Errorf("JSONFacts.Get() value = %v, want %v", gotValue, tt.wantValue)
			}
		})
	}
}

func TestEnhancedGetWithContext(t *testing.T) {
	// Create test data - make sure the structure matches what we're testing
	data := map[string]interface{}{
		"orders": map[string]interface{}{
			"items": []interface{}{
				map[string]interface{}{
					"id":     "O1",
					"status": "shipped",
				},
			},
		},
	}

	facts := NewJSONFacts(data)

	// Test with enhanced context for non-existent path
	value, exists, info := facts.EnhancedGet("orders.items[0].nonexistent")

	if exists {
		t.Errorf("Expected path to not exist")
	}

	if value != nil {
		t.Errorf("Expected nil value, got %v", value)
	}

	if info.Error == nil {
		t.Errorf("Expected error in PathResolutionInfo")
	}

	// Test successful resolution with type information
	value, exists, info = facts.EnhancedGet("orders.items[0].status")

	if !exists {
		t.Fatalf("Expected path to exist, error: %v", info.Error)
	}

	if value != "shipped" {
		t.Errorf("Expected 'shipped', got %v", value)
	}

	if info.Error != nil {
		t.Errorf("Expected no error, got %v", info.Error)
	}

	if info.ValueType == nil {
		t.Errorf("Expected ValueType to be set")
	} else if info.ValueType.PrimType != TypeString {
		t.Errorf("Expected string type, got %v", info.ValueType.PrimType)
	}
}

func TestPathCache(t *testing.T) {
	cache := NewPathCache()

	// Parse a path and cache it
	path1, err := cache.Get("customer.orders[0].items")
	if err != nil {
		t.Fatalf("Failed to parse path: %v", err)
	}

	// Get the same path from cache
	path2, err := cache.Get("customer.orders[0].items")
	if err != nil {
		t.Fatalf("Failed to get path from cache: %v", err)
	}

	// Verify paths are equal
	if path1.String() != path2.String() {
		t.Errorf("Paths don't match: %s vs %s", path1.String(), path2.String())
	}

	// Test path with complex elements
	complexPath, err := cache.Get("customer.orders[0].items[2].attributes")
	if err != nil {
		t.Fatalf("Failed to parse complex path: %v", err)
	}

	if complexPath.Namespace() != "customer" {
		t.Errorf("Expected namespace 'customer', got '%s'", complexPath.Namespace())
	}

	// The expected number should match the actual number of segments
	expectedSegments := 3 // orders[0], items[2], attributes
	if len(complexPath.Segments()) != expectedSegments {
		t.Errorf("Expected %d segments, got %d", expectedSegments, len(complexPath.Segments()))
	}

	// Verify index in segment
	ordersSeg := complexPath.Segments()[0]
	if idx, hasIdx := ordersSeg.GetIndex(); !hasIdx || *idx != 0 {
		t.Errorf("Expected orders segment to have index 0")
	}
}
