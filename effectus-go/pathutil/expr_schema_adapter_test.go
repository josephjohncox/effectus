package pathutil

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTypedExprFacts_Get(t *testing.T) {
	// Create sample data
	data := map[string]interface{}{
		"customer": map[string]interface{}{
			"name":    "John Doe",
			"age":     30,
			"active":  true,
			"balance": 150.75,
			"tags":    []string{"premium", "loyal"},
			"address": map[string]interface{}{
				"city":    "New York",
				"country": "USA",
			},
		},
		"orders": []interface{}{
			map[string]interface{}{
				"id":     "ORD-123",
				"total":  99.99,
				"status": "shipped",
			},
			map[string]interface{}{
				"id":     "ORD-456",
				"total":  75.50,
				"status": "pending",
			},
		},
	}

	// Create typed facts provider
	facts := NewExprFacts(data)
	typedFacts := NewTypedExprFacts(facts)

	// Test simple access
	value, exists := typedFacts.Get("customer.name")
	assert.True(t, exists)
	assert.Equal(t, "John Doe", value)

	// Test array access
	value, exists = typedFacts.Get("orders[0].id")
	assert.True(t, exists)
	assert.Equal(t, "ORD-123", value)

	// Test non-existent path
	_, exists = typedFacts.Get("customer.non_existent")
	assert.False(t, exists)
}

func TestTypedExprFacts_GetTypeInfo(t *testing.T) {
	// Create sample data
	data := map[string]interface{}{
		"customer": map[string]interface{}{
			"name":    "John Doe",
			"age":     30,
			"active":  true,
			"balance": 150.75,
		},
		"orders": []interface{}{
			map[string]interface{}{
				"id":     "ORD-123",
				"total":  99.99,
				"status": "shipped",
			},
		},
	}

	// Create typed facts provider
	facts := NewExprFacts(data)
	typedFacts := NewTypedExprFacts(facts)
	typedFacts.RegisterNestedTypes()

	// Test type info for various paths
	assert.Equal(t, "map", typedFacts.GetTypeInfo("customer"))
	assert.Equal(t, "string", typedFacts.GetTypeInfo("customer.name"))
	assert.Equal(t, "integer", typedFacts.GetTypeInfo("customer.age"))
	assert.Equal(t, "boolean", typedFacts.GetTypeInfo("customer.active"))
	assert.Equal(t, "float", typedFacts.GetTypeInfo("customer.balance"))
	assert.Equal(t, "array", typedFacts.GetTypeInfo("orders"))
	assert.Equal(t, "map", typedFacts.GetTypeInfo("orders[0]"))
	assert.Equal(t, "string", typedFacts.GetTypeInfo("orders[0].id"))

	// Test non-existent path
	assert.Equal(t, "unknown", typedFacts.GetTypeInfo("customer.non_existent"))
}

func TestTypedExprFacts_EvaluateExprWithType(t *testing.T) {
	// Create sample data
	data := map[string]interface{}{
		"a":    10,
		"b":    20,
		"c":    true,
		"d":    "hello",
		"nums": []int{1, 2, 3, 4, 5},
		"user": map[string]interface{}{
			"name": "Alice",
			"age":  25,
		},
	}

	// Create typed facts provider
	facts := NewExprFacts(data)
	typedFacts := NewTypedExprFacts(facts)

	// Test expressions with different result types
	testCases := []struct {
		name        string
		expr        string
		wantValue   interface{}
		wantTypeStr string
	}{
		{
			name:        "simple arithmetic",
			expr:        "a + b",
			wantValue:   30,
			wantTypeStr: "int",
		},
		{
			name:        "boolean expression",
			expr:        "a > b",
			wantValue:   false,
			wantTypeStr: "bool",
		},
		{
			name:        "string concatenation",
			expr:        "d + ' world'",
			wantValue:   "hello world",
			wantTypeStr: "string",
		},
		{
			name:        "array access",
			expr:        "nums[2]",
			wantValue:   3,
			wantTypeStr: "int",
		},
		{
			name:        "object access",
			expr:        "user.name",
			wantValue:   "Alice",
			wantTypeStr: "string",
		},
		{
			name:        "complex expression",
			expr:        "a * 2 + b / 2",
			wantValue:   float64(30), // expr library converts divisions to float64
			wantTypeStr: "float64",
		},
		{
			name:        "function call",
			expr:        "len(nums)",
			wantValue:   5,
			wantTypeStr: "int",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			value, resultType, err := typedFacts.EvaluateExprWithType(tc.expr)

			assert.NoError(t, err)
			assert.Equal(t, tc.wantValue, value)
			assert.Equal(t, tc.wantTypeStr, resultType.String())

			// Verify type caching works
			cachedValue, cachedType, err := typedFacts.EvaluateExprWithType(tc.expr)
			assert.NoError(t, err)
			assert.Equal(t, value, cachedValue)
			assert.Equal(t, resultType, cachedType)
		})
	}
}

func TestTypedExprFacts_TypeCheck(t *testing.T) {
	// Create sample data
	data := map[string]interface{}{
		"a": 10,
		"b": 20,
		"c": true,
		"d": "hello",
	}

	// Create typed facts provider
	facts := NewExprFacts(data)
	typedFacts := NewTypedExprFacts(facts)

	// Valid expressions should not return errors
	assert.NoError(t, typedFacts.TypeCheck("a + b"))
	assert.NoError(t, typedFacts.TypeCheck("a > b"))
	assert.NoError(t, typedFacts.TypeCheck("c && (a < b)"))
	assert.NoError(t, typedFacts.TypeCheck("d + ' world'"))

	// Test with AllowUndefinedVariables flag
	assert.NoError(t, typedFacts.TypeCheck("unknown_var + 5"))

	// Invalid syntax should return errors
	assert.Error(t, typedFacts.TypeCheck("a +"))
	assert.Error(t, typedFacts.TypeCheck("(a + b"))
}

func TestCreateTypedFactProvider_Struct(t *testing.T) {
	// Define a test struct
	type Address struct {
		Street  string
		City    string
		Country string
	}

	type User struct {
		Name    string
		Age     int
		Active  bool
		Address Address
		Tags    []string
	}

	// Create test data
	user := User{
		Name:   "John Doe",
		Age:    30,
		Active: true,
		Address: Address{
			Street:  "123 Main St",
			City:    "New York",
			Country: "USA",
		},
		Tags: []string{"admin", "user"},
	}

	// Create typed fact provider from struct
	typedFacts, err := CreateTypedFactProvider(user)
	assert.NoError(t, err)

	// Test data access
	value, exists := typedFacts.Get("data.Name")
	assert.True(t, exists)
	assert.Equal(t, "John Doe", value)

	value, exists = typedFacts.Get("data.Age")
	assert.True(t, exists)
	assert.Equal(t, 30, value)

	value, exists = typedFacts.Get("data.Active")
	assert.True(t, exists)
	assert.Equal(t, true, value)

	value, exists = typedFacts.Get("data.Address.City")
	assert.True(t, exists)
	assert.Equal(t, "New York", value)

	// Test type info
	assert.Equal(t, "string", typedFacts.GetTypeInfo("data.Name"))
	assert.Equal(t, "integer", typedFacts.GetTypeInfo("data.Age"))
	assert.Equal(t, "boolean", typedFacts.GetTypeInfo("data.Active"))
	assert.Equal(t, "object", typedFacts.GetTypeInfo("data.Address"))
	assert.Equal(t, "string", typedFacts.GetTypeInfo("data.Address.City"))
	assert.Equal(t, "array", typedFacts.GetTypeInfo("data.Tags"))
}

func TestGetPathType(t *testing.T) {
	// Create sample data
	data := map[string]interface{}{
		"a": 10,
		"b": "hello",
		"c": true,
		"d": 3.14,
		"e": []string{"one", "two", "three"},
		"f": map[string]int{"x": 1, "y": 2},
	}

	// Create typed facts provider
	facts := NewExprFacts(data)
	typedFacts := NewTypedExprFacts(facts)

	// Test getting path types
	assert.Equal(t, reflect.TypeOf(10), typedFacts.GetPathType("a"))
	assert.Equal(t, reflect.TypeOf(""), typedFacts.GetPathType("b"))
	assert.Equal(t, reflect.TypeOf(true), typedFacts.GetPathType("c"))
	assert.Equal(t, reflect.TypeOf(3.14), typedFacts.GetPathType("d"))
	assert.Equal(t, reflect.TypeOf([]string{}), typedFacts.GetPathType("e"))
	assert.Equal(t, reflect.TypeOf(map[string]int{}), typedFacts.GetPathType("f"))

	// Non-existent path should return nil
	assert.Nil(t, typedFacts.GetPathType("non_existent"))
}

// TestNestedComplexStructs tests handling of deeply nested complex struct types
func TestNestedComplexStructs(t *testing.T) {
	// Define complex nested structs
	type GeoLocation struct {
		Latitude  float64
		Longitude float64
	}

	type ContactDetails struct {
		Email     string
		Phone     string
		Emergency *struct {
			Name         string
			Relationship string
			Phone        string
		}
	}

	type Address struct {
		Street     string
		City       string
		Country    string
		PostalCode string
		Location   GeoLocation
	}

	type Transaction struct {
		ID        string
		Amount    float64
		Timestamp time.Time
		Status    string
	}

	type Customer struct {
		ID      string
		Profile struct {
			Name            string
			Age             int
			JoinDate        time.Time
			PremiumMember   bool
			Contact         ContactDetails
			Address         Address
			PaymentMethods  []string
			TransactionList []Transaction
			Preferences     map[string]interface{}
		}
		Stats map[string]int
	}

	// Create a complex customer
	now := time.Now()
	customer := Customer{
		ID: "CUST-12345",
		Profile: struct {
			Name            string
			Age             int
			JoinDate        time.Time
			PremiumMember   bool
			Contact         ContactDetails
			Address         Address
			PaymentMethods  []string
			TransactionList []Transaction
			Preferences     map[string]interface{}
		}{
			Name:          "Alice Smith",
			Age:           35,
			JoinDate:      now.AddDate(-2, -3, -15), // 2 years, 3 months, 15 days ago
			PremiumMember: true,
			Contact: ContactDetails{
				Email: "alice@example.com",
				Phone: "555-123-4567",
				Emergency: &struct {
					Name         string
					Relationship string
					Phone        string
				}{
					Name:         "Bob Smith",
					Relationship: "Spouse",
					Phone:        "555-765-4321",
				},
			},
			Address: Address{
				Street:     "123 Main St",
				City:       "Springfield",
				Country:    "USA",
				PostalCode: "12345",
				Location: GeoLocation{
					Latitude:  37.7749,
					Longitude: -122.4194,
				},
			},
			PaymentMethods: []string{"visa", "paypal", "crypto"},
			TransactionList: []Transaction{
				{
					ID:        "TXN-001",
					Amount:    125.50,
					Timestamp: now.AddDate(0, -1, 0), // 1 month ago
					Status:    "completed",
				},
				{
					ID:        "TXN-002",
					Amount:    75.25,
					Timestamp: now.AddDate(0, 0, -5), // 5 days ago
					Status:    "pending",
				},
			},
			Preferences: map[string]interface{}{
				"theme":          "dark",
				"notifications":  true,
				"weeklyReports":  false,
				"favoriteColors": []string{"blue", "green"},
			},
		},
		Stats: map[string]int{
			"totalOrders":  27,
			"totalSpent":   4350,
			"returnsCount": 2,
		},
	}

	// Create typed fact provider from the complex struct
	typedFacts, err := CreateTypedFactProvider(customer)
	assert.NoError(t, err)

	// Test accessing various nested paths
	test := func(path string, expectedValue interface{}, expectedType string) {
		t.Run(path, func(t *testing.T) {
			// Test value access
			value, exists := typedFacts.Get(path)
			assert.True(t, exists, "Path should exist: "+path)
			assert.Equal(t, expectedValue, value)

			// Test type info
			typeInfo := typedFacts.GetTypeInfo(path)
			assert.Equal(t, expectedType, typeInfo)
		})
	}

	// Basic fields
	test("data.ID", "CUST-12345", "string")
	test("data.Profile.Name", "Alice Smith", "string")
	test("data.Profile.Age", 35, "integer")
	test("data.Profile.PremiumMember", true, "boolean")

	// Nested struct fields
	test("data.Profile.Address.City", "Springfield", "string")
	test("data.Profile.Address.Location.Latitude", 37.7749, "float")

	// Pointer to struct
	test("data.Profile.Contact.Emergency.Name", "Bob Smith", "string")
	test("data.Profile.Contact.Emergency.Relationship", "Spouse", "string")

	// Arrays
	test("data.Profile.PaymentMethods[1]", "paypal", "string")

	// Maps
	test("data.Stats.totalOrders", 27, "integer")

	// Complex nested paths
	test("data.Profile.TransactionList[0].ID", "TXN-001", "string")
	test("data.Profile.TransactionList[0].Amount", 125.50, "float")
	test("data.Profile.TransactionList[0].Status", "completed", "string")

	// Map with complex values
	test("data.Profile.Preferences.theme", "dark", "string")
	test("data.Profile.Preferences.notifications", true, "boolean")
	test("data.Profile.Preferences.favoriteColors[0]", "blue", "string")
}

// TestComplexExpressions tests more complex expressions involving multiple operations
func TestComplexExpressions(t *testing.T) {
	// Create a complex data structure
	data := map[string]interface{}{
		"numbers": []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		"products": []map[string]interface{}{
			{
				"id":    "p1",
				"name":  "Laptop",
				"price": 999.99,
				"stock": 50,
				"tags":  []string{"electronics", "computers"},
				"specs": map[string]interface{}{
					"cpu":  "Intel i7",
					"ram":  16,
					"ssd":  512,
					"gpu":  "NVIDIA RTX 3060",
					"wifi": true,
				},
			},
			{
				"id":    "p2",
				"name":  "Smartphone",
				"price": 699.99,
				"stock": 100,
				"tags":  []string{"electronics", "mobile"},
				"specs": map[string]interface{}{
					"cpu":       "A15",
					"ram":       8,
					"storage":   256,
					"camera_mp": 12,
					"5g":        true,
				},
			},
			{
				"id":    "p3",
				"name":  "Headphones",
				"price": 199.99,
				"stock": 75,
				"tags":  []string{"electronics", "audio"},
				"specs": map[string]interface{}{
					"type":         "over-ear",
					"noise_cancel": true,
					"wireless":     true,
					"battery_hrs":  20,
				},
			},
		},
		"constants": map[string]interface{}{
			"vat_rate":                0.2,
			"discount":                0.1,
			"free_shipping_threshold": 500,
		},
		"user": map[string]interface{}{
			"name":    "John",
			"premium": true,
			"cart": []map[string]interface{}{
				{
					"product_id": "p1",
					"quantity":   1,
				},
				{
					"product_id": "p3",
					"quantity":   2,
				},
			},
		},
	}

	// Create typed facts provider
	facts := NewExprFacts(data)
	typedFacts := NewTypedExprFacts(facts)

	// Test complex expressions
	testCases := []struct {
		name        string
		expr        string
		wantValue   interface{}
		wantTypeStr string
	}{
		{
			name:        "array filtering with map",
			expr:        "filter(numbers, {# > 5})",
			wantValue:   []interface{}{6, 7, 8, 9, 10},
			wantTypeStr: "[]interface {}",
		},
		{
			name:        "array sum",
			expr:        "reduce(numbers, 0, {acc + #})",
			wantValue:   float64(55), // expr library uses float64 for reduce operations
			wantTypeStr: "float64",
		},
		{
			name:        "complex product lookup",
			expr:        "filter(products, {#.id == 'p2'})[0].specs.storage",
			wantValue:   float64(256), // expr library uses float64 for numeric literals in maps
			wantTypeStr: "float64",
		},
		{
			name:        "price calculation with taxes and discount",
			expr:        "products[0].price * (1 + constants.vat_rate) * (1 - constants.discount)",
			wantValue:   float64(999.99 * 1.2 * 0.9),
			wantTypeStr: "float64",
		},
		{
			name:        "complex conditional",
			expr:        "user.premium ? 'Premium User' : 'Regular User'",
			wantValue:   "Premium User",
			wantTypeStr: "string",
		},
		{
			name:        "array operations in conditions",
			expr:        "any(user.cart, {#.product_id == 'p1'}) ? 'Has Laptop' : 'No Laptop'",
			wantValue:   "Has Laptop",
			wantTypeStr: "string",
		},
		{
			name:        "complex cart calculation",
			expr:        "sum(map(user.cart, {filter(products, {#.id == $})[0].price * @.quantity}))",
			wantValue:   float64(999.99 + 2*199.99), // laptop + 2*headphones
			wantTypeStr: "float64",
		},
		{
			name:        "boolean complex expression",
			expr:        "user.premium && sum(map(user.cart, {filter(products, {#.id == $})[0].price * @.quantity})) > constants.free_shipping_threshold",
			wantValue:   true,
			wantTypeStr: "bool",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			value, resultType, err := typedFacts.EvaluateExprWithType(tc.expr)

			assert.NoError(t, err)
			assert.Equal(t, tc.wantValue, value)
			assert.Equal(t, tc.wantTypeStr, resultType.String())
		})
	}
}

// TestNilValues tests handling of nil values in data structures
func TestNilValues(t *testing.T) {
	// Create data with nil values
	data := map[string]interface{}{
		"a": nil,
		"b": map[string]interface{}{
			"c": nil,
			"d": "value",
			"e": map[string]interface{}{
				"f": nil,
			},
		},
		"g": []interface{}{1, nil, 3},
	}

	// Create typed facts provider
	facts := NewExprFacts(data)
	typedFacts := NewTypedExprFacts(facts)
	typedFacts.RegisterNestedTypes()

	// Test nil value handling
	value, exists := typedFacts.Get("a")
	assert.True(t, exists)
	assert.Nil(t, value)

	value, exists = typedFacts.Get("b.c")
	assert.True(t, exists)
	assert.Nil(t, value)

	value, exists = typedFacts.Get("b.e.f")
	assert.True(t, exists)
	assert.Nil(t, value)

	value, exists = typedFacts.Get("g[1]")
	assert.True(t, exists)
	assert.Nil(t, value)

	// Test type info for nil values
	assert.Equal(t, "nil", typedFacts.GetTypeInfo("a"))
	assert.Equal(t, "nil", typedFacts.GetTypeInfo("b.c"))
	assert.Equal(t, "nil", typedFacts.GetTypeInfo("b.e.f"))
	assert.Equal(t, "nil", typedFacts.GetTypeInfo("g[1]"))

	// Test expressions with nil values
	result, resultType, err := typedFacts.EvaluateExprWithType("a == nil")
	assert.NoError(t, err)
	assert.Equal(t, true, result)
	assert.Equal(t, "bool", resultType.String())

	result, resultType, err = typedFacts.EvaluateExprWithType("b.c == nil")
	assert.NoError(t, err)
	assert.Equal(t, true, result)
	assert.Equal(t, "bool", resultType.String())

	// Test nil in conditions
	result, resultType, err = typedFacts.EvaluateExprWithType("b.c == nil ? 'nil value' : 'non-nil'")
	assert.NoError(t, err)
	assert.Equal(t, "nil value", result)
	assert.Equal(t, "string", resultType.String())

	// Test array operations with nil
	result, resultType, err = typedFacts.EvaluateExprWithType("filter(g, {# != nil})")
	assert.NoError(t, err)
	assert.Equal(t, []interface{}{1, 3}, result)
	assert.Equal(t, "[]interface {}", resultType.String())
}

// TestExpressionCaching tests that the expression cache works correctly
func TestExpressionCaching(t *testing.T) {
	// Create data
	data := map[string]interface{}{
		"a": 10,
		"b": 20,
	}

	// Create typed facts provider
	facts := NewExprFacts(data)
	typedFacts := NewTypedExprFacts(facts)

	// First evaluation (should compile and cache)
	expr := "a * 2 + b"
	value1, type1, err1 := typedFacts.EvaluateExprWithType(expr)
	assert.NoError(t, err1)
	assert.Equal(t, 40, value1)

	// Update the fact data
	facts.UpdateData(map[string]interface{}{
		"a": 20, // Changed from 10
		"b": 20,
	})

	// Second evaluation (should use cached program but with updated data)
	value2, type2, err2 := typedFacts.EvaluateExprWithType(expr)
	assert.NoError(t, err2)
	assert.Equal(t, 60, value2)   // Result should change with the data
	assert.Equal(t, type1, type2) // Type should remain the same

	// Check that we're using the cached type
	assert.Equal(t, reflect.TypeOf(40), type1)
}

// TestIntegration tests the integration of multiple features
func TestIntegration(t *testing.T) {
	// Define a complex order system
	type OrderItem struct {
		ProductID   string
		ProductName string
		Quantity    int
		UnitPrice   float64
		Discounted  bool
	}

	type Order struct {
		ID           string
		CustomerName string
		Items        []OrderItem
		ShippingInfo struct {
			Address  string
			Priority string
			Cost     float64
		}
		Status     string
		TotalPrice float64
	}

	// Create a sample order
	order := Order{
		ID:           "ORD-12345",
		CustomerName: "Jane Smith",
		Items: []OrderItem{
			{
				ProductID:   "P100",
				ProductName: "Ergonomic Keyboard",
				Quantity:    1,
				UnitPrice:   129.99,
				Discounted:  false,
			},
			{
				ProductID:   "P200",
				ProductName: "Wireless Mouse",
				Quantity:    2,
				UnitPrice:   49.99,
				Discounted:  true,
			},
		},
		Status: "processing",
	}

	// Calculate total price
	var total float64
	for _, item := range order.Items {
		price := item.UnitPrice * float64(item.Quantity)
		if item.Discounted {
			price *= 0.9 // 10% discount
		}
		total += price
	}
	order.TotalPrice = total

	// Set shipping info
	order.ShippingInfo.Address = "123 Main St, Springfield"
	order.ShippingInfo.Priority = "standard"
	order.ShippingInfo.Cost = 10.00

	// Create a typed fact provider
	typedFacts, err := CreateTypedFactProvider(order)
	assert.NoError(t, err)

	// Run several higher-level tests
	t.Run("ComplexPathAccess", func(t *testing.T) {
		// Access nested fields
		value, exists := typedFacts.Get("data.Items[1].ProductName")
		assert.True(t, exists)
		assert.Equal(t, "Wireless Mouse", value)

		// Type checking
		assert.Equal(t, "string", typedFacts.GetTypeInfo("data.Items[1].ProductName"))
		assert.Equal(t, "float", typedFacts.GetTypeInfo("data.Items[1].UnitPrice"))
		assert.Equal(t, "integer", typedFacts.GetTypeInfo("data.Items[1].Quantity"))
		assert.Equal(t, "boolean", typedFacts.GetTypeInfo("data.Items[1].Discounted"))
	})

	t.Run("ExpressionBasedCalculations", func(t *testing.T) {
		// Calculate first item subtotal
		result, _, err := typedFacts.EvaluateExprWithType("data.Items[0].UnitPrice * data.Items[0].Quantity")
		assert.NoError(t, err)
		assert.InDelta(t, 129.99, result, 0.001)

		// Calculate second item subtotal with discount
		result, _, err = typedFacts.EvaluateExprWithType("data.Items[1].Discounted ? data.Items[1].UnitPrice * data.Items[1].Quantity * 0.9 : data.Items[1].UnitPrice * data.Items[1].Quantity")
		assert.NoError(t, err)
		assert.InDelta(t, 49.99*2*0.9, result, 0.001)

		// Verify total calculation
		result, _, err = typedFacts.EvaluateExprWithType("data.TotalPrice")
		assert.NoError(t, err)
		assert.InDelta(t, total, result, 0.001)

		// Independent calculation to verify
		result, _, err = typedFacts.EvaluateExprWithType("data.Items[0].UnitPrice * data.Items[0].Quantity + (data.Items[1].Discounted ? data.Items[1].UnitPrice * data.Items[1].Quantity * 0.9 : data.Items[1].UnitPrice * data.Items[1].Quantity)")
		assert.NoError(t, err)
		assert.InDelta(t, total, result, 0.001)
	})

	t.Run("ComplexFilter", func(t *testing.T) {
		// Check if any item is discounted
		result, _, err := typedFacts.EvaluateExprWithType("any(data.Items, {#.Discounted})")
		assert.NoError(t, err)
		assert.Equal(t, true, result)

		// Get the name of the discounted item
		result, _, err = typedFacts.EvaluateExprWithType("filter(data.Items, {#.Discounted})[0].ProductName")
		assert.NoError(t, err)
		assert.Equal(t, "Wireless Mouse", result)
	})

	t.Run("ConditionalExpressions", func(t *testing.T) {
		// Shipping status message
		result, _, err := typedFacts.EvaluateExprWithType("data.Status == 'processing' ? 'Your order is being processed' : 'Your order has been shipped'")
		assert.NoError(t, err)
		assert.Equal(t, "Your order is being processed", result)

		// Discount status
		result, _, err = typedFacts.EvaluateExprWithType("any(data.Items, {#.Discounted}) ? 'Order contains discounted items' : 'No discounts applied'")
		assert.NoError(t, err)
		assert.Equal(t, "Order contains discounted items", result)
	})
}
