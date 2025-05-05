package pathutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExprFacts_Get(t *testing.T) {
	// Create test data
	data := map[string]interface{}{
		"user": map[string]interface{}{
			"name":    "John Doe",
			"age":     30,
			"active":  true,
			"balance": 100.5,
			"tags":    []string{"customer", "premium"},
			"address": map[string]interface{}{
				"city":    "New York",
				"country": "USA",
			},
		},
		"items": []interface{}{
			map[string]interface{}{
				"id":    "item1",
				"price": 10.99,
			},
			map[string]interface{}{
				"id":    "item2",
				"price": 20.99,
			},
		},
		"config": map[string]interface{}{
			"features": map[string]interface{}{
				"darkMode": true,
				"cache":    false,
			},
		},
	}

	// Create ExprFacts provider
	facts := NewExprFacts(data)

	// Test cases
	testCases := []struct {
		name      string
		path      string
		wantValue interface{}
		wantFound bool
	}{
		{
			name:      "get root",
			path:      "",
			wantValue: data,
			wantFound: true,
		},
		{
			name:      "get direct field",
			path:      "user",
			wantValue: data["user"],
			wantFound: true,
		},
		{
			name:      "get nested field",
			path:      "user.name",
			wantValue: "John Doe",
			wantFound: true,
		},
		{
			name:      "get boolean field",
			path:      "user.active",
			wantValue: true,
			wantFound: true,
		},
		{
			name:      "get numeric field",
			path:      "user.age",
			wantValue: 30,
			wantFound: true,
		},
		{
			name:      "get float field",
			path:      "user.balance",
			wantValue: 100.5,
			wantFound: true,
		},
		{
			name:      "get array element",
			path:      "items[0].id",
			wantValue: "item1",
			wantFound: true,
		},
		{
			name:      "get array element via array access syntax",
			path:      "items[1].price",
			wantValue: 20.99,
			wantFound: true,
		},
		{
			name:      "get deeply nested field",
			path:      "user.address.city",
			wantValue: "New York",
			wantFound: true,
		},
		{
			name:      "field not found",
			path:      "user.phone",
			wantValue: nil,
			wantFound: false,
		},
		{
			name:      "complex array access",
			path:      "user.tags[1]",
			wantValue: "premium",
			wantFound: true,
		},
		{
			name:      "map with dots in field name",
			path:      "config.features.darkMode",
			wantValue: true,
			wantFound: true,
		},
	}

	// Run tests
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotValue, gotFound := facts.Get(tc.path)
			assert.Equal(t, tc.wantFound, gotFound)
			if tc.wantFound {
				assert.Equal(t, tc.wantValue, gotValue)
			}
		})
	}
}

func TestExprFacts_EvaluateExpr(t *testing.T) {
	// Create test data
	data := map[string]interface{}{
		"user": map[string]interface{}{
			"name":   "John Doe",
			"age":    30,
			"active": true,
		},
		"items": []interface{}{
			map[string]interface{}{
				"id":    "item1",
				"price": 10.99,
			},
			map[string]interface{}{
				"id":    "item2",
				"price": 20.99,
			},
		},
		"threshold": 20,
	}

	// Create ExprFacts provider
	facts := NewExprFacts(data)

	// Test cases
	testCases := []struct {
		name      string
		expr      string
		wantValue interface{}
		wantErr   bool
	}{
		{
			name:      "simple field access",
			expr:      "user.name",
			wantValue: "John Doe",
			wantErr:   false,
		},
		{
			name:      "comparison",
			expr:      "user.age > 25",
			wantValue: true,
			wantErr:   false,
		},
		{
			name:      "boolean expression",
			expr:      "user.active && user.age < 40",
			wantValue: true,
			wantErr:   false,
		},
		{
			name:      "array access with condition",
			expr:      "items[0].price < threshold",
			wantValue: true,
			wantErr:   false,
		},
		{
			name:      "arithmetic operation",
			expr:      "items[0].price + items[1].price",
			wantValue: 31.98,
			wantErr:   false,
		},
		{
			name:      "array length",
			expr:      "len(items)",
			wantValue: 2,
			wantErr:   false,
		},
		{
			name:      "invalid expr",
			expr:      "unknown.field",
			wantValue: nil,
			wantErr:   true,
		},
	}

	// Run tests
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotValue, err := facts.EvaluateExpr(tc.expr)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)

				// For floating point values, use InDelta instead of Equal
				if tc.name == "arithmetic operation" {
					assert.InDelta(t, tc.wantValue, gotValue, 0.001)
				} else {
					assert.Equal(t, tc.wantValue, gotValue)
				}
			}
		})
	}
}

func TestStructFactRegistry(t *testing.T) {
	// Create registry
	registry := NewStructFactRegistry()

	// Register data
	type User struct {
		Name   string
		Age    int
		Active bool
	}

	type Item struct {
		ID    string
		Price float64
	}

	// Register structs
	registry.Register("user", User{
		Name:   "John Doe",
		Age:    30,
		Active: true,
	})

	registry.Register("items", []Item{
		{ID: "item1", Price: 10.99},
		{ID: "item2", Price: 20.99},
	})

	// Test simple field access
	value, found := registry.Get("user.Name")
	assert.True(t, found)
	assert.Equal(t, "John Doe", value)

	// Test array access
	value, found = registry.Get("items[0].ID")
	assert.True(t, found)
	assert.Equal(t, "item1", value)

	// Test non-existent field
	_, found = registry.Get("user.Email")
	assert.False(t, found)

	// Test with context
	value, result := registry.GetWithContext("user.Age")
	assert.True(t, result.Exists)
	assert.Equal(t, 30, value)
}

func TestJSONToFactsAdapter(t *testing.T) {
	// Create registry and adapter
	registry := NewStructFactRegistry()
	adapter := NewJSONToFactsAdapter(registry)

	// Sample JSON
	jsonData := []byte(`{
		"name": "John Doe",
		"age": 30,
		"address": {
			"city": "New York",
			"country": "USA"
		},
		"tags": ["customer", "premium"]
	}`)

	// Register JSON
	err := adapter.RegisterJSON("user", jsonData)
	assert.NoError(t, err)

	// Test field access
	value, found := registry.Get("user.name")
	assert.True(t, found)
	assert.Equal(t, "John Doe", value)

	// Test nested field
	value, found = registry.Get("user.address.city")
	assert.True(t, found)
	assert.Equal(t, "New York", value)

	// Test array
	value, found = registry.Get("user.tags[0]")
	assert.True(t, found)
	assert.Equal(t, "customer", value)
}

// Define a custom struct for JSON test
type Customer struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Email   string   `json:"email"`
	Active  bool     `json:"active"`
	Tags    []string `json:"tags"`
	Address struct {
		Street  string `json:"street"`
		City    string `json:"city"`
		Country string `json:"country"`
	} `json:"address"`
}

func TestDirectStructAccess(t *testing.T) {
	// Create a customer struct directly
	customer := Customer{
		ID:     "cust123",
		Name:   "Jane Smith",
		Email:  "jane@example.com",
		Active: true,
		Tags:   []string{"vip", "preferred"},
	}
	customer.Address.Street = "123 Main St"
	customer.Address.City = "Boston"
	customer.Address.Country = "USA"

	// Create registry and register the struct directly
	registry := NewStructFactRegistry()
	registry.Register("customer", customer)

	// Test field access through registry
	value, found := registry.Get("customer.Name")
	assert.True(t, found)
	assert.Equal(t, "Jane Smith", value)

	// Test nested field
	value, found = registry.Get("customer.Address.City")
	assert.True(t, found)
	assert.Equal(t, "Boston", value)

	// Test array access
	value, found = registry.Get("customer.Tags[0]")
	assert.True(t, found)
	assert.Equal(t, "vip", value)
}

func TestFactProviderFromStruct(t *testing.T) {
	// Create a struct directly
	type Order struct {
		ID     string
		Amount float64
		Items  []struct {
			SKU   string
			Price float64
			Qty   int
		}
	}

	order := Order{
		ID:     "order123",
		Amount: 99.95,
		Items: []struct {
			SKU   string
			Price float64
			Qty   int
		}{
			{SKU: "ABC123", Price: 49.95, Qty: 1},
			{SKU: "XYZ789", Price: 25.00, Qty: 2},
		},
	}

	// Create a fact provider from the struct
	provider, err := NewFactProviderFromStruct(order)
	assert.NoError(t, err)

	// Test field access
	value, found := provider.Get("data.ID")
	assert.True(t, found)
	assert.Equal(t, "order123", value)

	// Test nested array access
	value, found = provider.Get("data.Items[0].SKU")
	assert.True(t, found)
	assert.Equal(t, "ABC123", value)

	// Test calculation
	value, result := provider.GetWithContext("data.Items[1].Price * data.Items[1].Qty")
	assert.True(t, result.Exists)
	assert.InDelta(t, 50.0, value, 0.001)
}
