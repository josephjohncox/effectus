package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/effectus/effectus-go/pathutil"
)

func main() {
	// Example 1: Using ExprFacts directly
	fmt.Println("=== Example 1: Basic ExprFacts ===")
	basicExample()

	// Example 2: Using TypedExprFacts with type information
	fmt.Println("\n=== Example 2: TypedExprFacts with Type Information ===")
	typedExample()

	// Example 3: Loading from JSON
	fmt.Println("\n=== Example 3: Loading from JSON ===")
	jsonExample()

	// Example 4: Loading from structs
	fmt.Println("\n=== Example 4: Loading from Structs ===")
	structExample()

	// Example 5: Using namespaced data
	fmt.Println("\n=== Example 5: Namespaced Data ===")
	namespacedExample()

	// Example 6: Advanced expressions
	fmt.Println("\n=== Example 6: Advanced Expressions ===")
	advancedExpressionsExample()
}

func basicExample() {
	// Create a simple data structure
	data := map[string]interface{}{
		"user": map[string]interface{}{
			"name":  "Alice",
			"age":   30,
			"roles": []string{"admin", "user"},
		},
		"settings": map[string]interface{}{
			"theme":       "dark",
			"language":    "en",
			"preferences": map[string]interface{}{"notifications": true},
		},
	}

	// Create ExprFacts provider
	facts := pathutil.NewExprFacts(data)

	// Access data using path expressions
	userName, _ := facts.Get("user.name")
	userAge, _ := facts.Get("user.age")
	userRole, _ := facts.Get("user.roles[0]")
	theme, _ := facts.Get("settings.theme")
	notifications, _ := facts.Get("settings.preferences.notifications")

	fmt.Printf("User: %s (age: %d, role: %s)\n", userName, userAge, userRole)
	fmt.Printf("Theme: %s, Notifications: %t\n", theme, notifications)

	// Evaluate expressions
	isAdmin, err := facts.EvaluateExprBool("'admin' in user.roles")
	if err != nil {
		log.Fatalf("Expression error: %v", err)
	}
	fmt.Printf("Is admin: %t\n", isAdmin)

	// Count roles
	roleCount, err := facts.EvaluateExpr("len(user.roles)")
	if err != nil {
		log.Fatalf("Expression error: %v", err)
	}
	fmt.Printf("Role count: %d\n", roleCount)
}

func typedExample() {
	// Create a data structure with different types
	data := map[string]interface{}{
		"metrics": map[string]interface{}{
			"count":       42,
			"temperature": 72.5,
			"active":      true,
			"labels":      []string{"prod", "web"},
			"created_at":  time.Now(),
			"metadata": map[string]interface{}{
				"region": "us-west",
				"zone":   "a",
			},
		},
	}

	// Create ExprFacts provider
	facts := pathutil.NewExprFacts(data)

	// Create TypedExprFacts with type information
	typedFacts := pathutil.NewTypedExprFacts(facts)

	// Register nested types
	typedFacts.RegisterNestedTypes()

	// Get data and type information
	paths := []string{
		"metrics.count",
		"metrics.temperature",
		"metrics.active",
		"metrics.labels",
		"metrics.created_at",
		"metrics.metadata.region",
	}

	for _, path := range paths {
		value, exists := typedFacts.Get(path)
		if !exists {
			fmt.Printf("Path not found: %s\n", path)
			continue
		}

		typeName := typedFacts.GetTypeInfo(path)
		fmt.Printf("Path: %-25s | Type: %-10s | Value: %v\n", path, typeName, value)
	}

	// Use type information for validation
	expr := "metrics.count > 0 && metrics.temperature < 100"
	err := typedFacts.TypeCheck(expr)
	if err != nil {
		fmt.Printf("Type check failed: %v\n", err)
	} else {
		fmt.Printf("Expression is valid: %s\n", expr)
	}

	// Evaluate with type information
	result, resultType, err := typedFacts.EvaluateExprWithType("metrics.count * 2")
	if err != nil {
		fmt.Printf("Evaluation error: %v\n", err)
	} else {
		fmt.Printf("Result: %v (Type: %s)\n", result, resultType)
	}
}

func jsonExample() {
	// JSON data representing a product catalog
	jsonData := `{
		"products": [
			{
				"id": "p1",
				"name": "Laptop",
				"price": 999.99,
				"in_stock": true,
				"specs": {
					"cpu": "Intel i7",
					"ram": 16,
					"storage": 512
				},
				"tags": ["electronics", "computers"]
			},
			{
				"id": "p2",
				"name": "Smartphone",
				"price": 699.99,
				"in_stock": true,
				"specs": {
					"cpu": "A15",
					"ram": 6,
					"storage": 128
				},
				"tags": ["electronics", "mobile"]
			}
		],
		"store": {
			"name": "Tech Store",
			"location": "San Francisco",
			"established": 2010
		}
	}`

	// Create JSON loader
	loader := pathutil.NewJSONLoader([]byte(jsonData))

	// Load into TypedExprFacts
	typedFacts, err := loader.LoadIntoTypedFacts()
	if err != nil {
		log.Fatalf("Failed to load JSON: %v", err)
	}

	// Get the underlying ExprFacts for expression evaluation
	facts := typedFacts.GetUnderlyingFacts()

	// Access product information
	fmt.Println("Products:")
	for i := 0; i < 2; i++ {
		productPath := fmt.Sprintf("products[%d]", i)
		name, _ := typedFacts.Get(productPath + ".name")
		price, _ := typedFacts.Get(productPath + ".price")
		inStock, _ := typedFacts.Get(productPath + ".in_stock")
		firstTag, _ := typedFacts.Get(productPath + ".tags[0]")

		fmt.Printf("  - %s: $%.2f (In stock: %t, Category: %s)\n", name, price, inStock, firstTag)

		// Get specs
		cpu, _ := typedFacts.Get(productPath + ".specs.cpu")
		ram, _ := typedFacts.Get(productPath + ".specs.ram")
		storage, _ := typedFacts.Get(productPath + ".specs.storage")
		fmt.Printf("    Specs: CPU: %s, RAM: %d GB, Storage: %d GB\n", cpu, ram, storage)
	}

	// Store information
	storeName, _ := typedFacts.Get("store.name")
	storeLocation, _ := typedFacts.Get("store.location")
	fmt.Printf("\nStore: %s in %s\n", storeName, storeLocation)

	// Use expressions to filter products
	result, err := facts.EvaluateExpr("filter(products, {#.price < 800})")
	if err != nil {
		log.Fatalf("Filter error: %v", err)
	}

	// Display filtered products
	filteredProducts, ok := result.([]interface{})
	if ok {
		fmt.Printf("\nProducts under $800:\n")
		for _, product := range filteredProducts {
			p := product.(map[string]interface{})
			fmt.Printf("  - %s: $%.2f\n", p["name"], p["price"])
		}
	}
}

// Product structure for struct loader example
type Product struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Price    float64 `json:"price"`
	InStock  bool    `json:"in_stock"`
	Category string  `json:"category"`
}

// Inventory structure for struct loader example
type Inventory struct {
	Products []Product `json:"products"`
	Location string    `json:"location"`
	Updated  time.Time `json:"updated_at"`
}

func structExample() {
	// Create inventory with products
	inventory := Inventory{
		Products: []Product{
			{
				ID:       "p1",
				Name:     "Monitor",
				Price:    349.99,
				InStock:  true,
				Category: "electronics",
			},
			{
				ID:       "p2",
				Name:     "Keyboard",
				Price:    79.99,
				InStock:  true,
				Category: "accessories",
			},
			{
				ID:       "p3",
				Name:     "Mouse",
				Price:    49.99,
				InStock:  false,
				Category: "accessories",
			},
		},
		Location: "Warehouse A",
		Updated:  time.Now(),
	}

	// Create struct loader
	loader := pathutil.NewStructLoader(inventory)

	// Load into TypedExprFacts
	typedFacts, err := loader.LoadIntoTypedFacts()
	if err != nil {
		log.Fatalf("Failed to load struct: %v", err)
	}

	// Get the underlying ExprFacts for expression evaluation
	facts := typedFacts.GetUnderlyingFacts()

	// Display inventory information
	location, _ := typedFacts.Get("location")
	updated, _ := typedFacts.Get("updated_at")
	fmt.Printf("Inventory at %s (updated: %v)\n", location, updated)

	// Count products
	count, err := facts.EvaluateExpr("len(products)")
	if err != nil {
		log.Fatalf("Count error: %v", err)
	}
	fmt.Printf("Total products: %d\n", count)

	// List products
	fmt.Println("Products:")
	for i := 0; i < int(count.(int)); i++ {
		name, _ := typedFacts.Get(fmt.Sprintf("products[%d].name", i))
		price, _ := typedFacts.Get(fmt.Sprintf("products[%d].price", i))
		inStock, _ := typedFacts.Get(fmt.Sprintf("products[%d].in_stock", i))
		category, _ := typedFacts.Get(fmt.Sprintf("products[%d].category", i))

		stockStatus := "In stock"
		if !inStock.(bool) {
			stockStatus = "Out of stock"
		}

		fmt.Printf("  - %s: $%.2f (%s) [%s]\n", name, price, stockStatus, category)
	}

	// Find products by category
	result, err := facts.EvaluateExpr("filter(products, {#.category == 'accessories'})")
	if err != nil {
		log.Fatalf("Filter error: %v", err)
	}

	accessories, ok := result.([]interface{})
	if ok {
		fmt.Printf("\nAccessories category (%d products):\n", len(accessories))
		for _, item := range accessories {
			product := item.(map[string]interface{})
			fmt.Printf("  - %s: $%.2f\n", product["name"], product["price"])
		}
	}

	// Calculate total value of in-stock products
	totalValue, err := facts.EvaluateExpr("sum(map(filter(products, {#.in_stock}), {#.price}))")
	if err != nil {
		log.Fatalf("Calculation error: %v", err)
	}
	fmt.Printf("\nTotal value of in-stock products: $%.2f\n", totalValue)
}

func namespacedExample() {
	// Define types for our data sources
	type User struct {
		ID       string   `json:"id"`
		Name     string   `json:"name"`
		Email    string   `json:"email"`
		Verified bool     `json:"verified"`
		Roles    []string `json:"roles"`
	}

	type Config struct {
		MaxUsers      int  `json:"max_users"`
		Debug         bool `json:"debug"`
		CacheEnabled  bool `json:"cache_enabled"`
		CacheDuration int  `json:"cache_duration"` // in seconds
	}

	// Create data sources
	user := User{
		ID:       "user123",
		Name:     "John Smith",
		Email:    "john@example.com",
		Verified: true,
		Roles:    []string{"editor", "viewer"},
	}

	config := Config{
		MaxUsers:      100,
		Debug:         false,
		CacheEnabled:  true,
		CacheDuration: 3600,
	}

	// JSON for stats
	statsJSON := `{
		"visits": 12500,
		"unique_users": 4200,
		"avg_session": 320,
		"top_pages": ["/home", "/products", "/about"],
		"conversion_rate": 0.035
	}`

	// Create namespaced loader
	loader := pathutil.NewNamespacedLoader()
	loader.AddSource("user", user)
	loader.AddSource("config", config)
	loader.AddSource("stats", []byte(statsJSON))

	// Load into TypedExprFacts
	typedFacts, err := loader.LoadIntoTypedFacts()
	if err != nil {
		log.Fatalf("Failed to load namespaced data: %v", err)
	}

	// Get the underlying ExprFacts for expression evaluation
	facts := typedFacts.GetUnderlyingFacts()

	// Access data across namespaces
	userName, _ := typedFacts.Get("user.name")
	userRoles, _ := typedFacts.Get("user.roles")
	cacheEnabled, _ := typedFacts.Get("config.cache_enabled")
	visits, _ := typedFacts.Get("stats.visits")
	topPage, _ := typedFacts.Get("stats.top_pages[0]")

	fmt.Printf("User: %s\n", userName)
	fmt.Printf("Roles: %v\n", userRoles)
	fmt.Printf("Cache enabled: %t\n", cacheEnabled)
	fmt.Printf("Site visits: %v\n", visits)
	fmt.Printf("Top page: %s\n", topPage)

	// Evaluate expressions across namespaces
	isAdmin, err := facts.EvaluateExprBool("'admin' in user.roles")
	if err != nil {
		log.Fatalf("Expression error: %v", err)
	}
	fmt.Printf("User is admin: %t\n", isAdmin)

	// Complex expression
	result, err := facts.EvaluateExpr("stats.visits > 10000 && config.cache_enabled && user.verified")
	if err != nil {
		log.Fatalf("Complex expression error: %v", err)
	}
	fmt.Printf("Complex condition result: %t\n", result)

	// Type checking
	fmt.Println("\nType Information:")
	paths := []string{
		"user.name",
		"user.roles",
		"config.max_users",
		"config.cache_enabled",
		"stats.visits",
		"stats.conversion_rate",
	}
	for _, path := range paths {
		typeName := typedFacts.GetTypeInfo(path)
		fmt.Printf("  - %-25s: %s\n", path, typeName)
	}
}

func advancedExpressionsExample() {
	// Create a data structure for testing advanced expressions
	data := map[string]interface{}{
		"items": []map[string]interface{}{
			{
				"id":     "item1",
				"name":   "Widget A",
				"price":  29.99,
				"tags":   []string{"sale", "new", "featured"},
				"stock":  42,
				"rating": 4.5,
			},
			{
				"id":     "item2",
				"name":   "Widget B",
				"price":  19.99,
				"tags":   []string{"sale", "clearance"},
				"stock":  7,
				"rating": 3.8,
			},
			{
				"id":     "item3",
				"name":   "Widget C",
				"price":  49.99,
				"tags":   []string{"new", "premium"},
				"stock":  0,
				"rating": 4.9,
			},
			{
				"id":     "item4",
				"name":   "Widget D",
				"price":  39.99,
				"tags":   []string{"featured"},
				"stock":  15,
				"rating": 4.2,
			},
		},
		"constants": map[string]interface{}{
			"tax_rate":       0.08,
			"free_shipping":  35.00,
			"discount_rate":  0.10,
			"low_stock":      10,
			"min_rating":     4.0,
			"featured_bonus": 5,
		},
	}

	// Create ExprFacts provider
	facts := pathutil.NewExprFacts(data)

	// Demo different expressions
	examples := []struct {
		name       string
		expression string
		desc       string
	}{
		{
			name:       "Filter: In-stock items",
			expression: "filter(items, {#.stock > 0})",
			desc:       "Gets all items with stock greater than 0",
		},
		{
			name:       "Map: Item names",
			expression: "map(items, {#.name})",
			desc:       "Extracts all item names into a new array",
		},
		{
			name:       "Filter + Map: Names of on-sale items",
			expression: "map(filter(items, {contains(#.tags, 'sale')}), {#.name})",
			desc:       "Gets names of all items tagged as 'sale'",
		},
		{
			name:       "Any: Check if any premium items exist",
			expression: "any(items, {contains(#.tags, 'premium')})",
			desc:       "Checks if any item has the 'premium' tag",
		},
		{
			name:       "All: Check if all items are in stock",
			expression: "all(items, {#.stock > 0})",
			desc:       "Checks if all items have stock greater than 0",
		},
		{
			name:       "Count: Number of featured items",
			expression: "len(filter(items, {contains(#.tags, 'featured')}))",
			desc:       "Counts items with the 'featured' tag",
		},
		{
			name:       "Sum: Total inventory value",
			expression: "sum(map(items, {#.price * #.stock}))",
			desc:       "Calculates the total value of all items in stock",
		},
		{
			name: "Complex: Discounted prices with tax",
			expression: `map(items, {
				price: #.price * (1 - constants.discount_rate) * (1 + constants.tax_rate),
				name: #.name,
				savings: #.price * constants.discount_rate
			})`,
			desc: "Calculates final prices with discount and tax",
		},
		{
			name: "Complex: Item availability status",
			expression: `map(items, {
				name: #.name,
				status: #.stock == 0 ? "Out of stock" : (#.stock < constants.low_stock ? "Low stock" : "In stock")
			})`,
			desc: "Generates stock status messages based on inventory levels",
		},
		{
			name: "Complex: Order recommendations",
			expression: `filter(items, {
				#.rating >= constants.min_rating && 
				(contains(#.tags, "featured") || #.stock < constants.low_stock)
			})`,
			desc: "Finds highly-rated items that are either featured or low in stock",
		},
	}

	// Run and display each example
	for _, ex := range examples {
		fmt.Printf("\n--- %s ---\n", ex.name)
		fmt.Printf("Expression: %s\n", strings.TrimSpace(ex.expression))
		fmt.Printf("Description: %s\n", ex.desc)

		result, err := facts.EvaluateExpr(ex.expression)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}

		// Format and display the result
		jsonResult, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			fmt.Printf("Result: %v\n", result)
		} else {
			fmt.Printf("Result:\n%s\n", jsonResult)
		}
	}
}
