# Effectus — Typed, Deterministic Rule Engine

*Production-grade rule execution with mathematical foundations*

## ✨ What is Effectus?

Effectus is a strongly-typed rule engine that transforms live data (Facts) into safe, deterministic actions (Effects). Built on category theory foundations, it provides **static validation** and **coherent execution flow** from extension loading through compilation to runtime.

### Key Features

- **🔒 Static Validation**: All rules validated at compile-time, not runtime
- **📊 Strongly Typed**: Facts and verbs defined with Protocol Buffers
- **🏗️ Coherent Flow**: Load Extensions → Compile & Validate → Execute
- **⚡ Multiple Executors**: Local, HTTP, gRPC, message queue execution
- **🔄 Hot Reload**: Update rules without restarting services
- **🔐 Capability System**: Fine-grained security and resource control
- **📦 Bundle System**: Package and distribute via OCI registries

## Core Architecture

```
Load Extensions → Compile & Validate → Execute
     ↓                    ↓               ↓
   Static            Type Checking    Executors
   Dynamic           Dependencies     (Local/HTTP/gRPC)
   OCI Bundles       Capabilities     Message Queues
```

## Quick Start

### 1. Install Effectus

```bash
go install github.com/effectus/effectus-go/cmd/effectusc@latest
go install github.com/effectus/effectus-go/cmd/effectusd@latest
```

### 2. Define Facts (Protocol Buffers)

```protobuf
// facts/customer/v1/customer.proto
message Customer {
  string code = 1;
  string email = 2;
  bool is_new = 3;
}
```

### 3. Write Rules

```go
// Simple business rule example
rule "welcome_new_customer" {
    when {
        customer.is_new == true
        customer.email != ""
    }
    then {
        SendEmail(
            to: customer.email,
            subject: "Welcome!",
            template: "welcome"
        )
    }
}
```

### 4. Run as a Library

```go
package main

import (
    "context"
    "log"

    "github.com/effectus/effectus-go/runtime"
    "github.com/effectus/effectus-go/loader"
)

func main() {
    // Create runtime with coherent flow
    rt := runtime.NewExecutionRuntime()
    
    // Register extensions (static, JSON, Proto, OCI)
    rt.RegisterExtensionLoader(loader.NewJSONSchemaLoader("core", "schemas/core.schema.json"))
    rt.RegisterExtensionLoader(loader.NewJSONVerbLoader("core", "verbs/core.verbs.json"))
    
    // Compile and validate BEFORE starting daemon
    if err := rt.CompileAndValidate(context.Background()); err != nil {
        log.Fatal("Compilation failed:", err)
    }
    
    // Execute rules
    result, err := rt.ExecuteVerb(context.Background(), "SendEmail", map[string]interface{}{
        "to": "user@example.com",
        "subject": "Welcome!",
        "template": "welcome",
    })
    _ = result
    _ = err
}
```

For compile‑time Go executors, use `loader.NewStaticVerbLoader(...)` instead of JSON loaders.

## Architecture Overview

### Extension System

**Multiple Loading Patterns**:
- **Static**: Compile-time registration for Go code
- **Dynamic**: Runtime loading from JSON, Protocol Buffers
- **OCI Bundles**: Distributed packages with versioning

### Compilation System

**Static Validation Before Runtime**:
- Type checking for all verbs and arguments
- Dependency resolution and validation  
- Capability verification and security checks
- Execution plan optimization

### Execution System

**Coherent Executor Interface**:
- **Local**: In-process execution
- **HTTP**: Remote API calls with retry policies
- **gRPC**: Typed remote procedure calls
- **Message**: Queue-based async execution
- **Mock**: Testing and development

## Extension Examples

### Static Extension

```go
// Register verbs at compile-time
verbs := []loader.VerbDefinition{
    {
        Spec: &VerbSpec{
            Name: "SendEmail",
            ArgTypes: map[string]string{
                "to": "string",
                "subject": "string",
            },
            ReturnType: "bool",
        },
        Executor: &EmailExecutor{},
    },
}

loader := loader.NewStaticVerbLoader("business", verbs)
runtime.RegisterExtensionLoader(loader)
```

### Dynamic Extension

```json
{
  "name": "ExternalAPI",
  "verbs": [
    {
      "name": "ValidateAccount", 
      "argTypes": {"accountId": "string"},
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

If `target` is omitted, verbs default to **stream** emission (stdout publisher).

### Load Bundles (CLI or Library)

```bash
# Run with local bundle
effectusd --bundle bundle.json

# Run with OCI registry
effectusd --oci-ref ghcr.io/myorg/rules:v1.0.0
```

```go
// Load bundles directly in Go (metadata + summaries)
import "github.com/effectus/effectus-go/unified"

bundle, _ := unified.LoadBundle("bundle.json")
puller := unified.NewOCIBundlePuller("./bundles")
bundle, _ = puller.Pull("ghcr.io/myorg/rules:v1.0.0")
_ = bundle
```

## CLI Usage

```bash
# Compile and validate rules
effectusc compile rules/ --output compiled.json

# Create a bundle (local or OCI)
effectusc bundle --name fraud --version 1.0.0 --schema-dir schemas --verb-dir verbs --rules-dir rules --output bundle.json

# Run with OCI registry + hot reload
effectusd --oci-ref ghcr.io/myorg/rules:latest --reload-interval 60s
```

## Publishing Artifacts (OCI + Helm)

```bash
# Publish a rules bundle to GHCR
effectusc bundle \
  --name fraud-demo \
  --version 1.0.0 \
  --schema-dir examples/fraud_e2e/schema \
  --verb-dir examples/fraud_e2e/verbs \
  --rules-dir examples/fraud_e2e/rules \
  --oci-ref ghcr.io/myorg/bundles/fraud-demo:1.0.0

# Publish the runtime image
docker build -t ghcr.io/myorg/effectusd:v1.0.0 .
docker push ghcr.io/myorg/effectusd:v1.0.0

# Install with Helm (OCI chart)
helm install effectusd oci://ghcr.io/myorg/helm/effectusd \
  --version 1.0.0 \
  --set bundle.ociRef=ghcr.io/myorg/bundles/fraud-demo:1.0.0
```

See `docs/EXTENSION_SYSTEM.md` for verb extension publishing and hot reload examples.

## Status UI and Playground

Effectus includes a lightweight status UI with rule listing, dependency graph, and dry-run playground. By default,
`effectusd` enables token auth for `/api/*` endpoints (it prints a generated token if you don’t provide one). You can
override per-endpoint roles via `--api-acl-file`.

Example ACL: `docs/acl.example.yml`.

The fact store is treated as a cache; use `--facts-cache-policy lru` with size limits to prevent unbounded growth.

```bash
effectusd --bundle compiled.json --http-addr :8080 --api-token devtoken
# open http://localhost:8080/ui
```

Post facts for a live universe snapshot:

```bash
curl -X POST http://localhost:8080/api/facts \\
  -H 'Authorization: Bearer devtoken' \\
  -H 'Content-Type: application/json' \\
  -d '{"universe":"prod","facts":{"customer":{"tier":"gold"},"order":{"total":120}}}'
```

## Adapters Quick Start

1. Define fact schemas (Proto or JSON) and register them with the type system.
2. Configure one or more `fact_sources` using adapter types (Kafka, CDC, S3, etc.).
3. Start `effectusd` with your bundle and adapter config.

Adapters available: `kafka`, `postgres_poller`, `postgres_cdc`, `mysql_cdc`, `amqp`, `grpc`, `redis_streams`,
`http`, `file_watcher`, `sql`, `s3`, `iceberg`.

## Warehouse Devstack

Run a local Trino + Iceberg + MinIO stack for warehouse adapters and Parquet ingestion.

```bash
just devstack-up
just devstack-seed-iceberg
just devstack-seed-parquet
just devstack-smoke-test
just devstack-trino-cli
just devstack-down
```

## CDC Demos and Integration Tests

```bash
just cdc-up
go run ./examples/cdc_all
just cdc-test
just cdc-down
```

Note: CDC demos and tests require Docker.

## Mathematical Foundations

Effectus is built on solid mathematical principles:

- **List Rules**: Modeled as free monoids (sequential effects)
- **Flow Rules**: Modeled as free monads (composable effects with branching)
- **Type System**: Category-theoretic approach to type safety
- **Capability System**: Lattice-based security model

See [docs/theory/](docs/theory/) for detailed mathematical foundations.

## Project Structure

```
.
├── compiler/       ← Extension compilation & validation
├── runtime/        ← Execution runtime with coherent flow
├── loader/         ← Extension loading (static/dynamic/OCI)
├── schema/         ← Type system & registries
├── cmd/
│   ├── effectusc/  ← Compiler CLI
│   └── effectusd/  ← Runtime daemon
└── examples/       ← Usage examples
```

## Documentation

| Document | Purpose |
|----------|---------|
| [Architecture](docs/ARCHITECTURE.md) | High-level system design |
| [System Intent](docs/SYSTEM_INTENT.md) | Design intent and correctness criteria |
| [Glossary](docs/GLOSSARY.md) | Shared vocabulary |
| [Coherent Flow](docs/coherent_flow.md) | Extension → Compilation → Execution flow |
| [Basics](docs/BASICS.md) | Core concepts: Facts, Verbs, Effects |
| [Verb System](docs/VERB_SYSTEM.md) | Verb specifications and executors |
| [Bundle System](docs/BUNDLE_SYSTEM.md) | Packaging and distribution |
| [Commands](docs/COMMANDS.md) | CLI reference |
| [Design](docs/design.md) | Comprehensive technical design |

## Key Benefits

### 🚫 **Fail-Fast Validation**
- All errors caught before daemon starts
- No runtime surprises from configuration issues
- Clear error messages with suggestions

### 🔧 **Flexible Execution**  
- Same verb can run locally or remotely
- Easy testing with mock executors
- Support for distributed systems

### ⚡ **Performance**
- Optimized execution plans
- Parallel execution support
- Efficient dependency resolution

### 🔐 **Security**
- Capability-based access control
- Static verification of permissions
- Resource isolation and protection

## Getting Started

1. **[Install Effectus](#1-install-effectus)**
2. **Define your fact schemas** using Protocol Buffers
3. **Write rules** expressing your business logic
4. **Register extensions** (static or dynamic)
5. **Compile and validate** before deployment
6. **Run the daemon** to execute rules against live facts

Check out [examples/coherent_flow/](examples/coherent_flow/) for a complete working example.

## Development Status

Effectus is under active development. Current focus areas:

- ✅ Core execution engine and type system
- ✅ Extension loading and compilation system  
- ✅ Basic CLI tools and runtime daemon
- 🚧 OCI bundle distribution system
- 🚧 Advanced execution policies and saga compensation
- 📋 Production monitoring and observability

## Contributing

We welcome contributions! Please see `CONTRIBUTING.md` for workflow details and `SECURITY.md` for vulnerability reporting.

## License

[MIT License](LICENSE) - see LICENSE file for details.
