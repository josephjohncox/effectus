package pathutil

import (
	"encoding/json"
	"fmt"
)

// JSONToFactsAdapter converts JSON data to a format usable by the ExprFacts provider
type JSONToFactsAdapter struct {
	// Registry to register the data with
	registry *StructFactRegistry
}

// NewJSONToFactsAdapter creates a new adapter
func NewJSONToFactsAdapter(registry *StructFactRegistry) *JSONToFactsAdapter {
	return &JSONToFactsAdapter{
		registry: registry,
	}
}

// RegisterJSON registers JSON data with a namespace
func (a *JSONToFactsAdapter) RegisterJSON(namespace string, jsonData []byte) error {
	// Parse the JSON data
	var data interface{}
	err := json.Unmarshal(jsonData, &data)
	if err != nil {
		return fmt.Errorf("unmarshaling JSON: %w", err)
	}

	// Register the data with the registry
	a.registry.Register(namespace, data)
	return nil
}

// RegisterJSONMap registers JSON data with a namespace, parsing into a map
func (a *JSONToFactsAdapter) RegisterJSONMap(namespace string, jsonData []byte) error {
	// Parse the JSON data into a map
	var data map[string]interface{}
	err := json.Unmarshal(jsonData, &data)
	if err != nil {
		return fmt.Errorf("unmarshaling JSON to map: %w", err)
	}

	// Register the data with the registry
	a.registry.Register(namespace, data)
	return nil
}

// RegisterJSONStruct registers JSON data with a namespace, parsing into a struct
func (a *JSONToFactsAdapter) RegisterJSONStruct(namespace string, jsonData []byte, target interface{}) error {
	// Parse the JSON data into the target struct
	err := json.Unmarshal(jsonData, target)
	if err != nil {
		return fmt.Errorf("unmarshaling JSON to struct: %w", err)
	}

	// Register the struct with the registry
	a.registry.Register(namespace, target)
	return nil
}

// BuildFactProvider builds a fact provider from the registered data
func (a *JSONToFactsAdapter) BuildFactProvider() FactProvider {
	return a.registry.GetFacts()
}

// JSONCustomStructs demonstrates how to create custom structs for JSON data
type JSONCustomStructs struct {
	// Customer represents a customer entity
	Customer struct {
		ID           string  `json:"id"`
		Name         string  `json:"name"`
		Email        string  `json:"email"`
		Balance      float64 `json:"balance"`
		Status       string  `json:"status"`
		Active       bool    `json:"active"`
		RegisteredAt string  `json:"registered_at"`
	}

	// Order represents an order entity
	Order struct {
		ID            string   `json:"id"`
		CustomerID    string   `json:"customer_id"`
		Total         float64  `json:"total"`
		Status        string   `json:"status"`
		Items         []Item   `json:"items"`
		ShippingInfo  Shipping `json:"shipping"`
		PaymentMethod string   `json:"payment_method"`
		CreatedAt     string   `json:"created_at"`
	}
}

// Item represents an item in an order
type Item struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Price    float64 `json:"price"`
	Quantity int     `json:"quantity"`
}

// Shipping represents shipping information
type Shipping struct {
	Method  string  `json:"method"`
	Address Address `json:"address"`
	Cost    float64 `json:"cost"`
}

// Address represents a shipping address
type Address struct {
	Street  string `json:"street"`
	City    string `json:"city"`
	State   string `json:"state"`
	Country string `json:"country"`
	ZIP     string `json:"zip"`
}

// CreateJSONExample creates an example JSON payload
func CreateJSONExample() []byte {
	// This is just an example of what the JSON might look like
	jsonData := `
	{
		"id": "cust-123",
		"name": "John Smith",
		"email": "john@example.com",
		"balance": 125.50,
		"status": "active",
		"active": true,
		"registered_at": "2023-01-15T10:30:00Z",
		"orders": [
			{
				"id": "ord-456",
				"total": 89.99,
				"status": "shipped",
				"items": [
					{
						"id": "item-789",
						"name": "Widget Pro",
						"price": 49.99,
						"quantity": 1
					},
					{
						"id": "item-012",
						"name": "Gadget Mini",
						"price": 19.99,
						"quantity": 2
					}
				],
				"shipping": {
					"method": "express",
					"address": {
						"street": "123 Main St",
						"city": "Anytown",
						"state": "CA",
						"country": "USA",
						"zip": "12345"
					},
					"cost": 10.00
				},
				"payment_method": "credit_card",
				"created_at": "2023-05-20T14:25:00Z"
			}
		]
	}`

	return []byte(jsonData)
}
