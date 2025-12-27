package pathutil

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestJSONLoader(t *testing.T) {
	// Test JSON data
	jsonData := []byte(`{
		"customer": {
			"name": "John Doe",
			"age": 30,
			"active": true,
			"orders": [
				{"id": "order1", "amount": 99.99},
				{"id": "order2", "amount": 149.95}
			],
			"metadata": {
				"tags": ["premium", "loyal"],
				"created_at": "2020-01-01T12:00:00Z"
			}
		}
	}`)

	// Create loader
	loader := NewJSONLoader(jsonData)

	// Load into provider
	facts, err := loader.Load()
	assert.NoError(t, err)
	assert.NotNil(t, facts)

	// Test simple path access
	value, exists := facts.Get("customer.name")
	assert.True(t, exists)
	assert.Equal(t, "John Doe", value)

	// Test array access
	value, exists = facts.Get("customer.orders[0].id")
	assert.True(t, exists)
	assert.Equal(t, "order1", value)

	// Test nested object
	value, exists = facts.Get("customer.metadata.tags[1]")
	assert.True(t, exists)
	assert.Equal(t, "loyal", value)

	// Test type information
	assertType(t, facts, "customer.name", "string")
	assertType(t, facts, "customer.orders[0].amount", "float")
	assertType(t, facts, "customer.active", "boolean")
	assertType(t, facts, "customer.metadata.tags", "array")
}

func TestProtoLoader(t *testing.T) {
	// Create a proto struct message
	customerMap, err := structpb.NewStruct(map[string]interface{}{
		"name":   "John Doe",
		"age":    int64(30),
		"active": true,
		"orders": []interface{}{
			map[string]interface{}{
				"id":     "order1",
				"amount": float64(99.99),
			},
			map[string]interface{}{
				"id":     "order2",
				"amount": float64(149.95),
			},
		},
	})
	assert.NoError(t, err)

	// Create loader
	loader := NewProtoLoader(customerMap)

	// Load into provider
	facts, err := loader.Load()
	assert.NoError(t, err)
	assert.NotNil(t, facts)

	// Test simple path access
	value, exists := facts.Get("name")
	assert.True(t, exists)
	assert.Equal(t, "John Doe", value)

	// Test array access
	value, exists = facts.Get("orders[0].id")
	assert.True(t, exists)
	assert.Equal(t, "order1", value)

	// Test type information
	assertType(t, facts, "name", "string")
	assertType(t, facts, "orders[0].amount", "float")
	assertType(t, facts, "active", "boolean")
}

func TestStructLoader(t *testing.T) {
	// Test struct
	type Order struct {
		ID     string  `json:"id"`
		Amount float64 `json:"amount"`
	}

	type Customer struct {
		Name   string  `json:"name"`
		Age    int     `json:"age"`
		Active bool    `json:"active"`
		Orders []Order `json:"orders"`
		Tags   []string
	}

	// Create test data
	customer := Customer{
		Name:   "John Doe",
		Age:    30,
		Active: true,
		Orders: []Order{
			{ID: "order1", Amount: 99.99},
			{ID: "order2", Amount: 149.95},
		},
		Tags: []string{"premium", "loyal"},
	}

	// Create loader
	loader := NewStructLoader(customer)

	// Load into provider
	facts, err := loader.Load()
	assert.NoError(t, err)
	assert.NotNil(t, facts)

	// Test simple field access
	value, exists := facts.Get("name")
	assert.True(t, exists)
	assert.Equal(t, "John Doe", value)

	// Test nested struct access
	value, exists = facts.Get("orders[0].id")
	assert.True(t, exists)
	assert.Equal(t, "order1", value)

	// Test type information
	assertType(t, facts, "name", "string")
	assertType(t, facts, "age", "integer")
	assertType(t, facts, "active", "boolean")
	assertType(t, facts, "orders", "array")
}

func TestNamespacedLoader(t *testing.T) {
	// Test data
	type Customer struct {
		Name   string `json:"name"`
		Age    int    `json:"age"`
		Active bool   `json:"active"`
	}

	type Product struct {
		ID    string  `json:"id"`
		Price float64 `json:"price"`
		Stock int     `json:"stock"`
	}

	customer := Customer{
		Name:   "Jane Smith",
		Age:    28,
		Active: true,
	}

	product := Product{
		ID:    "prod-123",
		Price: 29.99,
		Stock: 42,
	}

	// JSON data
	orderJSON := []byte(`{
		"id": "order-456",
		"customer_id": "cust-789",
		"items": [
			{"product_id": "prod-123", "quantity": 2},
			{"product_id": "prod-456", "quantity": 1}
		],
		"total": 89.97
	}`)

	// Create namespaced loader
	loader := NewNamespacedLoader()
	loader.AddSource("customer", customer)
	loader.AddSource("product", product)
	loader.AddSource("order", orderJSON)

	// Load into provider
	facts, err := loader.Load()
	assert.NoError(t, err)
	assert.NotNil(t, facts)

	// Test access with namespaces
	value, exists := facts.Get("customer.name")
	assert.True(t, exists)
	assert.Equal(t, "Jane Smith", value)

	value, exists = facts.Get("product.price")
	assert.True(t, exists)
	assert.Equal(t, 29.99, value)

	value, exists = facts.Get("order.items[0].quantity")
	assert.True(t, exists)
	assert.Equal(t, float64(2), value)

	// Test complex type information
	assertType(t, facts, "customer.name", "string")
	assertType(t, facts, "product.price", "float")
	assertType(t, facts, "order.items", "array")
}

func assertType(t *testing.T, provider *RegistryFactProvider, path, expected string) {
	typeInfo, exists := provider.GetRegistry().GetType(path)
	assert.True(t, exists)
	typeStr, ok := typeInfo.(string)
	assert.True(t, ok)
	assert.Equal(t, expected, NormalizeTypeName(typeStr))
}

func TestStructConversion(t *testing.T) {
	// Define a complex struct with nested types
	type Address struct {
		Street  string `json:"street"`
		City    string `json:"city"`
		Country string `json:"country,omitempty"`
		ZipCode string `json:"-"` // Should be excluded
	}

	type Order struct {
		ID     string  `json:"id"`
		Amount float64 `json:"amount"`
	}

	type Customer struct {
		Name    string    `json:"name"`
		Age     int       `json:"age"`
		Active  bool      `json:"active"`
		Address Address   `json:"address"`
		Orders  []Order   `json:"orders"`
		Created time.Time `json:"created_at"`
		private string    // Should be excluded (unexported)
	}

	// Create test data
	now := time.Now()
	customer := Customer{
		Name:   "John Doe",
		Age:    30,
		Active: true,
		Address: Address{
			Street:  "123 Main St",
			City:    "New York",
			Country: "USA",
			ZipCode: "10001", // Should be excluded
		},
		Orders: []Order{
			{ID: "order1", Amount: 99.99},
			{ID: "order2", Amount: 149.95},
		},
		Created: now,
		private: "secret", // Should be excluded
	}

	// Test conversion
	result, err := structToMap(customer)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// Check top-level fields
	assert.Equal(t, "John Doe", result["name"])
	assert.Equal(t, 30, result["age"])
	assert.Equal(t, true, result["active"])
	assert.Equal(t, now, result["created_at"])

	// Check nested struct
	addressMap, ok := result["address"].(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "123 Main St", addressMap["street"])
	assert.Equal(t, "New York", addressMap["city"])
	assert.Equal(t, "USA", addressMap["country"])
	assert.NotContains(t, addressMap, "ZipCode") // Should be excluded

	// Check slice of structs
	ordersSlice, ok := result["orders"].([]interface{})
	assert.True(t, ok)
	assert.Len(t, ordersSlice, 2)
	order1, ok := ordersSlice[0].(map[string]interface{})
	assert.True(t, ok)
	order2, ok := ordersSlice[1].(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "order1", order1["id"])
	assert.Equal(t, 99.99, order1["amount"])
	assert.Equal(t, "order2", order2["id"])
	assert.Equal(t, 149.95, order2["amount"])

	// Check excluded fields
	assert.NotContains(t, result, "private") // Should be excluded
}

func TestNestedStructConversion(t *testing.T) {
	// Define a complex struct with various types and nesting
	type NestedStruct struct {
		IntValue    int               `json:"int_value"`
		StringValue string            `json:"string_value"`
		MapValue    map[string]string `json:"map_value"`
		SliceValue  []int             `json:"slice_value"`
	}

	type TestStruct struct {
		Nested       NestedStruct             `json:"nested"`
		NestedPtr    *NestedStruct            `json:"nested_ptr,omitempty"`
		PtrSlice     []*NestedStruct          `json:"ptr_slice"`
		MapOfStructs map[string]NestedStruct  `json:"map_of_structs"`
		MapOfPtrs    map[string]*NestedStruct `json:"map_of_ptrs"`
	}

	// Create test data
	nestedValue := NestedStruct{
		IntValue:    42,
		StringValue: "nested value",
		MapValue:    map[string]string{"key1": "value1", "key2": "value2"},
		SliceValue:  []int{1, 2, 3},
	}

	testData := TestStruct{
		Nested:    nestedValue,
		NestedPtr: &nestedValue,
		PtrSlice:  []*NestedStruct{&nestedValue, nil},
		MapOfStructs: map[string]NestedStruct{
			"struct1": nestedValue,
		},
		MapOfPtrs: map[string]*NestedStruct{
			"ptr1": &nestedValue,
			"ptr2": nil,
		},
	}

	// Test deep conversion
	result, err := structToNestedMap(testData)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// Check the result is a map
	resultMap, ok := result.(map[string]interface{})
	assert.True(t, ok)

	// Check nested struct
	nestedMap, ok := resultMap["nested"].(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, 42, nestedMap["int_value"])
	assert.Equal(t, "nested value", nestedMap["string_value"])

	// Check nested ptr
	nestedPtrMap, ok := resultMap["nested_ptr"].(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, 42, nestedPtrMap["int_value"])

	// Check ptr slice
	ptrSlice, ok := resultMap["ptr_slice"].([]interface{})
	assert.True(t, ok)
	assert.Len(t, ptrSlice, 2)
	ptrSliceItem, ok := ptrSlice[0].(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, 42, ptrSliceItem["int_value"])
	assert.Nil(t, ptrSlice[1]) // Nil pointer

	// Check map of structs
	mapOfStructs, ok := resultMap["map_of_structs"].(map[string]interface{})
	assert.True(t, ok)
	struct1, ok := mapOfStructs["struct1"].(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, 42, struct1["int_value"])

	// Check map of ptrs
	mapOfPtrs, ok := resultMap["map_of_ptrs"].(map[string]interface{})
	assert.True(t, ok)
	ptr1, ok := mapOfPtrs["ptr1"].(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, 42, ptr1["int_value"])
	assert.Nil(t, mapOfPtrs["ptr2"]) // Nil pointer
}
