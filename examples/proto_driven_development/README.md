# Proto-Driven Development with Effectus

This example demonstrates how developers use protobuf schemas as the **single source of truth** for building Effectus services. Instead of JSON schemas that get converted, you define verbs and facts directly as `.proto` files and leverage the entire buf ecosystem.

## 🎯 Why Proto-First?

**Traditional Approach (JSON → Proto)**:
```json
// verb.json (gets converted to proto)
{
  "name": "send_email",
  "input": {"to": "string", "subject": "string"},
  "output": {"message_id": "string"}
}
```

**Proto-First Approach**:
```protobuf
// send_email.proto (source of truth)
message SendEmailInput {
  string to = 1;
  string subject = 2;
}
```

## 🏗️ Benefits

1. **Type Safety**: Real protobuf types from day one
2. **Multi-Language**: Automatic Go, Python, TypeScript, Java, Rust clients
3. **Versioning**: Buf semantic versioning and breaking change detection
4. **Compatibility**: Forward/backward compatibility built-in
5. **Registry**: Teams can use Buf Schema Registry or git workflows
6. **Tooling**: Full buf ecosystem (lint, format, generate, push)

## 🚀 Developer Workflow

### 1. Define Schemas as Proto Files

Create your verb interfaces directly as protobuf:

```bash
# Create a new verb interface
mkdir -p proto/acme/v1/verbs
cat > proto/acme/v1/verbs/send_notification.proto << 'EOF'
syntax = "proto3";
package acme.v1.verbs;
import "google/protobuf/timestamp.proto";

message SendNotificationInput {
  string user_id = 1;
  string message = 2;
  NotificationType type = 3;
}

message SendNotificationOutput {
  string notification_id = 1;
  google.protobuf.Timestamp sent_at = 2;
  DeliveryStatus status = 3;
}

enum NotificationType {
  NOTIFICATION_TYPE_UNSPECIFIED = 0;
  NOTIFICATION_TYPE_EMAIL = 1;
  NOTIFICATION_TYPE_SMS = 2;
  NOTIFICATION_TYPE_PUSH = 3;
}

enum DeliveryStatus {
  DELIVERY_STATUS_UNSPECIFIED = 0;
  DELIVERY_STATUS_QUEUED = 1;
  DELIVERY_STATUS_SENT = 2;
  DELIVERY_STATUS_DELIVERED = 3;
}
EOF
```

Create your fact schemas directly as protobuf:

```bash
# Create a new fact schema
mkdir -p proto/acme/v1/facts
cat > proto/acme/v1/facts/user_event.proto << 'EOF'
syntax = "proto3";
package acme.v1.facts;
import "google/protobuf/timestamp.proto";

message UserEvent {
  string user_id = 1;
  string event_type = 2;
  google.protobuf.Timestamp timestamp = 3;
  map<string, string> properties = 4;
  EventContext context = 5;
}

message EventContext {
  string session_id = 1;
  string ip_address = 2;
  string user_agent = 3;
  optional string referrer = 4;
}
EOF
```

### 2. Configure Buf for Your Organization

```yaml
# buf.yaml
version: v1
name: buf.build/acme/effectus-schemas
deps:
  - buf.build/effectus/effectus  # Base Effectus types
  - buf.build/googleapis/googleapis
breaking:
  use:
    - FILE
lint:
  use:
    - DEFAULT
  rules:
    - FIELD_LOWER_SNAKE_CASE
    - ENUM_PASCAL_CASE
```

### 3. Generate Multi-Language Clients

```bash
# Generate Go, Python, TypeScript, Java clients
buf generate

# Results in:
# gen/go/acme/v1/verbs/send_notification.pb.go
# gen/python/acme/v1/verbs/send_notification_pb2.py  
# gen/typescript/acme/v1/verbs/SendNotification_pb.ts
# gen/java/acme/v1/verbs/SendNotificationProto.java
```

### 4. Use Generated Types in Your Service

**Go Service Implementation**:
```go
package main

import (
    verbsv1 "acme.com/gen/go/acme/v1/verbs"
    factsv1 "acme.com/gen/go/acme/v1/facts"
    effectusv1 "github.com/effectus/effectus-go/gen/effectus/v1"
)

type NotificationService struct {
    // Your service dependencies
}

func (s *NotificationService) Execute(
    ctx context.Context,
    req *verbsv1.SendNotificationInput,
) (*verbsv1.SendNotificationOutput, error) {
    // Type-safe implementation using generated types
    switch req.Type {
    case verbsv1.NotificationType_NOTIFICATION_TYPE_EMAIL:
        return s.sendEmail(ctx, req)
    case verbsv1.NotificationType_NOTIFICATION_TYPE_SMS:
        return s.sendSMS(ctx, req)
    default:
        return nil, fmt.Errorf("unsupported notification type: %v", req.Type)
    }
}

func (s *NotificationService) sendEmail(
    ctx context.Context,
    req *verbsv1.SendNotificationInput,
) (*verbsv1.SendNotificationOutput, error) {
    // Send email implementation
    return &verbsv1.SendNotificationOutput{
        NotificationId: generateID(),
        SentAt: timestamppb.Now(),
        Status: verbsv1.DeliveryStatus_DELIVERY_STATUS_SENT,
    }, nil
}
```

**Python Client**:
```python
from acme.v1.verbs import send_notification_pb2
from effectus.v1 import execution_pb2, execution_pb2_grpc

# Create type-safe request
request = send_notification_pb2.SendNotificationInput(
    user_id="user_123",
    message="Welcome to our platform!",
    type=send_notification_pb2.NotificationType.NOTIFICATION_TYPE_EMAIL
)

# Execute via Effectus
client = execution_pb2_grpc.RulesetExecutionServiceStub(channel)
response = client.ExecuteRuleset(execution_pb2.ExecutionRequest(
    ruleset_name="user_onboarding",
    facts=pack_facts([user_event])
))
```

**TypeScript Client**:
```typescript
import { SendNotificationInput, NotificationType } from './gen/acme/v1/verbs/SendNotification_pb';
import { RulesetExecutionServiceClient } from './gen/effectus/v1/execution_connect';

const request = new SendNotificationInput({
  userId: "user_123",
  message: "Welcome to our platform!",
  type: NotificationType.EMAIL
});

const client = new RulesetExecutionServiceClient(transport);
const response = await client.executeRuleset({
  rulesetName: "user_onboarding",
  facts: packFacts([userEvent])
});
```

## 🔄 Schema Evolution Workflow

### 1. Version Your Schemas

When you need to evolve a schema:

```bash
# Create v2 of your verb
cp proto/acme/v1/verbs/send_notification.proto proto/acme/v2/verbs/send_notification.proto

# Edit v2 to add new fields
cat >> proto/acme/v2/verbs/send_notification.proto << 'EOF'
message SendNotificationInput {
  string user_id = 1;
  string message = 2;
  NotificationType type = 3;
  optional Priority priority = 4;  // New field
  optional string template_id = 5; // New field
}

enum Priority {
  PRIORITY_UNSPECIFIED = 0;
  PRIORITY_LOW = 1;
  PRIORITY_NORMAL = 2;
  PRIORITY_HIGH = 3;
  PRIORITY_URGENT = 4;
}
EOF
```

### 2. Check Breaking Changes

```bash
# Buf automatically detects breaking changes
buf breaking --against .git#branch=main

# If no breaking changes, you're good to go!
# If breaking changes detected, bump major version
```

### 3. Migrate Gradually

Your system can support both v1 and v2 simultaneously:

```go
// Your service supports both versions
func (s *NotificationService) ExecuteV1(req *v1.SendNotificationInput) (*v1.SendNotificationOutput, error) {
    // Convert v1 to v2 internally
    v2Req := &v2.SendNotificationInput{
        UserId: req.UserId,
        Message: req.Message,
        Type: convertTypeV1ToV2(req.Type),
        Priority: v2.Priority_PRIORITY_NORMAL, // Default for v1 requests
    }
    return s.ExecuteV2(v2Req)
}

func (s *NotificationService) ExecuteV2(req *v2.SendNotificationInput) (*v2.SendNotificationOutput, error) {
    // Latest implementation
}
```

## 🏢 Team Collaboration Patterns

### Option 1: Git-Based Workflow

```bash
# Typical development workflow
git checkout -b feature/add-priority-field
# Edit proto files
buf generate
buf lint
buf breaking --against origin/main
git commit -am "Add priority field to notifications"
git push origin feature/add-priority-field
# Create PR with schema changes
```

**CI/CD Integration**:
```yaml
# .github/workflows/schemas.yml
name: Schema Validation
on: [push, pull_request]
jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: bufbuild/buf-setup-action@v1
      - name: Lint schemas
        run: buf lint
      - name: Check breaking changes
        run: buf breaking --against origin/main
      - name: Generate code
        run: buf generate
      - name: Test generated code
        run: go test ./gen/...
```

### Option 2: Buf Schema Registry (BSR)

For enterprise teams wanting centralized schema management:

```bash
# Push schemas to BSR
buf registry login
buf push

# Teams can depend on your schemas
# buf.yaml in other services:
deps:
  - buf.build/acme/effectus-schemas

# Automatic updates and notifications
buf mod update
```

### Option 3: Hybrid Approach

- **Core schemas** (shared across teams) → Buf Schema Registry
- **Service-specific schemas** → Git-based workflow
- **Public APIs** → BSR with semantic versioning

## 🛠️ Advanced Patterns

### 1. Schema-Driven Testing

```go
func TestNotificationSchemaCompatibility(t *testing.T) {
    // Test that v1 requests work with v2 service
    v1Request := &v1.SendNotificationInput{
        UserId: "test_user",
        Message: "test message",
        Type: v1.NotificationType_NOTIFICATION_TYPE_EMAIL,
    }
    
    response, err := service.ExecuteV1(v1Request)
    assert.NoError(t, err)
    assert.NotNil(t, response)
}

func TestSchemaEvolution(t *testing.T) {
    // Ensure new fields have sensible defaults
    v2Request := &v2.SendNotificationInput{
        UserId: "test_user", 
        Message: "test message",
        Type: v2.NotificationType_NOTIFICATION_TYPE_EMAIL,
        // Priority not set - should default gracefully
    }
    
    response, err := service.ExecuteV2(v2Request)
    assert.NoError(t, err)
    assert.NotNil(t, response)
}
```

### 2. Auto-Generated Documentation

```bash
# Generate documentation from schemas
buf generate --template buf.gen.docs.yaml

# Results in beautiful HTML docs:
# docs/html/acme/v1/verbs/send_notification.html
# docs/html/acme/v1/facts/user_event.html
```

### 3. Runtime Schema Validation

```go
func (s *NotificationService) ValidateInput(req proto.Message) error {
    // Use protobuf reflection for runtime validation
    descriptor := req.ProtoReflect().Descriptor()
    fields := descriptor.Fields()
    
    for i := 0; i < fields.Len(); i++ {
        field := fields.Get(i)
        if field.HasOptionalKeyword() {
            continue // Optional fields are fine
        }
        
        value := req.ProtoReflect().Get(field)
        if !value.IsValid() {
            return fmt.Errorf("required field %s is missing", field.Name())
        }
    }
    
    return nil
}
```

## 📈 Benefits in Practice

### For Individual Developers

- **Type Safety**: No more runtime errors from typos
- **IDE Support**: Full autocomplete and error checking
- **Multi-Language**: Write once, use everywhere
- **Documentation**: Self-documenting schemas

### For Teams

- **Consistency**: Shared schema standards across services
- **Evolution**: Safe schema evolution with compatibility checking
- **Review Process**: Schema changes go through code review
- **Testing**: Schema validation in CI/CD

### For Organizations

- **Governance**: Centralized schema management
- **Discoverability**: Schema registry as API catalog
- **Compliance**: Audit trail of schema changes
- **Interoperability**: Standard protocols across teams

## 🎯 Migration Strategy

### From Existing JSON Schemas

1. **Convert existing schemas** to proto format
2. **Run both systems** in parallel during transition
3. **Migrate clients** incrementally
4. **Deprecate JSON schemas** once migration complete

### From Other Schema Systems

1. **Generate proto equivalents** of existing schemas
2. **Use buf breaking** to ensure compatibility
3. **Implement adapters** for legacy systems
4. **Phase out old systems** over time

This proto-first approach transforms Effectus from a Go-centric system into a truly polyglot platform where schemas are the foundation for type-safe, multi-language rule execution across your entire organization. 