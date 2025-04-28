package main

import (
	"context"
	"fmt"
	"log"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/schema"
	"github.com/effectus/effectus-go/unified"
	"github.com/effectus/examples/common/facts/v1"
)

func main() {
	// Create an example Facts protobuf message
	protoFacts := &facts.Facts{
		Customer: &facts.Customer{
			Id:     "CUST123",
			Name:   "Example Customer",
			Email:  "customer@example.com",
			Region: "EMEA",
			Age:    35,
			Vip:    true,
		},
		Order: &facts.Order{
			Id:         "ORD456",
			CustomerId: "CUST123",
			Total:      99.99,
			Status:     "pending",
			Items: []*facts.OrderItem{
				{
					ProductId: "PROD001",
					Name:      "Product 1",
					Price:     49.99,
					Quantity:  2,
				},
			},
			CreatedAt: 1684147200, // May 15, 2023
		},
	}

	// Create ProtoFacts from the protobuf message
	factsObj := schema.NewProtoFacts(protoFacts)

	// Create a new compiler with proto-aware type checking
	compiler := unified.NewCompiler()

	// Register fact types from the proto message
	ts := schema.NewTypeSystem()
	ts.LoadTypesFromProtoMessage(protoFacts)

	// Print a report of the fact types
	fmt.Println("=== Fact Types from Proto ===")
	reportProtoTypes(factsObj)

	// Compile a rule file
	specFile, err := compiler.CompileFile("../examples/test.eff", factsObj.Schema())
	if err != nil {
		log.Fatalf("Failed to compile rule file: %v", err)
	}

	// Create a simple executor for demonstration
	executor := &SimpleExecutor{Facts: factsObj}

	// Execute the rules
	fmt.Println("\n=== Executing Rules ===")
	err = specFile.Execute(context.Background(), factsObj, executor)
	if err != nil {
		log.Fatalf("Failed to execute rules: %v", err)
	}
}

// SimpleExecutor is a basic implementation of effectus.Executor
type SimpleExecutor struct {
	Facts effectus.Facts
}

// Do implements the effectus.Executor interface
func (e *SimpleExecutor) Do(effect effectus.Effect) (interface{}, error) {
	fmt.Printf("Executing effect: %s\n", effect.Verb)
	fmt.Printf("  Payload: %v\n", effect.Payload)

	// For this example, just return true for all verbs
	return true, nil
}

// reportProtoTypes prints the path and value of each fact in the Facts object
func reportProtoTypes(facts effectus.Facts) {
	// Check some paths
	checkPath(facts, "customer.id")
	checkPath(facts, "customer.name")
	checkPath(facts, "customer.email")
	checkPath(facts, "customer.region")
	checkPath(facts, "customer.age")
	checkPath(facts, "customer.vip")

	checkPath(facts, "order.id")
	checkPath(facts, "order.total")
	checkPath(facts, "order.status")
	checkPath(facts, "order.items")

	// This should work for nested repeated fields too
	if items, exists := facts.Get("order.items"); exists {
		if itemsSlice, ok := items.([]interface{}); ok && len(itemsSlice) > 0 {
			fmt.Println("  order.items[0]:", itemsSlice[0])
		}
	}
}

func checkPath(facts effectus.Facts, path string) {
	value, exists := facts.Get(path)
	if exists {
		fmt.Printf("  %s: %v (%T)\n", path, value, value)
	} else {
		fmt.Printf("  %s: not found\n", path)
	}
}
