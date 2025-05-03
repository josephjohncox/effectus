package tests

import (
	"testing"

	"github.com/effectus/effectus-go/pathutil"
)

func TestPathParsing(t *testing.T) {
	// Test parsing paths
	paths := []string{
		"customer.orders[0].items",
		"customer.orders[0].items[2].attributes",
	}

	for _, pathStr := range paths {
		path, err := pathutil.ParseString(pathStr)
		if err != nil {
			t.Fatalf("Failed to parse path %s: %v", pathStr, err)
		}

		// Check that converting back to string gives a valid path
		backToString := path.String()
		if backToString == "" {
			t.Errorf("Path string representation is empty for %s", pathStr)
		}

		// Parse the string representation again
		pathAgain, err := pathutil.ParseString(backToString)
		if err != nil {
			t.Fatalf("Failed to parse path string %s: %v", backToString, err)
		}

		// Verify they're equal
		if path.String() != pathAgain.String() {
			t.Errorf("Path string mismatch: %s vs %s", path.String(), pathAgain.String())
		}
	}

	// Test path with complex elements
	complexPath, err := pathutil.ParseString("customer.orders[0].items[2].attributes")
	if err != nil {
		t.Fatalf("Failed to parse complex path: %v", err)
	}

	if complexPath.Namespace != "customer" {
		t.Errorf("Expected namespace 'customer', got '%s'", complexPath.Namespace)
	}

	elements := complexPath.Elements
	if len(elements) != 3 { // 3 elements: orders[0], items[2], attributes
		t.Errorf("Expected 3 elements, got %d", len(elements))
	}

	// Check the first element
	orderElem := elements[0]
	if orderElem.Name != "orders" {
		t.Errorf("Expected element name 'orders', got '%s'", orderElem.Name)
	}

	if !orderElem.HasIndex() {
		t.Errorf("Expected orders element to have an index")
	}

	idx, _ := orderElem.GetIndex()
	if idx != 0 {
		t.Errorf("Expected index 0, got %d", idx)
	}
}

func TestPathResolution(t *testing.T) {
	// Create test data for the customer namespace
	customerData := map[string]interface{}{
		"name": "Acme Corp",
		"contact": map[string]interface{}{
			"email": "info@acme.com",
		},
		"orders": []interface{}{
			map[string]interface{}{
				"id": "O1",
				"items": []interface{}{
					map[string]interface{}{
						"sku": "SKU001",
					},
					map[string]interface{}{
						"sku": "SKU002",
					},
				},
			},
		},
		"attributes": map[string]interface{}{
			"type": "enterprise",
		},
	}

	// Create a registry to handle namespaces
	registry := pathutil.NewRegistry()

	// Create a memory provider for customer namespace
	customerProvider := pathutil.NewMemoryProvider(customerData)
	registry.Register("customer", customerProvider)

	// Create a resolver
	resolver := pathutil.NewPathResolver(false)

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
			// Parse the path
			path, err := pathutil.ParseString(tt.path)
			if err != nil {
				t.Fatalf("Failed to parse path: %v", err)
			}

			// Resolve the path
			gotValue, gotResult := resolver.ResolveWithContext(registry, path)
			gotOk := gotResult.Exists && gotResult.Error == nil

			if gotOk != tt.wantOk {
				if gotResult.Error != nil {
					t.Errorf("Resolution error: %v", gotResult.Error)
				}
				t.Errorf("Path exists = %v, want %v", gotOk, tt.wantOk)
				return
			}

			if gotOk && gotValue != tt.wantValue {
				t.Errorf("Value = %v, want %v", gotValue, tt.wantValue)
			}
		})
	}
}
