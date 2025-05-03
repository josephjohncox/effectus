package tests

import (
	"strings"
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
		path := pathutil.Path(pathStr)

		// Check that converting back to string gives a valid path
		backToString := path.String()
		if backToString == "" {
			t.Errorf("Path string representation is empty for %s", pathStr)
		}

		// Parse the string representation again
		pathAgain := pathutil.Path(backToString)

		// Verify they're equal
		if path.String() != pathAgain.String() {
			t.Errorf("Path string mismatch: %s vs %s", path.String(), pathAgain.String())
		}
	}

	// Test path with namespace
	complexPath := pathutil.Path("customer.orders[0].items[2].attributes")

	if complexPath.Namespace() != "customer" {
		t.Errorf("Expected namespace 'customer', got '%s'", complexPath.Namespace())
	}

	// Test path with dots and brackets
	pathStr := string(complexPath)
	if !strings.Contains(pathStr, "orders[0]") && !strings.Contains(pathStr, "items[2]") {
		t.Errorf("Path should contain array indices: %s", pathStr)
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
	customerProvider := pathutil.NewGjsonProvider(customerData)
	registry.Register("customer", customerProvider)

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
			// Use the registry to resolve the path
			gotValue, exists := registry.Get(tt.path)

			if exists != tt.wantOk {
				t.Errorf("Path exists = %v, want %v", exists, tt.wantOk)
				return
			}

			if exists && gotValue != tt.wantValue {
				t.Errorf("Value = %v, want %v", gotValue, tt.wantValue)
			}
		})
	}
}
