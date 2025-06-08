package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/effectus/effectus-go/loader"
	"github.com/effectus/effectus-go/runtime"
)

// ExampleBusinessExecutor demonstrates a business-specific executor
type ExampleBusinessExecutor struct {
	Name string
}

func (ebe *ExampleBusinessExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	fmt.Printf("🔧 Executing business operation: %s\n", ebe.Name)
	fmt.Printf("   Args: %v\n", args)

	// Simulate business logic
	result := map[string]interface{}{
		"operation": ebe.Name,
		"status":    "completed",
		"timestamp": "2024-01-15T10:30:00Z",
	}

	// Add specific results based on operation
	switch ebe.Name {
	case "ValidatePayment":
		if amount, ok := args["amount"].(float64); ok {
			result["valid"] = amount > 0 && amount <= 10000
			result["amount"] = amount
		}
	case "ProcessPayment":
		result["transactionId"] = "txn-12345"
		result["confirmation"] = "Payment processed successfully"
	case "SendNotification":
		if to, ok := args["to"].(string); ok {
			result["recipient"] = to
			result["delivered"] = true
		}
	}

	return result, nil
}

// ExampleVerbSpec implements loader.VerbSpec
type ExampleVerbSpec struct {
	name         string
	description  string
	capabilities []string
	resources    []ExampleResourceSpec
	argTypes     map[string]string
	requiredArgs []string
	returnType   string
	inverseVerb  string
}

type ExampleResourceSpec struct {
	resource     string
	capabilities []string
}

func (evs *ExampleVerbSpec) GetName() string           { return evs.name }
func (evs *ExampleVerbSpec) GetDescription() string    { return evs.description }
func (evs *ExampleVerbSpec) GetCapabilities() []string { return evs.capabilities }
func (evs *ExampleVerbSpec) GetResources() []loader.ResourceSpec {
	specs := make([]loader.ResourceSpec, len(evs.resources))
	for i, r := range evs.resources {
		specs[i] = &r
	}
	return specs
}
func (evs *ExampleVerbSpec) GetArgTypes() map[string]string { return evs.argTypes }
func (evs *ExampleVerbSpec) GetRequiredArgs() []string      { return evs.requiredArgs }
func (evs *ExampleVerbSpec) GetReturnType() string          { return evs.returnType }
func (evs *ExampleVerbSpec) GetInverseVerb() string         { return evs.inverseVerb }

func (ers *ExampleResourceSpec) GetResource() string       { return ers.resource }
func (ers *ExampleResourceSpec) GetCapabilities() []string { return ers.capabilities }

func createDemoExtensions() []loader.Loader {
	// Static extension with business verbs
	verbs := []loader.VerbDefinition{
		{
			Spec: &ExampleVerbSpec{
				name:         "ValidatePayment",
				description:  "Validates payment information",
				capabilities: []string{"read", "idempotent"},
				resources: []ExampleResourceSpec{
					{resource: "payment", capabilities: []string{"read"}},
				},
				argTypes:     map[string]string{"amount": "float", "method": "string"},
				requiredArgs: []string{"amount"},
				returnType:   "ValidationResult",
			},
			Executor: &ExampleBusinessExecutor{Name: "ValidatePayment"},
		},
		{
			Spec: &ExampleVerbSpec{
				name:         "ProcessPayment",
				description:  "Processes a payment transaction",
				capabilities: []string{"write", "exclusive"},
				resources: []ExampleResourceSpec{
					{resource: "payment", capabilities: []string{"write"}},
					{resource: "account", capabilities: []string{"read"}},
				},
				argTypes:     map[string]string{"amount": "float", "account": "string"},
				requiredArgs: []string{"amount", "account"},
				returnType:   "PaymentResult",
				inverseVerb:  "RefundPayment",
			},
			Executor: &ExampleBusinessExecutor{Name: "ProcessPayment"},
		},
		{
			Spec: &ExampleVerbSpec{
				name:         "SendNotification",
				description:  "Sends notification to user",
				capabilities: []string{"write", "idempotent"},
				resources: []ExampleResourceSpec{
					{resource: "notification", capabilities: []string{"create"}},
				},
				argTypes:     map[string]string{"to": "string", "message": "string", "type": "string"},
				requiredArgs: []string{"to", "message"},
				returnType:   "bool",
			},
			Executor: &ExampleBusinessExecutor{Name: "SendNotification"},
		},
	}

	// Static schema with functions and data
	schemaLoader := loader.NewStaticSchemaLoader("business").
		AddFunction("calculateFee", func(amount float64) float64 {
			return amount * 0.029 // 2.9% processing fee
		}).
		AddFunction("formatCurrency", func(amount float64) string {
			return fmt.Sprintf("$%.2f", amount)
		}).
		AddData("config.maxAmount", 10000.0).
		AddData("config.currency", "USD").
		AddData("config.feeRate", 0.029).
		AddType("PaymentResult", loader.TypeDefinition{
			Name: "PaymentResult",
			Type: "object",
			Properties: map[string]interface{}{
				"success":       map[string]string{"type": "boolean"},
				"transactionId": map[string]string{"type": "string"},
				"amount":        map[string]string{"type": "number"},
			},
			Description: "Result of payment processing",
		})

	return []loader.Loader{
		loader.NewStaticVerbLoader("business", verbs),
		schemaLoader,
	}
}

func createDynamicExtensions() (string, error) {
	// Create dynamic extension file
	dynamicManifest := `{
  "name": "ExternalValidators",
  "version": "1.0.0",
  "description": "External validation verbs",
  "verbs": [
    {
      "name": "ValidateAccount",
      "description": "Validates account information",
      "capabilities": ["read", "idempotent"],
      "resources": [
        {
          "resource": "account",
          "capabilities": ["read"]
        }
      ],
      "argTypes": {
        "accountId": "string",
        "accountType": "string"
      },
      "requiredArgs": ["accountId"],
      "returnType": "ValidationResult",
      "executorType": "mock"
    },
    {
      "name": "CheckFraud",
      "description": "Checks for fraudulent activity",
      "capabilities": ["read", "exclusive"],
      "resources": [
        {
          "resource": "fraud_detection",
          "capabilities": ["read"]
        }
      ],
      "argTypes": {
        "amount": "float",
        "account": "string",
        "location": "string"
      },
      "requiredArgs": ["amount", "account"],
      "returnType": "FraudResult",
      "executorType": "http",
      "executorConfig": {
        "url": "https://api.fraud-detection.com/check",
        "method": "POST",
        "timeout": "5s"
      }
    }
  ]
}`

	tempFile, err := os.CreateTemp("", "dynamic-*.verbs.json")
	if err != nil {
		return "", err
	}
	defer tempFile.Close()

	if _, err := tempFile.WriteString(dynamicManifest); err != nil {
		return "", err
	}

	return tempFile.Name(), nil
}

func main() {
	fmt.Println("🚀 === Effectus Coherent Flow Demo ===")
	fmt.Println()

	ctx := context.Background()

	// 1. Create execution runtime
	fmt.Println("📋 Phase 1: Initialize Runtime")
	rt := runtime.NewExecutionRuntime()
	fmt.Printf("   ✅ Runtime created in state: %s\n", rt.GetRuntimeInfo().State)
	fmt.Println()

	// 2. Register extensions
	fmt.Println("📦 Phase 2: Register Extensions")

	// Static extensions
	staticLoaders := createDemoExtensions()
	for _, loader := range staticLoaders {
		rt.RegisterExtensionLoader(loader)
		fmt.Printf("   ✅ Registered static loader: %s\n", loader.Name())
	}

	// Dynamic extensions
	dynamicFile, err := createDynamicExtensions()
	if err != nil {
		log.Fatalf("❌ Failed to create dynamic extensions: %v", err)
	}
	defer os.Remove(dynamicFile)

	rt.RegisterExtensionLoader(loader.NewJSONVerbLoader("external", dynamicFile))
	fmt.Printf("   ✅ Registered dynamic loader: %s\n", dynamicFile)
	fmt.Printf("   📊 Total loaders: %d\n", rt.GetRuntimeInfo().LoaderCount)
	fmt.Println()

	// 3. Compile and validate
	fmt.Println("🔨 Phase 3: Compile and Validate")
	if err := rt.CompileAndValidate(ctx); err != nil {
		log.Fatalf("❌ Compilation failed: %v", err)
	}

	info := rt.GetRuntimeInfo()
	fmt.Printf("   ✅ Compilation successful!\n")
	fmt.Printf("   📊 Runtime state: %s\n", info.State)
	fmt.Printf("   📊 Verbs compiled: %d\n", info.VerbCount)
	fmt.Printf("   📊 Functions compiled: %d\n", info.FunctionCount)
	fmt.Printf("   📊 Dependencies: %v\n", info.Dependencies)
	fmt.Printf("   📊 Capabilities: %v\n", info.Capabilities)
	fmt.Println()

	// 4. Execute individual verbs
	fmt.Println("⚡ Phase 4: Execute Individual Verbs")

	testCases := []struct {
		verb string
		args map[string]interface{}
	}{
		{
			verb: "ValidatePayment",
			args: map[string]interface{}{
				"amount": 150.00,
				"method": "credit_card",
			},
		},
		{
			verb: "ProcessPayment",
			args: map[string]interface{}{
				"amount":  150.00,
				"account": "acc-12345",
			},
		},
		{
			verb: "SendNotification",
			args: map[string]interface{}{
				"to":      "customer@example.com",
				"message": "Payment processed successfully",
				"type":    "email",
			},
		},
		{
			verb: "ValidateAccount",
			args: map[string]interface{}{
				"accountId":   "acc-12345",
				"accountType": "checking",
			},
		},
	}

	for i, tc := range testCases {
		fmt.Printf("   🎯 Test %d: %s\n", i+1, tc.verb)
		result, err := rt.ExecuteVerb(ctx, tc.verb, tc.args)
		if err != nil {
			fmt.Printf("   ❌ Failed: %v\n", err)
		} else {
			fmt.Printf("   ✅ Success: %v\n", result)
		}
		fmt.Println()
	}

	// 5. Demonstrate error handling
	fmt.Println("🚫 Phase 5: Error Handling")

	// Try invalid verb
	_, err = rt.ExecuteVerb(ctx, "NonExistentVerb", map[string]interface{}{})
	if err != nil {
		fmt.Printf("   ✅ Correctly rejected invalid verb: %v\n", err)
	}

	// Try invalid arguments
	_, err = rt.ExecuteVerb(ctx, "ValidatePayment", map[string]interface{}{})
	if err != nil {
		fmt.Printf("   ✅ Correctly rejected invalid args: %v\n", err)
	}
	fmt.Println()

	// 6. Demonstrate hot reload
	fmt.Println("🔄 Phase 6: Hot Reload")
	if err := rt.HotReload(ctx); err != nil {
		fmt.Printf("   ❌ Hot reload failed: %v\n", err)
	} else {
		fmt.Printf("   ✅ Hot reload successful!\n")
		fmt.Printf("   📊 Runtime state: %s\n", rt.GetRuntimeInfo().State)
	}
	fmt.Println()

	// 7. Summary
	fmt.Println("📋 === Flow Summary ===")
	fmt.Println("✅ 1. Extension Loading - Static and dynamic extensions registered")
	fmt.Println("✅ 2. Compilation - All extensions compiled and validated")
	fmt.Println("✅ 3. Type Checking - Arguments and types validated")
	fmt.Println("✅ 4. Execution - Verbs executed with appropriate executors")
	fmt.Println("✅ 5. Error Handling - Invalid operations properly rejected")
	fmt.Println("✅ 6. Hot Reload - Runtime can be updated without restart")
	fmt.Println()
	fmt.Println("🎉 This demonstrates the coherent flow:")
	fmt.Println("   Load Extensions → Compile & Validate → Execute")
	fmt.Println("   📌 Static validation BEFORE daemon starts")
	fmt.Println("   📌 Clear separation between verb specs and executors")
	fmt.Println("   📌 Support for local, HTTP, message queue execution")
	fmt.Println("   📌 Coherent interfaces throughout the system")
}
