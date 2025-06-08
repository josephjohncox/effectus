package main

import (
	"fmt"

	"github.com/effectus/effectus-go/pathutil"
	"github.com/effectus/effectus-go/schema"
)

// Example business data structures
type Customer struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Email   string  `json:"email"`
	VIP     bool    `json:"vip"`
	Balance float64 `json:"balance"`
	Region  string  `json:"region"`
	IsNew   bool    `json:"isNew"`
}

type OrderItem struct {
	SKU      string  `json:"sku"`
	Quantity int     `json:"quantity"`
	Price    float64 `json:"price"`
}

type Order struct {
	ID       string      `json:"id"`
	Customer Customer    `json:"customer"`
	Items    []OrderItem `json:"items"`
	Total    float64     `json:"total"`
	Status   string      `json:"status"`
}

// createSampleBusinessData generates example business data
func createSampleBusinessData() map[string]interface{} {
	customer := Customer{
		ID:      "cust-123",
		Name:    "Alice Johnson",
		Email:   "alice@example.com",
		VIP:     true,
		Balance: 2500.00,
		Region:  "US-CA",
		IsNew:   false,
	}

	items := []OrderItem{
		{SKU: "WIDGET-001", Quantity: 2, Price: 29.99},
		{SKU: "GADGET-002", Quantity: 1, Price: 199.99},
	}

	order := Order{
		ID:       "order-456",
		Customer: customer,
		Items:    items,
		Total:    259.97,
		Status:   "confirmed",
	}

	// Return flattened data for easy path access
	return map[string]interface{}{
		// Customer data
		"customer.id":      customer.ID,
		"customer.name":    customer.Name,
		"customer.email":   customer.Email,
		"customer.vip":     customer.VIP,
		"customer.balance": customer.Balance,
		"customer.region":  customer.Region,
		"customer.isNew":   customer.IsNew,

		// Order data
		"order.id":       order.ID,
		"order.total":    order.Total,
		"order.status":   order.Status,
		"order.items":    len(items),
		"order.customer": customer, // Nested object

		// System data
		"timestamp": "2024-01-15T10:30:00Z",
		"source":    "order-service",
	}
}

// setupBusinessFunctions registers domain-specific functions
func setupBusinessFunctions(registry *schema.Registry) {
	// Business validation functions
	registry.RegisterFunction("isHighValue", func(amount float64) bool {
		return amount > 1000.0
	})

	registry.RegisterFunction("isVIPCustomer", func(customer map[string]interface{}) bool {
		if vip, ok := customer["vip"].(bool); ok {
			return vip
		}
		return false
	})

	registry.RegisterFunction("calculateDiscount", func(total float64, isVIP bool) float64 {
		if isVIP && total > 500.0 {
			return total * 0.1 // 10% VIP discount
		}
		return 0.0
	})

	registry.RegisterFunction("formatCurrency", func(amount float64) string {
		return fmt.Sprintf("$%.2f", amount)
	})

	// Regional functions
	registry.RegisterFunction("isUSCustomer", func(region string) bool {
		return len(region) >= 2 && region[:2] == "US"
	})
}

// Example of how to set up business facts and schemas
func main() {
	fmt.Println("=== Business Facts Example ===")

	// 1. Create registry
	registry := schema.NewRegistry()

	// 2. Register business functions
	setupBusinessFunctions(registry)

	// 3. Load business data
	businessData := createSampleBusinessData()
	registry.LoadFromMap(businessData)

	// 4. Test business expressions
	expressions := []string{
		"customer.vip == true",
		"order.total > 1000.0",
		"isHighValue(order.total)",
		"isVIPCustomer(order.customer)",
		"customer.region == \"US-CA\"",
		"isUSCustomer(customer.region)",
		"order.items > 1",
		"customer.balance > order.total",
	}

	fmt.Println("\nEvaluating business expressions:")
	for _, expr := range expressions {
		result, err := registry.EvaluateBoolean(expr)
		if err != nil {
			fmt.Printf("  ❌ %s -> ERROR: %v\n", expr, err)
		} else {
			status := "❌"
			if result {
				status = "✅"
			}
			fmt.Printf("  %s %s -> %v\n", status, expr, result)
		}
	}

	// 5. Show calculated values
	fmt.Println("\nCalculated business values:")

	discount, err := registry.EvaluateExpression("calculateDiscount(order.total, customer.vip)")
	if err == nil {
		fmt.Printf("  VIP discount: %v\n", discount)
	}

	formatted, err := registry.EvaluateExpression("formatCurrency(order.total)")
	if err == nil {
		fmt.Printf("  Formatted total: %v\n", formatted)
	}

	// 6. Show pathutil integration
	fmt.Println("\nPathutil integration:")
	factProvider := pathutil.NewRegistryFactProvider()
	factProvider.GetRegistry().LoadFromMap(businessData)

	if customerName, exists := factProvider.Get("customer.name"); exists {
		fmt.Printf("  Customer name: %v\n", customerName)
	}

	if orderTotal, exists := factProvider.Get("order.total"); exists {
		fmt.Printf("  Order total: %v\n", orderTotal)
	}

	// 7. Show type information
	fmt.Println("\nType information:")
	if typeInfo, exists := registry.GetType("customer.balance"); exists {
		fmt.Printf("  customer.balance type: %v\n", typeInfo)
	}

	if typeInfo, exists := registry.GetType("customer.vip"); exists {
		fmt.Printf("  customer.vip type: %v\n", typeInfo)
	}

	fmt.Println("\nThis example shows how to:")
	fmt.Println("  • Load your business data into the registry")
	fmt.Println("  • Register domain-specific functions")
	fmt.Println("  • Evaluate business expressions")
	fmt.Println("  • Integrate with pathutil for fact access")
	fmt.Println("  • In your application, load data from databases, APIs, etc.")
}
