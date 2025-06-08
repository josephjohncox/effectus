# Effectus Extension System

The Effectus Extension System provides a comprehensive, unified approach to extending the rule engine with new verbs, schemas, and rules. It supports both static registration (compile-time) and dynamic loading (runtime) through multiple distribution mechanisms.

## Overview

The extension system enables:

- **Unified Extension Loading**: Single framework for all extension types
- **Multiple Distribution Methods**: Local files, JSON manifests, Protocol Buffers, OCI bundles
- **Static and Dynamic Registration**: Compile-time and runtime extension support
- **Type Safety**: Full compile-time verification of extensions
- **Version Management**: Schema evolution and compatibility checking
- **Hot Reloading**: Dynamic updates without service restart

## Architecture

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│                 │     │                 │     │                 │
│ Extension Mgr   │────▶│   Compilation   │────▶│   Execution     │
│                 │     │     System      │     │    Runtime      │
└─────────────────┘     └─────────────────┘     └─────────────────┘
         │                       │                       │
         │                       │                       │
         ▼                       ▼                       ▼
┌───────────────────────────────────────────────────────────────┐
│                                                               │
│                    Unified Extension System                   │
│                                                               │
└───────────────────────────────────────────────────────────────┘
                                │
                ┌───────────────┼───────────────┐
                │               │               │
                ▼               ▼               ▼
         ┌─────────────┐ ┌─────────────┐ ┌─────────────┐
         │    Static   │ │   Dynamic   │ │     OCI     │
         │ Registration│ │    Files    │ │   Bundles   │
         └─────────────┘ └─────────────┘ └─────────────┘
```

### Core Components

1. **ExtensionManager**: Central coordinator for all extension loading
2. **Multiple Loaders**: Static, JSON, Protocol Buffer, OCI bundle support
3. **LoaderAdapter**: Bridges new system to existing registries
4. **VerbExecutor Interface**: Unified interface for verb implementations
5. **Compilation System**: Static validation and type checking
6. **Execution Runtime**: Hot-reload capable runtime system

## Extension Types

### 1. Static Registration (Compile-time)

For extensions known at compile time:

```go
// Verb registration
staticVerbs := loader.NewStaticVerbLoader().
    AddVerb("send_email", &EmailVerbSpec{}).
    AddVerb("log_event", &LogVerbSpec{})

// Schema registration  
staticSchemas := loader.NewStaticSchemaLoader().
    AddSchema("user", userSchema).
    AddSchema("order", orderSchema)

// Load into manager
mgr := loader.NewExtensionManager()
mgr.AddLoader(staticVerbs)
mgr.AddLoader(staticSchemas)
```

### 2. Dynamic Registration (Runtime)

For extensions loaded at runtime:

#### JSON Manifest-based
```bash
# Create manifest
cat > verbs/manifest.json << EOF
{
  "verbs": [
    {"id": 1001, "name": "send_email", "spec_file": "email_spec.json"},
    {"id": 1002, "name": "log_event", "spec_file": "log_spec.json"}
  ]
}
EOF

# Load dynamically
mgr.LoadFromDirectory("./verbs")
```

#### Protocol Buffer-based
```protobuf
// verb_spec.proto
message VerbSpecProto {
  uint32 id = 1;
  string name = 2;
  string capability = 3;
  google.protobuf.Any payload_schema = 4;
}
```

### 3. OCI Bundle Distribution

Package and distribute as OCI artifacts:

```bash
# Create bundle
effectusc bundle \
  --name customer-rules \
  --version 1.2.0 \
  --verbs ./verbs \
  --schemas ./schemas \
  --rules ./rules \
  --oci-ref ghcr.io/myorg/customer-rules:v1.2.0

# Load from OCI
effectusd --oci-ref ghcr.io/myorg/customer-rules:v1.2.0
```

## Verb Implementation Interface

All verb executors implement the unified interface:

```go
type VerbExecutor interface {
    Execute(ctx context.Context, effect Effect) (proto.Message, error)
    Compensate(ctx context.Context, effect Effect, result proto.Message) error
}

// Example implementation
type EmailExecutor struct {
    client emailapi.Client
}

func (e *EmailExecutor) Execute(ctx context.Context, effect Effect) (proto.Message, error) {
    payload := effect.Payload.(*EmailPayload)
    messageID, err := e.client.SendEmail(ctx, payload)
    return &EmailResult{MessageID: messageID}, err
}

func (e *EmailExecutor) Compensate(ctx context.Context, effect Effect, result proto.Message) error {
    emailResult := result.(*EmailResult)
    return e.client.RecallEmail(ctx, emailResult.MessageID)
}
```

## Execution Types

The system supports multiple execution patterns:

### Local Execution
```go
type LocalExecutor struct {
    handler func(ctx context.Context, args map[string]interface{}) (interface{}, error)
}
```

### HTTP Execution
```go
type HTTPExecutor struct {
    client   *http.Client
    endpoint string
    method   string
}
```

### gRPC Execution
```go
type GRPCExecutor struct {
    client grpc.ClientConnInterface
    method string
}
```

### Message Queue Execution
```go
type MessageQueueExecutor struct {
    publisher MessagePublisher
    topic     string
}
```

## Coherent Flow Architecture

The extension system implements a coherent flow: **Load → Compile → Execute**

### 1. Loading Phase
```go
// Load all extensions
extensions, err := mgr.LoadAll(ctx)
if err != nil {
    return fmt.Errorf("failed to load extensions: %w", err)
}
```

### 2. Compilation Phase
```go
// Compile and validate
compiler := compilation.NewExtensionCompiler()
plan, err := compiler.Compile(ctx, extensions)
if err != nil {
    return fmt.Errorf("compilation failed: %w", err)
}
```

### 3. Execution Phase
```go
// Execute with hot-reload capability
runtime := execution.NewExecutionRuntime()
if err := runtime.LoadPlan(plan); err != nil {
    return fmt.Errorf("failed to load execution plan: %w", err)
}
```

## Bundle Structure

Bundles are self-contained packages with versioning and metadata:

```json
{
  "name": "customer-rules",
  "version": "1.2.0",
  "description": "Customer management rules",
  "verbHash": "a1b2c3d4...",
  "createdAt": "2023-06-15T12:34:56Z",
  "verbs": [
    {"name": "send_email", "capability": "external", "spec": "..."}
  ],
  "schemas": [
    {"name": "customer", "format": "protobuf", "definition": "..."}
  ],
  "rules": [
    {"name": "validate_customer", "type": "list", "content": "..."}
  ],
  "requiredFacts": ["customer.name", "customer.email"],
  "piiMasks": ["customer.ssn", "payment.cardNumber"]
}
```

## CLI Integration

The CLI provides comprehensive bundle management:

### Creating Bundles
```bash
effectusc bundle create \
  --name "order-processing" \
  --version "2.1.0" \
  --verbs ./business_verbs \
  --schemas ./schemas \
  --rules ./rules \
  --output bundle.json
```

### Distributing via OCI
```bash
effectusc bundle push \
  --bundle bundle.json \
  --ref ghcr.io/company/order-processing:v2.1.0
```

### Running with Extensions
```bash
# From local bundle
effectusd --bundle ./bundle.json

# From OCI registry with hot-reload
effectusd --oci-ref ghcr.io/company/order-processing:latest --reload-interval 60s

# From directory with automatic discovery
effectusd --extensions-dir ./extensions
```

## Advanced Features

### Hot Reloading
```go
// Enable hot reloading
runtime.EnableHotReload(30 * time.Second)

// Runtime will automatically:
// 1. Check for new bundle versions
// 2. Compile new extensions
// 3. Atomically swap execution plans
// 4. Maintain zero-downtime operation
```

### Capability-based Security
```go
// Verbs declare required capabilities
type VerbSpec struct {
    Name       string
    Capability capability.Type  // Read, Modify, Create, Delete
    // ...
}

// Runtime enforces capability constraints
executor := eval.NewListExecutor(
    verbReg, 
    eval.WithCapabilityRestriction(capability.Read)
)
```

### PII Redaction
```go
// Bundle declares PII fields
bundle.PiiMasks = []string{
    "customer.ssn",
    "payment.cardNumber",
    "user.medicalRecord",
}

// Runtime automatically masks in logs
// Original: {"customer": {"ssn": "123-45-6789"}}
// Logged:   {"customer": {"ssn": "***"}}
```

### Saga Compensation
```go
// Enable compensation for transactional integrity
effectusd --bundle ./bundle.json --saga --saga-store postgres

// On failure, system automatically:
// 1. Logs all successful effects
// 2. Calls compensate() on each executor in reverse order
// 3. Ensures transactional rollback
```

## Integration Examples

### Manufacturing Integration
```go
// Manufacturing-specific executors
registry.Register("reserve_material", &MaterialReservationExecutor{})
registry.Register("schedule_operation", &ProductionScheduleExecutor{})
registry.Register("quality_check", &QualityInspectionExecutor{})
```

### Financial Services Integration
```go
// Finance-specific executors
registry.Register("validate_transaction", &TransactionValidatorExecutor{})
registry.Register("calculate_risk", &RiskCalculatorExecutor{})
registry.Register("send_alert", &ComplianceAlertExecutor{})
```

### E-commerce Integration
```go
// E-commerce-specific executors
registry.Register("check_inventory", &InventoryCheckExecutor{})
registry.Register("process_payment", &PaymentProcessorExecutor{})
registry.Register("ship_order", &ShippingExecutor{})
```

## Benefits

The unified extension system provides:

1. **Consistency**: Single approach for all extension types
2. **Type Safety**: Compile-time verification prevents runtime errors
3. **Flexibility**: Support for both static and dynamic loading
4. **Distribution**: Multiple deployment and distribution options
5. **Evolution**: Safe schema and verb evolution with versioning
6. **Performance**: Hot-reload without service interruption
7. **Security**: Capability-based protection and PII handling
8. **Reliability**: Saga-based compensation for transactional integrity

This comprehensive system enables teams to extend Effectus effectively while maintaining the mathematical guarantees and safety properties that make it suitable for mission-critical systems.

## Future Enhancements

- **Formal Verification**: Static proofs of extension correctness
- **Multi-Language Support**: Extension development in Python, TypeScript, Rust
- **Advanced Caching**: Intelligent caching of compiled extensions
- **Distributed Extensions**: Extensions that span multiple services
- **ML Integration**: Extensions that incorporate machine learning models 