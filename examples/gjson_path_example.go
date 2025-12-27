package main

import (
	"fmt"
	"log"

	"github.com/effectus/effectus-go/pathutil"
)

// Example struct type
type User struct {
	Name     string   `json:"name"`
	Age      int      `json:"age"`
	IsActive bool     `json:"is_active"`
	Roles    []string `json:"roles"`
	Address  Address  `json:"address"`
}

type Address struct {
	Street  string `json:"street"`
	City    string `json:"city"`
	Country string `json:"country"`
}

func main() {
	// Example 1: Direct JSON data
	jsonData := `{
		"config": {
			"version": "1.0.0",
			"features": {
				"darkMode": true,
				"analytics": false
			}
		},
		"stats": {
			"visitors": 1250,
			"countries": ["US", "UK", "FR", "DE", "JP"]
		}
	}`

	// Create provider from JSON
	jsonProvider, err := pathutil.NewRegistryFactProviderFromJSON([]byte(jsonData))
	if err != nil {
		log.Fatalf("Failed to create JSON provider: %v", err)
	}

	// Example 2: Struct data
	user := User{
		Name:     "John Doe",
		Age:      32,
		IsActive: true,
		Roles:    []string{"admin", "editor"},
		Address: Address{
			Street:  "123 Main St",
			City:    "New York",
			Country: "USA",
		},
	}

	// Create provider from struct
	structProvider, err := pathutil.NewRegistryFactProviderFromStruct(user)
	if err != nil {
		log.Fatalf("Failed to create struct provider: %v", err)
	}

	// Example 3: Create a registry with multiple namespaces
	registry := pathutil.NewRegistry()
	registry.Register("app", jsonProvider)
	registry.Register("user", structProvider)

	// Example 4: Path access with direct string syntax

	// Simple path
	version, exists := registry.Get("app.config.version")
	if exists {
		fmt.Printf("App version: %s\n", version)
	}

	// Array access
	country, exists := registry.Get("app.stats.countries[2]")
	if exists {
		fmt.Printf("Third country: %s\n", country)
	}

	// Direct array access
	role, exists := registry.Get("user.roles[0]")
	if exists {
		fmt.Printf("First role: %s\n", role)
	}

	// Nested path
	city, exists := registry.Get("user.address.city")
	if exists {
		fmt.Printf("City: %s\n", city)
	}

	// Feature access
	analytics, exists := registry.Get("app.config.features.analytics")
	if exists {
		fmt.Printf("Analytics enabled: %v\n", analytics)
	}

	// String key access with quoted key
	// Note: For demonstration only, this path doesn't exist in our test data
	_, exists = registry.Get("app.users[\"admin\"]")
	fmt.Printf("Admin exists: %v\n", exists)
}
