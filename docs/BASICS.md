# Effectus Basics

This document explains the fundamental concepts of Effectus: Facts, Verbs, Effects, and the coherent flow architecture.

## Core Concepts

### Facts
**Facts** represent the current state of your domain - customer data, inventory levels, machine status, etc. They are:
- **Strongly typed** using Protocol Buffers
- **Immutable** once created
- **Extensible** without breaking existing rules
- **Versioned** for schema evolution

### Verbs  
**Verbs** define *what actions can be taken*. They are:
- **Specifications** that declare argument types, return types, and capabilities
- **Statically defined** and versioned as a closed set
- **Capability-protected** for security and resource control
- **Executable** through multiple execution strategies (local, HTTP, gRPC, message queues)

### Effects
**Effects** are the *actual execution* of verbs with specific arguments. They are:
- **Idempotent** where possible for safe retry
- **Atomic** operations that either succeed or fail completely
- **Ordered** based on dependencies and capabilities
- **Compensatable** for saga-style transaction rollback

## Coherent Flow Architecture

Effectus follows a coherent flow from extension loading through compilation to execution:

```
Extension Loading → Compilation & Validation → Execution
       ↓                     ↓                    ↓
  Static/Dynamic        Type Checking        Multiple Executors
  JSON/Proto/OCI       Dependencies         (Local/HTTP/gRPC)
  Directory Scanning   Capabilities         Message Queues
```

### 1. Extension Loading
- **Static Registration**: Compile-time verb definitions in Go
- **Dynamic Loading**: Runtime loading from JSON/Protocol Buffer files
- **OCI Bundles**: Distributed packages with versioning
- **Directory Scanning**: Automatic discovery of extension files

### 2. Compilation & Validation
- **Type Checking**: Validate all verb arguments and return types
- **Dependency Resolution**: Ensure all required verbs are available
- **Capability Verification**: Check security constraints
- **Execution Planning**: Create optimized execution strategies

### 3. Runtime Execution
- **Executor Selection**: Choose appropriate execution strategy
- **State Management**: Track runtime state with hot-reload capability
- **Error Handling**: Compensation and retry policies
- **Observability**: Metrics, tracing, and structured logging

## Fact System

Facts use Protocol Buffers for strong typing and extensibility:

```protobuf
// customer/v1/customer.proto
message Customer {
  string code = 1;
  string email = 2;
  bool is_new = 3;
  string tier = 4;
}

// inventory/v1/item.proto  
message InventoryItem {
  string sku = 1;
  int32 quantity = 2;
  double unit_cost = 3;
  bool low_stock = 4;
}
```

If `target` is omitted, verbs default to stream emission (stdout).

### Schema Composition

Facts are composed from multiple modules:

```protobuf
message Facts {
  customer.v1.Customer customer = 1;
  inventory.v1.InventoryItem inventory = 2;
  // Add new modules without breaking existing rules
  sensors.v1.Temperature temperature = 3;
}
```

### Path Resolution

Rules reference facts using dot notation:
- `customer.email` → string
- `inventory.quantity` → int32  
- `temperature.ambient_c` → double

The compiler validates all paths at compilation time.

## Verb System

### Verb Specifications

Verbs are defined as specifications that declare their contract:

```go
type VerbSpec interface {
    GetName() string
    GetCapabilities() []string
    GetArgTypes() map[string]string
    GetReturnType() string
    GetResources() []ResourceSpec
}
```

### Static Registration

```go
verbs := []loader.VerbDefinition{
    {
        Spec: &VerbSpec{
            Name: "SendEmail",
            ArgTypes: map[string]string{
                "to": "string",
                "subject": "string", 
                "body": "string",
            },
            ReturnType: "bool",
            Capabilities: []string{"write", "idempotent"},
        },
        Executor: &EmailExecutor{},
    },
}

loader := loader.NewStaticVerbLoader("communication", verbs)
runtime.RegisterExtensionLoader(loader)
```

### Dynamic Registration

```json
{
  "name": "BusinessVerbs",
  "verbs": [
    {
      "name": "ValidateAccount",
      "argTypes": {
        "accountId": "string",
        "accountType": "string"
      },
      "returnType": "ValidationResult",
      "capabilities": ["read", "idempotent"],
      "target": {
        "type": "http",
        "config": {
          "url": "https://api.validation.com/check",
          "method": "POST",
          "timeout": "5s"
        }
      }
    }
  ]
}
```

## Capability System

Verbs declare required capabilities for security and resource control:

### Capability Levels
```go
const (
    CapRead   Capability = 1 << iota  // Read-only operations
    CapWrite                          // Modify existing resources  
    CapCreate                         // Create new resources
    CapDelete                         // Delete resources
)
```

### Capability Lattice
Capabilities form a lattice: `Read ≤ Write ≤ Create ≤ Delete`

### Resource Protection
```go
type ResourceSpec interface {
    GetResource() string      // e.g., "customer", "inventory"
    GetCapabilities() []string // e.g., ["read", "write"]
}
```

Resources are protected by capability and key:
- **Capability**: What level of access is required
- **Key**: Which specific resource instance (e.g., customer ID)

## Execution Models

### Local Execution
Verbs execute directly in the Effectus process:
```go
type LocalExecutor struct {
    impl VerbExecutor
}

func (e *LocalExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    return e.impl.Execute(ctx, args)
}
```

### Remote Execution
Verbs execute via external systems:
```go
type HTTPExecutor struct {
    config *HTTPExecutorConfig
}

func (e *HTTPExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    // Make HTTP request to external API
    return httpCall(e.config.URL, args)
}
```

### Executor Selection
The compilation process determines the appropriate executor based on:
- **Verb specification**: What execution type is configured
- **Runtime environment**: What executors are available
- **Policy configuration**: Security and performance constraints

## Rule Structure

Rules define when effects should be triggered:

```go
rule "low_inventory_alert" {
    when {
        inventory.quantity < 10
        inventory.low_stock == false
    }
    then {
        SendEmail(
            to: "warehouse@company.com",
            subject: "Low Inventory Alert",
            body: "Item " + inventory.sku + " is running low"
        )
        UpdateInventoryFlag(
            sku: inventory.sku,
            low_stock: true
        )
    }
}
```

## Type Safety

### Compile-Time Validation
- All fact paths validated against schemas
- Verb arguments checked against specifications
- Return types verified for consistency
- Dependencies resolved and validated

### Runtime Safety  
- Argument types validated before execution
- Resource access checked against capabilities
- Error handling with compensation support
- Execution traced for observability

## Extensibility

### Schema Evolution
Add new fact types without breaking existing rules:
```protobuf
// Add new fields to existing types
message Customer {
  string code = 1;
  string email = 2;
  bool is_new = 3;
  string tier = 4;
  // New field - doesn't break existing rules
  google.protobuf.Timestamp last_login = 5;
}

// Add entirely new fact types
message SensorData {
  double temperature = 1;
  double humidity = 2;
  google.protobuf.Timestamp reading_time = 3;
}
```

### Verb Extension
Add new verbs through the extension system:
- **Development**: Register statically during development
- **Configuration**: Load dynamically from JSON/Proto files
- **Distribution**: Package in OCI bundles for deployment

### Multiple Deployment Patterns
- **Embedded**: Include Effectus as a library in Go applications
- **Sidecar**: Run as separate process alongside applications  
- **Service**: Deploy as standalone rule execution service
- **Multi-language**: Connect from any language via Protocol Buffers

This foundation provides a robust, extensible system for rule-based automation while maintaining mathematical rigor and practical usability.
