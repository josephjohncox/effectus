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
func RegisterBusinessVerbs(registry *verb.Registry) {
	// Example: E-commerce verbs

	// Read customer verb (read-only, idempotent, commutative)
	if err := registry.RegisterVerb(&verb.Spec{
		Name:         "ReadCustomer",
		Description:  "Reads customer information",
		Capability:   verb.CapRead | verb.CapIdempotent | verb.CapCommutative,
		Resources:    verb.ResourceSet{{Resource: "customer", Cap: verb.CapRead}},
		ArgTypes:     map[string]string{"customerId": "string"},
		RequiredArgs: []string{"customerId"},
		ReturnType:   "Customer",
		Executor:     &BusinessVerbExecutor{Name: "ReadCustomer"},
	}); err != nil {
		log.Fatalf("Register ReadCustomer: %v", err)
	}

	// Update customer verb (read-write, idempotent)
	if err := registry.RegisterVerb(&verb.Spec{
		Name:         "UpdateCustomer",
		Description:  "Updates customer information",
		Capability:   verb.CapReadWrite | verb.CapIdempotent,
		Resources:    verb.ResourceSet{{Resource: "customer", Cap: verb.CapReadWrite}},
		ArgTypes:     map[string]string{"customerId": "string", "data": "object"},
		RequiredArgs: []string{"customerId", "data"},
		ReturnType:   "bool",
		Inverse:      "RollbackCustomerUpdate",
		Executor:     &BusinessVerbExecutor{Name: "UpdateCustomer"},
	}); err != nil {
		log.Fatalf("Register UpdateCustomer: %v", err)
	}

	// Create order verb (create, not idempotent)
	if err := registry.RegisterVerb(&verb.Spec{
		Name:         "CreateOrder",
		Description:  "Creates a new order",
		Capability:   verb.CapCreate,
		Resources:    verb.ResourceSet{{Resource: "order", Cap: verb.CapCreate}},
		ArgTypes:     map[string]string{"customerId": "string", "items": "array"},
		RequiredArgs: []string{"customerId", "items"},
		ReturnType:   "Order",
		Inverse:      "CancelOrder",
		Executor:     &BusinessVerbExecutor{Name: "CreateOrder"},
	}); err != nil {
		log.Fatalf("Register CreateOrder: %v", err)
	}

	// Process payment verb (read-write, exclusive - must only run once)
	if err := registry.RegisterVerb(&verb.Spec{
		Name:        "ProcessPayment",
		Description: "Processes a payment for an order",
		Capability:  verb.CapReadWrite | verb.CapExclusive,
		Resources: verb.ResourceSet{
			{Resource: "order", Cap: verb.CapRead},
			{Resource: "payment", Cap: verb.CapReadWrite},
		},
		ArgTypes:     map[string]string{"orderId": "string", "amount": "float", "method": "string"},
		RequiredArgs: []string{"orderId", "amount", "method"},
		ReturnType:   "PaymentResult",
		Inverse:      "RefundPayment",
		Executor:     &BusinessVerbExecutor{Name: "ProcessPayment"},
	}); err != nil {
		log.Fatalf("Register ProcessPayment: %v", err)
	}

	// Send notification verb (write-only, idempotent)
	if err := registry.RegisterVerb(&verb.Spec{
		Name:         "SendNotification",
		Description:  "Sends a notification to a user",
		Capability:   verb.CapWrite | verb.CapIdempotent,
		Resources:    verb.ResourceSet{{Resource: "notification", Cap: verb.CapCreate}},
		ArgTypes:     map[string]string{"to": "string", "message": "string", "type": "string"},
		RequiredArgs: []string{"to", "message"},
		ReturnType:   "bool",
		Executor:     &BusinessVerbExecutor{Name: "SendNotification"},
	}); err != nil {
		log.Fatalf("Register SendNotification: %v", err)
	}
}

// Example of how to set up business verbs in your application
func main() {
	fmt.Println("=== Business Verbs Example ===")

	// Create verb registry
	registry := verb.NewRegistry(nil)

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
		result, err := customerVerb.Executor.Execute(context.Background(), map[string]interface{}{
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
