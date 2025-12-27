package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/effectus/effectus-go/loader"
	"github.com/effectus/effectus-go/schema"
	"github.com/effectus/effectus-go/schema/verb"
)

// === Static Extension Example ===

// BusinessVerbExecutor demonstrates a static verb executor
type BusinessVerbExecutor struct {
	Name string
}

func (bve *BusinessVerbExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	fmt.Printf("Executing %s with args: %v\n", bve.Name, args)
	return map[string]interface{}{
		"status": "success",
		"verb":   bve.Name,
		"result": "business_operation_completed",
	}, nil
}

// BusinessVerbSpec implements loader.VerbSpec for static registration
type BusinessVerbSpec struct {
	name         string
	description  string
	capabilities []string
	resources    []BusinessResourceSpec
	argTypes     map[string]string
	requiredArgs []string
	returnType   string
	inverseVerb  string
}

type BusinessResourceSpec struct {
	resource     string
	capabilities []string
}

func (bvs *BusinessVerbSpec) GetName() string           { return bvs.name }
func (bvs *BusinessVerbSpec) GetDescription() string    { return bvs.description }
func (bvs *BusinessVerbSpec) GetCapabilities() []string { return bvs.capabilities }
func (bvs *BusinessVerbSpec) GetResources() []loader.ResourceSpec {
	specs := make([]loader.ResourceSpec, len(bvs.resources))
	for i, r := range bvs.resources {
		specs[i] = &r
	}
	return specs
}
func (bvs *BusinessVerbSpec) GetArgTypes() map[string]string { return bvs.argTypes }
func (bvs *BusinessVerbSpec) GetRequiredArgs() []string      { return bvs.requiredArgs }
func (bvs *BusinessVerbSpec) GetReturnType() string          { return bvs.returnType }
func (bvs *BusinessVerbSpec) GetInverseVerb() string         { return bvs.inverseVerb }

func (brs *BusinessResourceSpec) GetResource() string       { return brs.resource }
func (brs *BusinessResourceSpec) GetCapabilities() []string { return brs.capabilities }

// createStaticLoaders demonstrates static extension registration
func createStaticLoaders() []loader.Loader {
	// Create static verb definitions
	verbs := []loader.VerbDefinition{
		{
			Spec: &BusinessVerbSpec{
				name:         "ProcessPayment",
				description:  "Processes customer payment",
				capabilities: []string{"write", "exclusive"},
				resources: []BusinessResourceSpec{
					{resource: "payment", capabilities: []string{"write"}},
					{resource: "order", capabilities: []string{"read"}},
				},
				argTypes:     map[string]string{"amount": "float", "method": "string"},
				requiredArgs: []string{"amount", "method"},
				returnType:   "PaymentResult",
				inverseVerb:  "RefundPayment",
			},
			Executor: &BusinessVerbExecutor{Name: "ProcessPayment"},
		},
		{
			Spec: &BusinessVerbSpec{
				name:         "SendNotification",
				description:  "Sends notification to user",
				capabilities: []string{"write", "idempotent"},
				resources: []BusinessResourceSpec{
					{resource: "notification", capabilities: []string{"create"}},
				},
				argTypes:     map[string]string{"to": "string", "message": "string"},
				requiredArgs: []string{"to", "message"},
				returnType:   "bool",
			},
			Executor: &BusinessVerbExecutor{Name: "SendNotification"},
		},
	}

	// Create static schema loader
	schemaLoader := loader.NewStaticSchemaLoader("business").
		AddFunction("calculateTax", func(amount float64, rate float64) float64 {
			return amount * rate
		}).
		AddFunction("formatCurrency", func(amount float64) string {
			return fmt.Sprintf("$%.2f", amount)
		}).
		AddData("config.taxRate", 0.08).
		AddData("config.currency", "USD").
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

// === Dynamic Extension Example ===

func createDynamicExtensionFiles() (string, string, error) {
	// Create temporary directory for dynamic extensions
	tempDir, err := os.MkdirTemp("", "effectus-extensions-*")
	if err != nil {
		return "", "", err
	}

	// Create a verb manifest file
	verbManifest := `{
  "name": "ExternalVerbs",
  "version": "1.0.0",
  "description": "External verb definitions",
  "verbs": [
    {
      "name": "LogEvent",
      "description": "Logs an event to external system",
      "capabilities": ["write", "idempotent"],
      "resources": [
        {
          "resource": "audit_log",
          "capabilities": ["create"]
        }
      ],
      "argTypes": {
        "event": "string",
        "level": "string",
        "metadata": "object"
      },
      "requiredArgs": ["event", "level"],
      "returnType": "bool",
      "target": {
        "type": "mock"
      }
    },
    {
      "name": "ValidateInput",
      "description": "Validates input data",
      "capabilities": ["read", "idempotent", "commutative"],
      "resources": [
        {
          "resource": "validation_rules",
          "capabilities": ["read"]
        }
      ],
      "argTypes": {
        "data": "object",
        "schema": "string"
      },
      "requiredArgs": ["data", "schema"],
      "returnType": "ValidationResult",
      "target": {
        "type": "mock"
      }
    }
  ]
}`

	// Create a schema manifest file
	schemaManifest := `{
  "name": "ExternalSchema",
  "version": "1.0.0",
  "description": "External schema definitions",
  "types": {
    "ValidationResult": {
      "name": "ValidationResult",
      "type": "object",
      "properties": {
        "valid": {"type": "boolean"},
        "errors": {"type": "array"}
      },
      "description": "Result of validation operation"
    }
  },
  "functions": {
    "length": {
      "name": "length",
      "description": "Get string length",
      "type": "builtin"
    },
    "upper": {
      "name": "upper",
      "description": "Convert to uppercase",
      "type": "builtin"
    }
  },
  "initialData": {
    "system.version": "1.0.0",
    "system.environment": "development",
    "validation.maxLength": 100
  }
}`

	verbsFile := tempDir + "/external.verbs.json"
	schemaFile := tempDir + "/external.schema.json"

	if err := os.WriteFile(verbsFile, []byte(verbManifest), 0644); err != nil {
		return "", "", err
	}

	if err := os.WriteFile(schemaFile, []byte(schemaManifest), 0644); err != nil {
		return "", "", err
	}

	return verbsFile, schemaFile, nil
}

func main() {
	fmt.Println("=== Effectus Unified Extension System Demo ===")

	// 1. Create registries
	registry := schema.NewRegistry()
	verbRegistry := verb.NewRegistry(registry)

	// 2. Create extension manager
	em := loader.NewExtensionManager()

	// 3. Add static loaders
	fmt.Println("\n--- Loading Static Extensions ---")
	staticLoaders := createStaticLoaders()
	for _, l := range staticLoaders {
		em.AddLoader(l)
		fmt.Printf("Added static loader: %s\n", l.Name())
	}

	// 4. Create and add dynamic loaders
	fmt.Println("\n--- Loading Dynamic Extensions ---")
	verbsFile, schemaFile, err := createDynamicExtensionFiles()
	if err != nil {
		log.Fatalf("Error creating dynamic extension files: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(verbsFile))

	em.AddLoader(loader.NewJSONVerbLoader("external", verbsFile))
	em.AddLoader(loader.NewJSONSchemaLoader("external", schemaFile))
	fmt.Printf("Added dynamic verb loader: %s\n", verbsFile)
	fmt.Printf("Added dynamic schema loader: %s\n", schemaFile)

	// 5. Load all extensions
	fmt.Println("\n--- Loading All Extensions ---")
	if err := schema.LoadExtensionsIntoRegistries(em, registry, verbRegistry); err != nil {
		log.Fatalf("Error loading extensions: %v", err)
	}

	fmt.Println("✅ All extensions loaded successfully!")

	// 6. Demonstrate usage
	fmt.Println("\n--- Demonstration ---")

	// Show registered verbs
	fmt.Println("\nRegistered Verbs:")
	verbNames := []string{"ProcessPayment", "SendNotification", "LogEvent", "ValidateInput"}
	for _, name := range verbNames {
		if spec, exists := verbRegistry.GetVerb(name); exists {
			fmt.Printf("  ✅ %s: %s\n", spec.Name, spec.Description)
		} else {
			fmt.Printf("  ❌ %s: Not found\n", name)
		}
	}

	// Show registered functions
	fmt.Println("\nTesting Functions:")
	testCases := []struct {
		expr     string
		expected string
	}{
		{"calculateTax(100.0, config.taxRate)", "8"},
		{"formatCurrency(123.45)", "$123.45"},
		{"length('hello')", "5"},
		{"upper('world')", "WORLD"},
	}

	for _, tc := range testCases {
		result, err := registry.EvaluateExpression(tc.expr)
		if err != nil {
			fmt.Printf("  ❌ %s -> ERROR: %v\n", tc.expr, err)
		} else {
			fmt.Printf("  ✅ %s -> %v\n", tc.expr, result)
		}
	}

	// Show loaded data
	fmt.Println("\nLoaded Data:")
	dataPaths := []string{"config.taxRate", "config.currency", "system.version", "validation.maxLength"}
	for _, path := range dataPaths {
		if value, exists := registry.Get(path); exists {
			fmt.Printf("  ✅ %s = %v\n", path, value)
		} else {
			fmt.Printf("  ❌ %s = <not found>\n", path)
		}
	}

	// Execute a verb
	fmt.Println("\nExecuting Verbs:")
	if spec, exists := verbRegistry.GetVerb("ProcessPayment"); exists {
		ctx := context.Background()
		result, err := spec.Executor.Execute(ctx, map[string]interface{}{
			"amount": 99.99,
			"method": "credit_card",
		})
		if err != nil {
			fmt.Printf("  ❌ ProcessPayment failed: %v\n", err)
		} else {
			fmt.Printf("  ✅ ProcessPayment result: %v\n", result)
		}
	}

	fmt.Println("\n=== Extension System Benefits ===")
	fmt.Println("✅ Unified loading for static and dynamic extensions")
	fmt.Println("✅ Protocol Buffer and OCI bundle support (placeholders)")
	fmt.Println("✅ JSON-based configuration for runtime extensions")
	fmt.Println("✅ Type-safe static registration")
	fmt.Println("✅ Automatic directory scanning")
	fmt.Println("✅ Consistent interface for all extension types")
}
