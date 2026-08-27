# Effectus Extension System

> **Status:** `runtime.ExecutionRuntime` loads `.eff` and `.effx` workflows and lowers them into canonical checked IR.

JSON manifests define verbs and their targets. They do not define workflows.
The extension runtime supports static loaders, JSON manifests, protobuf sources, and OCI bundles.

## Overview

The extension system enables:

- **Unified Extension Loading**: Single framework for all extension types
- **Multiple Distribution Methods**: Local files, JSON manifests, Protocol Buffers, OCI bundles
- **Static and Dynamic Registration**: Compile-time and runtime extension support
- **Type Safety**: Full compile-time verification of extensions
- **Version Management**: Schema evolution and compatibility checking
- **Hot Reloading**: Dynamic updates without service restart

Compiled generations retain registered initial data and function implementations. Workflow fact lookup merges immutable initial data with caller facts. Caller facts override the same path. Current workflow IR does not call registered functions; function implementations remain available as generation metadata.

OCI extension references must use an immutable digest such as `registry.example/extension@sha256:...`.
Mutable tags are rejected. A caller can also require a configured `OCISignatureVerifier`.

Go plugins are trusted native code, not capability sandboxes. Plugin directories and `.so` files must be read-only. The loader rejects writable files, writable directories, links, and non-regular files. Do not mount an untrusted plugin directory.

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

For the current JSON verb manifest format (used by `*.verbs.json` loaders), define capabilities, resources, and required
args explicitly:

```json
{
  "name": "ExternalAPI",
  "version": "1.0.0",
  "description": "HTTP-backed validators",
  "verbs": [
    {
      "name": "ValidateAccount",
      "description": "Calls external validation service",
      "capabilities": ["write", "idempotent"],
      "resources": [
        { "resource": "account_validation", "capabilities": ["write", "idempotent"] }
      ],
      "argTypes": { "accountId": "string" },
      "requiredArgs": ["accountId"],
      "returnType": "ValidationResult",
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

HTTP targets reject loopback, private, link-local, multicast, and unspecified addresses by default.
The HTTP client validates every DNS answer and redirect destination.
Set `allowPrivateNetwork: true` only for a trusted private target.

### Checked workflows

Put each workflow in an `.eff` or `.effx` file.
The JSON verb manifest must not contain a `workflows` field.

```effx
flow "charge-order" priority 10 {
  when { order.id != "" }
  steps {
    receipt = Charge(order_id: order.id, amount: 12500)
    RecordReceipt(receipt: $receipt)
  }
}
```

The extension manager first creates an immutable staged snapshot.
The checked compiler reads only this snapshot.
The compiler does not call filesystem, DNS, HTTP, or OCI loaders.

The compiler validates capabilities, resource subsets, types, required arguments, inverse contracts, result bindings, and fact paths.
It publishes a snapshot only after `ir.Check` accepts the artifact.
A failed reload closes candidate resources and keeps the active snapshot.
An active execution keeps its snapshot until the execution ends.

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

# Resolve the published tag to its immutable digest, then load that digest.
# Example digest shown for syntax only.
effectusd --oci-ref ghcr.io/myorg/customer-rules@sha256:<manifest-digest>
```

### 4. Extension Manifest Resolution

Declare bundle dependencies with semver constraints and checksums:

```json
{
  "name": "customer-stack",
  "version": "0.1.0",
  "effectus": ">=1.4.0",
  "registries": [
    {"name": "public", "base": "ghcr.io/myorg", "default": true}
  ],
  "bundles": [
    {
      "name": "customer-rules",
      "version": "^1.2.0",
      "checksum": "sha256:...",
      "registry": "public"
    }
  ]
}
```

Resolve locally:

```bash
effectusc resolve --registry public=ghcr.io/myorg ./extensions.json
```

## Using Effectus as a Library

The simplest path is to follow the end-to-end example in `examples/fraud_e2e/main.go`. The flow is:

1. **Load schemas** into a `types.TypeSystem` for type checking.
2. **Load verb specs + executors** into a `verb.Registry`.
3. **Compile** `.eff` / `.effx` files with `compiler.NewCompiler()` and the facts/schema adapter.
4. **Execute** with the list or flow runtime (`spec.Execute`) using the verb registry.

The example shows concrete wiring for facts, schema adapters, and executors without extra boilerplate.

## Runtime Loading + Cross-Container Extensions

There are three supported extension protocols today:

1. **JSON manifests** (`loader.NewJSONVerbLoader`, `loader.NewJSONSchemaLoader`)  
2. **Protocol Buffers** (`loader.NewProtoVerbLoader`, `loader.NewProtoSchemaLoader`)  
3. **OCI bundles** (build with `effectusc bundle`, pull with `effectusd --oci-ref` or `effectusc resolve`)

Recommended runtime pattern:

```go
mgr := loader.NewExtensionManager()
mgr.AddLoader(loader.NewJSONVerbLoader("verbs", "./verbs.json"))
mgr.AddLoader(loader.NewJSONSchemaLoader("schema", "./schema.json"))
// OCI bundles are loaded via effectusd --oci-ref or effectusc resolve today.

registry := schema.NewRegistry()
verbRegistry := verb.NewRegistry(nil)
_ = schema.LoadExtensionsIntoRegistries(mgr, registry, verbRegistry)
```

For **cross-container** execution, keep verbs local and call remote services from the executor implementation (HTTP/gRPC/stream). The JSON loader supports `target.type` with `http`, `grpc`, `stream`, `oci`, and `mock`.

For **hot loading**, `runtime.ExecutionRuntime.HotReload` can re-run extension loading and compilation using the same `ExtensionManager` (swap bundles or directories without restart).

## Publishing Verb Extensions (OCI)

Verb executors can live outside the core binary and be loaded at runtime. The OCI loader expects a directory in the
bundle that includes one or more `*.verbs.json` (and optionally `*.schema.json`) files.

Example `extensions/payments.verbs.json`:

```json
{
  "name": "payments",
  "version": "1.0.0",
  "description": "HTTP-backed payment verbs",
  "verbs": [
    {
      "name": "AuthorizePayment",
      "description": "Authorize a card payment",
      "capabilities": ["write", "idempotent"],
      "resources": [{"resource": "payment", "capabilities": ["write", "idempotent"]}],
      "argTypes": {"orderId": "string", "amount": "float", "currency": "string"},
      "requiredArgs": ["orderId", "amount", "currency"],
      "returnType": "string",
      "target": {
        "type": "http",
        "config": {
          "url": "https://payments.internal/authorize",
          "method": "POST",
          "timeout": "5s"
        }
      }
    }
  ]
}
```

Publish the extension bundle with any OCI tooling (for example `oras`):

```bash
oras push ghcr.io/myorg/effectus-extensions:1.0.0 ./extensions
```

Then load it at runtime:

```go
mgr := loader.NewExtensionManager()
ociLoader := loader.NewOCIBundleLoader("payments", "ghcr.io/myorg/effectus-extensions@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
mgr.AddLoader(ociLoader)

rt := runtime.NewExecutionRuntime()
rt.RegisterExtensionLoader(ociLoader)
_ = rt.CompileAndValidate(context.Background())
```

Re-push the OCI tag and call `ExecutionRuntime.HotReload` to swap updated executors without a restart.

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

#### gRPC Verb Manifest Example

Use JSON verbs with `target.type: "grpc"` to call a gRPC service from rules:

```json
{
  "name": "ValidationRPC",
  "version": "1.0.0",
  "verbs": [
    {
      "name": "ValidateAccount",
      "description": "Calls account validation service via gRPC",
      "capabilities": ["write", "idempotent"],
      "argTypes": { "accountId": "string", "amount": "float" },
      "requiredArgs": ["accountId", "amount"],
      "returnType": "ValidationResult",
      "target": {
        "type": "grpc",
        "config": {
          "address": "validation:9090",
          "method": "/validation.v1.ValidationService/Validate",
          "timeout": "5s",
          "insecure": true,
          "metadata": { "x-tenant": "acme" }
        }
      }
    }
  ]
}
```

The gRPC executor uses TLS by default. Set `insecure: true` only for a trusted plaintext endpoint.

The default request and response type is `google.protobuf.Struct`. Example service:

```proto
syntax = "proto3";

package validation.v1;

import "google/protobuf/struct.proto";

service ValidationService {
  rpc Validate(google.protobuf.Struct) returns (google.protobuf.Struct);
}
```

For other protobuf messages, configure `descriptorSet`, `requestType`, and `responseType`.
The executor validates the unary method against the descriptor set before it sends a request.

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

### Durable workflow execution

`effectusd --saga` is rejected because the daemon compatibility executor is not connected to V2.
Use `runtime.ExecutionRuntime.ConfigureDurableWorkflowExecution` and `ExecuteWorkflowWithIdentity`.
The checked runtime commits each step intent before invocation and preserves unknown outcomes instead of claiming rollback.

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
