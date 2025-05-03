package verb

import (
	"context"
	"fmt"
)

// StandardVerbExecutor is a simple executor that just returns success
type StandardVerbExecutor struct {
	Name string
	Impl func(ctx context.Context, args map[string]interface{}) (interface{}, error)
}

// Execute implements the VerbExecutor interface
func (e *StandardVerbExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if e.Impl != nil {
		return e.Impl(ctx, args)
	}
	return fmt.Sprintf("%s executed successfully", e.Name), nil
}

// RegisterStandardVerbs registers common verbs with appropriate capabilities
func RegisterStandardVerbs(registry *VerbRegistry) {
	// Read customer verb (read-only, idempotent, commutative)
	registry.Register(&StandardVerbSpec{
		Name:         "ReadCustomer",
		Description:  "Reads customer information",
		Cap:          CapRead | CapIdempotent | CapCommutative,
		Resources:    ResourceSet{{Resource: "customer", Cap: CapRead}},
		ArgTypes:     map[string]string{"id": "string"},
		RequiredArgs: []string{"id"},
		ReturnType:   "Customer",
		ExecutorImpl: &StandardVerbExecutor{Name: "ReadCustomer"},
	})

	// Update customer verb (read-write, idempotent)
	registry.Register(&StandardVerbSpec{
		Name:         "UpdateCustomer",
		Description:  "Updates customer information",
		Cap:          CapReadWrite | CapIdempotent,
		Resources:    ResourceSet{{Resource: "customer", Cap: CapReadWrite}},
		ArgTypes:     map[string]string{"id": "string", "data": "object"},
		RequiredArgs: []string{"id", "data"},
		ReturnType:   "bool",
		InverseVerb:  "RollbackCustomerUpdate",
		ExecutorImpl: &StandardVerbExecutor{Name: "UpdateCustomer"},
	})

	// Create order verb (create, not idempotent)
	registry.Register(&StandardVerbSpec{
		Name:         "CreateOrder",
		Description:  "Creates a new order",
		Cap:          CapCreate,
		Resources:    ResourceSet{{Resource: "order", Cap: CapCreate}},
		ArgTypes:     map[string]string{"customerId": "string", "items": "array"},
		RequiredArgs: []string{"customerId", "items"},
		ReturnType:   "Order",
		InverseVerb:  "CancelOrder",
		ExecutorImpl: &StandardVerbExecutor{Name: "CreateOrder"},
	})

	// Process payment verb (read-write, exclusive - must only run once)
	registry.Register(&StandardVerbSpec{
		Name:        "ProcessPayment",
		Description: "Processes a payment for an order",
		Cap:         CapReadWrite | CapExclusive,
		Resources: ResourceSet{
			{Resource: "order", Cap: CapRead},
			{Resource: "payment", Cap: CapReadWrite},
		},
		ArgTypes:     map[string]string{"orderId": "string", "amount": "float", "method": "string"},
		RequiredArgs: []string{"orderId", "amount", "method"},
		ReturnType:   "PaymentResult",
		InverseVerb:  "RefundPayment",
		ExecutorImpl: &StandardVerbExecutor{Name: "ProcessPayment"},
	})

	// Send notification verb (write, idempotent, commutative)
	registry.Register(&StandardVerbSpec{
		Name:         "SendNotification",
		Description:  "Sends a notification",
		Cap:          CapWrite | CapIdempotent | CapCommutative,
		Resources:    ResourceSet{{Resource: "notification", Cap: CapWrite}},
		ArgTypes:     map[string]string{"recipient": "string", "message": "string"},
		RequiredArgs: []string{"recipient", "message"},
		ReturnType:   "bool",
		ExecutorImpl: &StandardVerbExecutor{Name: "SendNotification"},
	})
}

// RegisterTestVerbs registers a minimal set of verbs for testing
func RegisterTestVerbs(registry *VerbRegistry) {
	// DoSomething verb (simple test verb)
	registry.Register(&StandardVerbSpec{
		Name:         "DoSomething",
		Description:  "Test verb for simple operations",
		Cap:          CapReadWrite,
		Resources:    ResourceSet{{Resource: "test", Cap: CapReadWrite}},
		ArgTypes:     map[string]string{"msg": "string"},
		RequiredArgs: []string{"msg"},
		ReturnType:   "string",
		ExecutorImpl: &StandardVerbExecutor{Name: "DoSomething"},
	})
}
