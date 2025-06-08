# Buf Integration with Effectus

Effectus uses [Buf](https://buf.build) as the primary tool for managing protobuf schemas, ensuring compatibility, and generating multi-language clients. This integration makes protobuf schemas first-class citizens in the Effectus ecosystem.

## Overview

Buf transforms how Effectus handles:
- **Verb Interface Definitions**: Versioned protobuf schemas for all verb interfaces
- **Fact Schema Management**: Protobuf-based fact schemas with evolution tracking
- **Effect Schema Versioning**: Typed effect definitions with compatibility checking
- **Multi-language Code Generation**: Automatic client generation for Go, Python, TypeScript, Java, and Rust
- **Breaking Change Detection**: Automated schema compatibility validation
- **Schema Registry**: Centralized schema management with the Buf Schema Registry (BSR)

## Core Benefits

### 1. Reduced Boilerplate
Instead of manually managing protobuf compilation:
```bash
# Old way - manual protoc commands
protoc --go_out=. --go-grpc_out=. proto/*.proto

# New way - one command for everything
just buf-generate
```

### 2. Schema Versioning
Buf automatically tracks schema versions and compatibility:
```yaml
# buf.yaml
version: v1
name: buf.build/effectus/effectus
breaking:
  use:
    - FILE
```

### 3. Multi-language Support
Single configuration generates clients for all languages:
```yaml
# buf.gen.yaml
plugins:
  - plugin: buf.build/protocolbuffers/go
  - plugin: buf.build/protocolbuffers/python
  - plugin: buf.build/bufbuild/es  # TypeScript
  - plugin: buf.build/protocolbuffers/java
```

## Architecture

### Schema Organization
```
proto/
├── effectus/
│   └── v1/
│       ├── execution.proto    # Core execution service
│       ├── verbs.proto        # Verb interface management
│       ├── facts.proto        # Fact schema management
│       ├── verbs/
│       │   ├── send_email.proto
│       │   ├── http_request.proto
│       │   └── ...
│       └── facts/
│           ├── user_profile.proto
│           ├── system_event.proto
│           └── ...
```

### Generated Code Structure
```
effectus-go/gen/
├── effectus/v1/
│   ├── execution.pb.go
│   ├── execution_grpc.pb.go
│   ├── verbs.pb.go
│   └── facts.pb.go
clients/
├── python/
├── typescript/
├── java/
└── rust/
```

## Core Integration Components

### 1. BufIntegration Service

The `BufIntegration` service manages the entire schema lifecycle:

```go
type BufIntegration struct {
    workspaceRoot string
    protoDir      string
    genDir        string
    
    verbRegistry *VerbSchemaRegistry
    factRegistry *FactSchemaRegistry
    
    bufConfig *BufConfig
}
```

Key features:
- **Schema Registration**: Register new verb and fact schemas
- **Compatibility Validation**: Check for breaking changes
- **Code Generation**: Generate multi-language clients
- **Proto Generation**: Convert schemas to protobuf definitions

### 2. Schema Registries

#### Verb Schema Registry
Manages versioned verb interface definitions:

```go
type VerbSchema struct {
    Name               string                 `json:"name"`
    Version            string                 `json:"version"`
    Description        string                 `json:"description"`
    InputSchema        map[string]interface{} `json:"input_schema"`
    OutputSchema       map[string]interface{} `json:"output_schema"`
    RequiredCapabilities []string             `json:"required_capabilities"`
    ExecutionType      string                 `json:"execution_type"`
    BufModule          string                 `json:"buf_module"`
    BufCommit          string                 `json:"buf_commit"`
}
```

#### Fact Schema Registry
Manages versioned fact schemas:

```go
type FactSchema struct {
    Name            string                 `json:"name"`
    Version         string                 `json:"version"`
    Schema          map[string]interface{} `json:"schema"`
    Indexes         []IndexDefinition      `json:"indexes"`
    RetentionPolicy *RetentionPolicy       `json:"retention_policy"`
    PrivacyRules    []PrivacyRule          `json:"privacy_rules"`
    BufModule       string                 `json:"buf_module"`
    BufCommit       string                 `json:"buf_commit"`
}
```

## Development Workflow

### 1. Schema Registration

Register a new verb schema:
```bash
just register-verb send_email \
  '{"to": "string", "subject": "string", "body": "string"}' \
  '{"message_id": "string", "status": "string"}'
```

Register a new fact schema:
```bash
just register-fact user_profile \
  '{"user_id": "string", "email": "string", "name": "string", "created_at": "timestamp"}'
```

### 2. Code Generation

Generate code for all registered schemas:
```bash
just buf-generate
```

This creates:
- Go protobuf and gRPC code
- Python client libraries
- TypeScript client libraries
- Java client libraries
- Rust client libraries
- HTML documentation

### 3. Schema Validation

Validate compatibility and breaking changes:
```bash
just buf-validate
```

This runs:
- `buf lint` - Style and consistency checks
- `buf build` - Compilation validation
- `buf breaking` - Breaking change detection

### 4. Development Cycle

Complete development workflow:
```bash
just dev  # format, lint, test, build
```

## Configuration Files

### buf.yaml
Core Buf configuration:
```yaml
version: v1
name: buf.build/effectus/effectus
deps:
  - buf.build/googleapis/googleapis
  - buf.build/protocolbuffers/wellknowntypes
breaking:
  use:
    - FILE
lint:
  use:
    - DEFAULT
  except:
    - UNARY_RPC
  allow_comment_ignores: true
build:
  excludes:
    - examples/
    - tests/
```

### buf.gen.yaml
Code generation configuration:
```yaml
version: v1
managed:
  enabled: true
  go_package_prefix:
    default: github.com/effectus/effectus-go/gen
plugins:
  # Go generation
  - plugin: buf.build/protocolbuffers/go
    out: effectus-go/gen
    opt:
      - paths=source_relative
  - plugin: buf.build/grpc/go
    out: effectus-go/gen
    opt:
      - paths=source_relative
  
  # Multi-language generation
  - plugin: buf.build/protocolbuffers/python
    out: clients/python
  - plugin: buf.build/bufbuild/es
    out: clients/typescript
    opt:
      - target=ts
  - plugin: buf.build/protocolbuffers/java
    out: clients/java
  
  # Documentation
  - plugin: buf.build/bufbuild/doc
    out: docs/proto
    opt:
      - html,index.html
```

### buf.work.yaml
Workspace configuration:
```yaml
version: v1
directories:
  - proto
  - effectus-go/schema/proto
```

## Schema Evolution

### Version Management
Buf automatically manages schema versions using semantic versioning:

```bash
# Check compatibility with previous version
buf breaking --against .git#branch=main

# Push new version to registry
buf push
```

### Breaking Change Detection
Buf detects various types of breaking changes:
- Removed fields or services
- Changed field types or numbers
- Removed enum values
- Changed service signatures

### Migration Support
The `BufIntegration` service provides migration assistance:

```go
type MigrationInfo struct {
    FromVersion     string            `json:"from_version"`
    ToVersion       string            `json:"to_version"`
    MigrationSteps  []MigrationStep   `json:"migration_steps"`
    BackwardCompat  bool              `json:"backward_compatible"`
    MigrationGuide  string            `json:"migration_guide"`
}
```

## Integration with Compilation Flow

### 1. Load Phase
During ruleset loading, schemas are validated against Buf registry:

```go
func (r *ExecutionRuntime) LoadRuleset(ctx context.Context, bundle *loader.RuleBundle) error {
    // Validate fact schema compatibility
    if err := r.bufIntegration.ValidateFactSchema(bundle.FactSchema); err != nil {
        return fmt.Errorf("fact schema validation failed: %w", err)
    }
    
    // Validate verb interface compatibility
    for _, verb := range bundle.VerbSpecs {
        if err := r.bufIntegration.ValidateVerbSchema(verb.Schema); err != nil {
            return fmt.Errorf("verb schema validation failed: %w", err)
        }
    }
    
    return nil
}
```

### 2. Compile Phase
Schema validation is integrated into compilation:

```go
func (c *ExtensionCompiler) CompileRuleset(bundle *loader.RuleBundle) (*CompiledRuleset, error) {
    // Generate protobuf definitions
    if err := c.bufIntegration.GenerateProtoDefinitions(bundle); err != nil {
        return nil, fmt.Errorf("proto generation failed: %w", err)
    }
    
    // Validate compatibility
    if result := c.bufIntegration.ValidateCompatibility(); !result.Valid {
        return nil, fmt.Errorf("schema compatibility validation failed: %v", result.Errors)
    }
    
    return compiled, nil
}
```

### 3. Execute Phase
Runtime execution uses generated protobuf types:

```go
func (r *ExecutionRuntime) ExecuteRuleset(ctx context.Context, req *effectusv1.ExecutionRequest) (*effectusv1.ExecutionResponse, error) {
    // Facts are already typed through protobuf
    typedFacts := req.GetFacts()
    
    // Effects are generated with proper protobuf types
    effects, err := r.engine.Execute(ctx, typedFacts)
    if err != nil {
        return nil, err
    }
    
    return &effectusv1.ExecutionResponse{
        Success: true,
        Effects: effects,
        SchemaInfo: &effectusv1.SchemaInfo{
            FactSchemaVersion: r.bufIntegration.GetFactSchemaVersion(),
            VerbInterfaceVersion: r.bufIntegration.GetVerbInterfaceVersion(),
        },
    }, nil
}
```

## Client Usage Examples

### Go Client
```go
import effectusv1 "github.com/effectus/effectus-go/gen/effectus/v1"

client := effectusv1.NewRulesetExecutionServiceClient(conn)
response, err := client.ExecuteRuleset(ctx, &effectusv1.ExecutionRequest{
    RulesetName: "user_notifications",
    Facts: facts,
})
```

### Python Client
```python
import effectus.v1.execution_pb2 as execution
import effectus.v1.execution_pb2_grpc as execution_grpc

client = execution_grpc.RulesetExecutionServiceStub(channel)
response = client.ExecuteRuleset(execution.ExecutionRequest(
    ruleset_name="user_notifications",
    facts=facts
))
```

### TypeScript Client
```typescript
import { RulesetExecutionServiceClient } from './gen/effectus/v1/execution_connect';

const client = new RulesetExecutionServiceClient(transport);
const response = await client.executeRuleset({
    rulesetName: "user_notifications",
    facts: facts
});
```

## Advanced Features

### 1. Schema Registry Integration
Push schemas to Buf Schema Registry:

```bash
# Authenticate with BSR
buf registry login

# Push schemas
buf push
```

### 2. Private Module Support
For enterprise use with private schemas:

```yaml
# buf.yaml
version: v1
name: buf.build/your-org/effectus-schemas
deps:
  - buf.build/effectus/effectus
  - buf.build/your-org/private-schemas
```

### 3. Custom Validation Rules
Extend Buf validation with custom rules:

```yaml
# buf.yaml
lint:
  use:
    - DEFAULT
  rules:
    - FIELD_LOWER_SNAKE_CASE
    - SERVICE_PASCAL_CASE
    - RPC_PASCAL_CASE
```

### 4. Documentation Generation
Automatically generate documentation:

```bash
# Generate HTML documentation
buf generate --template buf.gen.docs.yaml

# Serve documentation
just docs-serve
```

## Best Practices

### 1. Schema Design
- Use semantic versioning for schema versions
- Design for forward and backward compatibility
- Include comprehensive field documentation
- Use consistent naming conventions

### 2. Dependency Management
- Pin Buf dependencies to specific versions
- Regularly update dependencies
- Test compatibility before updates

### 3. CI/CD Integration
```yaml
# .github/workflows/buf.yml
- name: Buf Validation
  run: |
    buf lint
    buf breaking --against origin/main
    buf generate --template buf.gen.yaml
```

### 4. Performance Optimization
- Use Buf's managed mode for optimal performance
- Enable incremental compilation
- Cache generated code appropriately

## Troubleshooting

### Common Issues

1. **Breaking Change Errors**
   ```bash
   buf breaking --against .git#branch=main
   # Review output and update schema version
   ```

2. **Generation Failures**
   ```bash
   buf generate --debug
   # Check plugin compatibility and configuration
   ```

3. **Import Resolution**
   ```bash
   buf mod update
   # Update dependencies to latest versions
   ```

### Debug Commands
```bash
# Verbose output
buf generate --debug

# Check configuration
buf config ls-files

# Validate workspace
buf workspace ls-files
```

## Migration Guide

### From Manual Protobuf
1. Move existing `.proto` files to `proto/` directory
2. Create `buf.yaml` and `buf.gen.yaml` configurations
3. Update build scripts to use `buf generate`
4. Integrate schema validation into CI/CD

### Schema Versioning
1. Register existing schemas with `BufIntegration`
2. Run compatibility validation
3. Update client code to use generated types
4. Enable breaking change detection

This integration makes Effectus a truly schema-first system where protobuf definitions drive the entire development workflow from compilation to client generation. 