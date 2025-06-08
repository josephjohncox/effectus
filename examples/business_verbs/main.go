package main

import (
	"context"
	"fmt"
	"log"

	"github.com/effectus/effectus-go/schema/verb"
)

// BusinessVerbExecutor is an example executor for business operations
type BusinessVerbExecutor struct {
	Name string
}

// Execute implements a simple business operation (in real apps, this would interact with databases, APIs, etc.)
func (e *BusinessVerbExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	fmt.Printf("Executing business verb: %s with args: %v\n", e.Name, args)

	// Simulate business logic
	switch e.Name {
	case "ReadCustomer":
		if customerId, ok := args["customerId"].(string); ok {
			return map[string]interface{}{
				"id":    customerId,
				"name":  "Sample Customer",
				"email": "customer@example.com",
			}, nil
		}
	case "ProcessPayment":
		if amount, ok := args["amount"].(float64); ok {
			return map[string]interface{}{
				"success":       true,
				"transactionId": "txn-12345",
				"amount":        amount,
			}, nil
		}
	}

	return map[string]interface{}{"status": "success"}, nil
}

// RegisterBusinessVerbs shows how to register domain-specific verbs
func RegisterBusinessVerbs(registry *verb.VerbRegistry) {
	// Example: E-commerce verbs

	// Read customer verb (read-only, idempotent, commutative)
	registry.Register(&verb.StandardVerbSpec{
		Name:         "ReadCustomer",
		Description:  "Reads customer information",
		Cap:          verb.CapRead | verb.CapIdempotent | verb.CapCommutative,
		Resources:    verb.ResourceSet{{Resource: "customer", Cap: verb.CapRead}},
		ArgTypes:     map[string]string{"customerId": "string"},
		RequiredArgs: []string{"customerId"},
		ReturnType:   "Customer",
		ExecutorImpl: &BusinessVerbExecutor{Name: "ReadCustomer"},
	})

	// Update customer verb (read-write, idempotent)
	registry.Register(&verb.StandardVerbSpec{
		Name:         "UpdateCustomer",
		Description:  "Updates customer information",
		Cap:          verb.CapReadWrite | verb.CapIdempotent,
		Resources:    verb.ResourceSet{{Resource: "customer", Cap: verb.CapReadWrite}},
		ArgTypes:     map[string]string{"customerId": "string", "data": "object"},
		RequiredArgs: []string{"customerId", "data"},
		ReturnType:   "bool",
		InverseVerb:  "RollbackCustomerUpdate",
		ExecutorImpl: &BusinessVerbExecutor{Name: "UpdateCustomer"},
	})

	// Create order verb (create, not idempotent)
	registry.Register(&verb.StandardVerbSpec{
		Name:         "CreateOrder",
		Description:  "Creates a new order",
		Cap:          verb.CapCreate,
		Resources:    verb.ResourceSet{{Resource: "order", Cap: verb.CapCreate}},
		ArgTypes:     map[string]string{"customerId": "string", "items": "array"},
		RequiredArgs: []string{"customerId", "items"},
		ReturnType:   "Order",
		InverseVerb:  "CancelOrder",
		ExecutorImpl: &BusinessVerbExecutor{Name: "CreateOrder"},
	})

	// Process payment verb (read-write, exclusive - must only run once)
	registry.Register(&verb.StandardVerbSpec{
		Name:        "ProcessPayment",
		Description: "Processes a payment for an order",
		Cap:         verb.CapReadWrite | verb.CapExclusive,
		Resources: verb.ResourceSet{
			{Resource: "order", Cap: verb.CapRead},
			{Resource: "payment", Cap: verb.CapReadWrite},
		},
		ArgTypes:     map[string]string{"orderId": "string", "amount": "float", "method": "string"},
		RequiredArgs: []string{"orderId", "amount", "method"},
		ReturnType:   "PaymentResult",
		InverseVerb:  "RefundPayment",
		ExecutorImpl: &BusinessVerbExecutor{Name: "ProcessPayment"},
	})

	// Send notification verb (write-only, idempotent)
	registry.Register(&verb.StandardVerbSpec{
		Name:         "SendNotification",
		Description:  "Sends a notification to a user",
		Cap:          verb.CapWrite | verb.CapIdempotent,
		Resources:    verb.ResourceSet{{Resource: "notification", Cap: verb.CapCreate}},
		ArgTypes:     map[string]string{"to": "string", "message": "string", "type": "string"},
		RequiredArgs: []string{"to", "message"},
		ReturnType:   "bool",
		ExecutorImpl: &BusinessVerbExecutor{Name: "SendNotification"},
	})
}

// Example of how to set up business verbs in your application
func main() {
	fmt.Println("=== Business Verbs Example ===")

	// Create verb registry
	registry := verb.NewVerbRegistry()

	// Register your business-specific verbs
	RegisterBusinessVerbs(registry)

	// Show some registered verbs
	verbNames := []string{"ReadCustomer", "UpdateCustomer", "CreateOrder", "ProcessPayment", "SendNotification"}
	fmt.Printf("Registered business verbs:\n")
	for _, name := range verbNames {
		if spec, exists := registry.GetVerb(name); exists {
			fmt.Printf("  - %s: %s\n", spec.Name, spec.Description)
		}
	}

	// Example: Execute a verb
	if customerVerb, exists := registry.GetVerb("ReadCustomer"); exists {
		result, err := customerVerb.ExecutorImpl.Execute(context.Background(), map[string]interface{}{
			"customerId": "cust-123",
		})
		if err != nil {
			log.Printf("Error: %v", err)
		} else {
			fmt.Printf("ReadCustomer result: %v\n", result)
		}
	}

	fmt.Println("\nThis example shows how to register domain-specific verbs.")
	fmt.Println("In your application, replace BusinessVerbExecutor with real implementations")
	fmt.Println("that interact with your databases, APIs, and business logic.")
}
