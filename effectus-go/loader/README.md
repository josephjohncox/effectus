# Effectus Extension System

The Extension System provides a unified interface for loading verbs and schemas into Effectus, supporting both static (compile-time) and dynamic (runtime) registration patterns.

## Design Principles

- **Unified Interface**: Same API for static and dynamic extensions
- **Multiple Sources**: Support JSON, Protocol Buffers, and OCI bundles
- **Type Safety**: Strong typing for static registration, validation for dynamic
- **Composable**: Mix and match different loader types
- **Extensible**: Easy to add new loader types

## Quick Start

```go
package main

import (
    "context"
    "github.com/effectus/effectus-go/loader"
    "github.com/effectus/effectus-go/schema"
    "github.com/effectus/effectus-go/schema/verb"
)

func main() {
    // Create registries
    registry := schema.NewRegistry()
    verbRegistry := verb.NewVerbRegistry()
    
    // Create extension manager
    em := loader.NewExtensionManager()
    
    // Add loaders (static and/or dynamic)
    em.AddLoader(createStaticLoader())
    em.AddLoader(loader.NewJSONVerbLoader("external", "verbs.json"))
    
    // Load all extensions
    schema.LoadExtensionsIntoRegistries(em, registry, verbRegistry)
}
```

## Loading Patterns

### 1. Static Registration (Compile-time)

Use when you have Go code that defines your extensions:

```go
// Define your verb executor
type MyExecutor struct {}
func (e *MyExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    // Implementation
}

// Define your verb spec
type MyVerbSpec struct {
    // Implementation of loader.VerbSpec interface
}

// Create static loader
verbs := []loader.VerbDefinition{
    {
        Spec:     &MyVerbSpec{...},
        Executor: &MyExecutor{},
    },
}

loader := loader.NewStaticVerbLoader("my-verbs", verbs)
```

**Benefits:**
- Type safety at compile time
- IDE support with autocomplete
- Performance (no parsing overhead)
- Direct integration with Go business logic

### 2. Dynamic Registration (Runtime)

Use when you want to load extensions from configuration files:

#### JSON Verb Manifest
```json
{
  "name": "BusinessVerbs",
  "version": "1.0.0", 
  "verbs": [
    {
      "name": "ProcessPayment",
      "description": "Processes customer payment",
      "capabilities": ["write", "exclusive"],
      "resources": [
        {
          "resource": "payment",
          "capabilities": ["write"]
        }
      ],
      "argTypes": {
        "amount": "float",
        "method": "string"
      },
      "requiredArgs": ["amount", "method"],
      "returnType": "PaymentResult",
      "executorType": "http",
      "executorConfig": {
        "url": "https://api.payments.com/process",
        "method": "POST"
      }
    }
  ]
}
```

#### JSON Schema Manifest
```json
{
  "name": "BusinessSchema",
  "version": "1.0.0",
  "types": {
    "PaymentResult": {
      "name": "PaymentResult", 
      "type": "object",
      "properties": {
        "success": {"type": "boolean"},
        "transactionId": {"type": "string"}
      }
    }
  },
  "functions": {
    "calculateTax": {
      "name": "calculateTax",
      "type": "builtin"
    }
  },
  "initialData": {
    "config.taxRate": 0.08,
    "config.currency": "USD"
  }
}
```

**Benefits:**
- Runtime configuration
- Non-Go services can provide extensions
- Easy deployment and updates
- Configuration-driven behavior

### 3. Protocol Buffer Registration

Use when you have protobuf-defined extensions:

```go
// Assuming you have a protobuf message defining verbs
loader := loader.NewProtoVerbLoader("proto-verbs", myProtoMessage)
```

**Benefits:**
- Language agnostic definitions
- Strong schema validation
- Integration with existing protobuf workflows
- Version compatibility

### 4. OCI Bundle Registration

Use for distributed extension packages:

```go
loader := loader.NewOCIBundleLoader("external", "registry.io/my-extensions:v1.0.0")
```

**Benefits:**
- Distribution via container registries
- Versioning and signing
- Dependency management
- Hot reloading capabilities

## Directory Scanning

Automatically discover extensions in directories:

```go
// Scan directory for *.verbs.json and *.schema.json files
loaders, err := loader.LoadFromDirectory("./extensions")
if err != nil {
    log.Fatal(err)
}

em := loader.NewExtensionManager()
for _, l := range loaders {
    em.AddLoader(l)
}
```

## Executor Types

### Built-in Executors

- **mock**: Returns mock responses for testing
- **noop**: No-operation executor
- **http**: Makes HTTP calls to external services

### Custom Executors

Implement the `loader.VerbExecutor` interface:

```go
type CustomExecutor struct {
    Config map[string]interface{}
}

func (ce *CustomExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    // Custom implementation
}
```

## Integration Patterns

### Application Startup

```go
func initializeExtensions() (*schema.Registry, *verb.VerbRegistry, error) {
    registry := schema.NewRegistry()
    verbRegistry := verb.NewVerbRegistry()
    
    em := loader.NewExtensionManager()
    
    // Static business logic
    em.AddLoader(createBusinessVerbs())
    em.AddLoader(createBusinessFunctions())
    
    // Dynamic configuration
    if loaders, err := loader.LoadFromDirectory("./config/extensions"); err == nil {
        for _, l := range loaders {
            em.AddLoader(l)
        }
    }
    
    // External packages
    em.AddLoader(loader.NewOCIBundleLoader("external", "registry.io/effectus-extensions:latest"))
    
    // Load everything
    if err := schema.LoadExtensionsIntoRegistries(em, registry, verbRegistry); err != nil {
        return nil, nil, err
    }
    
    return registry, verbRegistry, nil
}
```

### Hot Reloading

```go
func setupHotReload(em *loader.ExtensionManager, registry *schema.Registry, verbRegistry *verb.VerbRegistry) {
    ticker := time.NewTicker(30 * time.Second)
    go func() {
        for range ticker.C {
            // Clear existing extensions
            registry.ClearAll()
            // Note: verb registry doesn't have clear - would need implementation
            
            // Reload all extensions
            schema.LoadExtensionsIntoRegistries(em, registry, verbRegistry)
        }
    }()
}
```

### Testing

```go
func TestExtensions(t *testing.T) {
    // Create test extension from string
    manifest := `{"name": "test", "verbs": [...]}`
    loader, err := loader.LoadExtensionsFromReader(
        strings.NewReader(manifest), 
        ".verbs.json",
    )
    require.NoError(t, err)
    
    em := loader.NewExtensionManager()
    em.AddLoader(loader)
    
    // Test loading
    registry := schema.NewRegistry()
    verbRegistry := verb.NewVerbRegistry()
    err = schema.LoadExtensionsIntoRegistries(em, registry, verbRegistry)
    require.NoError(t, err)
    
    // Verify extensions loaded correctly
    _, exists := verbRegistry.GetVerb("TestVerb")
    assert.True(t, exists)
}
```

## Best Practices

### 1. Layered Loading
Load extensions in layers from most general to most specific:
1. Core business extensions (static)
2. Environment-specific configuration (dynamic)
3. Customer-specific customizations (OCI/external)

### 2. Error Handling
Always validate extensions before production use:
```go
if err := em.LoadExtensions(ctx, adapter); err != nil {
    log.Printf("Extension loading failed: %v", err)
    // Fall back to default behavior or fail fast
}
```

### 3. Security
- Validate all dynamic extension sources
- Use signed OCI bundles in production
- Implement capability-based security
- Audit extension loading

### 4. Performance
- Static extensions have no runtime overhead
- Cache compiled dynamic extensions
- Use batch loading for multiple extensions
- Monitor extension loading performance

## Extension File Conventions

- `*.verbs.json` - Verb definitions
- `*.schema.json` - Schema and function definitions  
- `*.proto` - Protocol Buffer definitions
- Follow semantic versioning for extension packages

## See Also

- [Extension System Example](../../examples/extension_system/)
- [Verb System Documentation](../schema/verb/)
- [Schema Registry Documentation](../schema/)
- [OCI Bundle Specification](../../docs/oci-bundles.md) 