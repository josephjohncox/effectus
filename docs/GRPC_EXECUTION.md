# gRPC Execution Interface

The Effectus gRPC Execution Interface provides a standardized, language-agnostic way to execute rules using **rulesets** as logical namespaces. Each ruleset gets its own typed protobuf interface with dynamic endpoint registration.

## Overview

The gRPC interface enables:

- **Ruleset-based Organization**: Logical grouping of related rules
- **Dynamic Service Registration**: Auto-generated endpoints for each ruleset
- **Typed Interfaces**: Protocol Buffer schemas for facts and effects
- **Hot Reloading**: Update rulesets without restarting the server
- **Multi-language Support**: Any language with gRPC support can use Effectus
- **Streaming Execution**: Real-time updates for long-running rules

## Architecture

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│                 │     │                 │     │                 │
│    Client       │────▶│  gRPC Server    │────▶│ Execution       │
│ (Any Language)  │     │  (Dynamic)      │     │   Runtime       │
└─────────────────┘     └─────────────────┘     └─────────────────┘
         │                       │                       │
         │                       │                       │
         ▼                       ▼                       ▼
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│                 │     │                 │     │                 │
│ Typed Facts     │     │   Rulesets      │     │ Typed Effects   │
│ (Protobuf)      │     │ (Namespaces)    │     │ (Protobuf)      │
└─────────────────┘     └─────────────────┘     └─────────────────┘
```

### Core Concepts

#### **Ruleset**
A logical grouping of related rules with:
- **Name and Version**: Unique identification
- **Fact Schema**: Typed input requirements
- **Effect Schemas**: Typed output definitions
- **Rules**: Compiled business logic
- **Dependencies**: External service requirements
- **Capabilities**: Required permissions

#### **Dynamic Registration**
- Each ruleset automatically gets a gRPC endpoint
- Endpoints are registered/unregistered on hot-reload
- Type-safe protobuf interfaces generated from schemas
- Service reflection enabled for dynamic discovery

## Service Interface

### Core Service Definition

```protobuf
service RulesetExecutionService {
  // Execute rules with typed facts → typed effects
  rpc ExecuteRuleset(ExecutionRequest) returns (ExecutionResponse);
  
  // Get ruleset metadata and schema information
  rpc GetRulesetInfo(RulesetInfoRequest) returns (RulesetInfo);
  
  // List all registered rulesets
  rpc ListRulesets(ListRulesetsRequest) returns (ListRulesetsResponse);
  
  // Dynamically register new rulesets
  rpc RegisterRuleset(RegisterRulesetRequest) returns (RegisterRulesetResponse);
  
  // Remove rulesets
  rpc UnregisterRuleset(UnregisterRulesetRequest) returns (UnregisterRulesetResponse);
  
  // Stream execution progress for long-running rules
  rpc StreamExecution(ExecutionRequest) returns (stream ExecutionUpdate);
}
```

### Key Message Types

#### ExecutionRequest
```protobuf
message ExecutionRequest {
  string ruleset_name = 1;      // Which ruleset to execute
  string version = 2;           // Specific version (optional)
  google.protobuf.Any facts = 3; // Typed input facts
  ExecutionOptions options = 4;  // Execution configuration
  string trace_id = 5;          // Distributed tracing ID
}
```

#### ExecutionResponse
```protobuf
message ExecutionResponse {
  bool success = 1;                    // Execution success
  repeated TypedEffect effects = 2;    // Generated effects
  string execution_id = 3;             // Unique execution ID
  google.protobuf.Timestamp duration = 4; // Execution time
  map<string, string> metadata = 5;    // Execution metadata
  repeated string errors = 6;          // Error messages
  repeated string warnings = 7;        // Warning messages
}
```

## Usage Examples

### Go Client

```go
package main

import (
    "context"
    "log"
    
    "google.golang.org/grpc"
    "google.golang.org/protobuf/types/known/anypb"
    "google.golang.org/protobuf/types/known/structpb"
)

func main() {
    // Connect to Effectus gRPC server
    conn, err := grpc.Dial("localhost:8080", grpc.WithInsecure())
    if err != nil {
        log.Fatal(err)
    }
    defer conn.Close()
    
    client := NewRulesetExecutionServiceClient(conn)
    
    // Create typed facts
    customerFacts := map[string]interface{}{
        "customer_id": "cust-12345",
        "email": "user@example.com",
        "account_status": "Active",
        "registration_date": "2024-01-15T10:30:00Z",
    }
    
    factsStruct, _ := structpb.NewStruct(customerFacts)
    factsAny, _ := anypb.New(factsStruct)
    
    // Execute customer management rules
    response, err := client.ExecuteRuleset(context.Background(), &ExecutionRequest{
        RulesetName: "customer_management",
        Version:     "1.0.0",
        Facts:       factsAny,
        Options: &ExecutionOptions{
            EnableTracing: true,
            MaxEffects:    10,
            TimeoutSeconds: 30,
        },
        TraceId: "trace-12345",
    })
    
    if err != nil {
        log.Fatal(err)
    }
    
    log.Printf("Execution successful: %d effects generated", len(response.Effects))
    for _, effect := range response.Effects {
        log.Printf("Effect: %s (status: %s)", effect.VerbName, effect.Status)
    }
}
```

### Python Client

```python
import grpc
from google.protobuf import struct_pb2
from google.protobuf import any_pb2

# Import generated gRPC stubs
from effectus_pb2_grpc import RulesetExecutionServiceStub
from effectus_pb2 import ExecutionRequest, ExecutionOptions

def execute_payment_rules():
    # Connect to Effectus
    channel = grpc.insecure_channel('localhost:8080')
    client = RulesetExecutionServiceStub(channel)
    
    # Create typed payment facts
    payment_facts = {
        "transaction_id": "txn-67890",
        "amount": 5000.0,
        "currency": "USD",
        "customer_id": "cust-12345",
        "risk_score": 0.3
    }
    
    # Convert to protobuf Any
    facts_struct = struct_pb2.Struct()
    facts_struct.update(payment_facts)
    facts_any = any_pb2.Any()
    facts_any.Pack(facts_struct)
    
    # Execute payment processing rules
    response = client.ExecuteRuleset(
        ExecutionRequest(
            ruleset_name="payment_processing",
            version="2.1.0",
            facts=facts_any,
            options=ExecutionOptions(
                enable_tracing=True,
                timeout_seconds=30
            )
        )
    )
    
    if response.success:
        print(f"Payment processing successful: {len(response.effects)} effects")
        for effect in response.effects:
            print(f"  {effect.verb_name}: {effect.status}")
    else:
        print(f"Payment processing failed: {response.errors}")

if __name__ == "__main__":
    execute_payment_rules()
```

### TypeScript/Node.js Client

```typescript
import * as grpc from '@grpc/grpc-js';
import { RulesetExecutionServiceClient } from './generated/effectus_grpc_pb';
import { ExecutionRequest, ExecutionOptions } from './generated/effectus_pb';
import { Any } from 'google-protobuf/google/protobuf/any_pb';
import { Struct } from 'google-protobuf/google/protobuf/struct_pb';

const client = new RulesetExecutionServiceClient(
    'localhost:8080',
    grpc.credentials.createInsecure()
);

// Execute customer rules
const customerFacts = {
    customer_id: 'cust-12345',
    email: 'user@example.com',
    account_status: 'Active',
    credit_score: 750
};

const factsStruct = Struct.fromJavaScript(customerFacts);
const factsAny = new Any();
factsAny.pack(factsStruct.serializeBinary(), 'google.protobuf.Struct');

const request = new ExecutionRequest();
request.setRulesetName('customer_management');
request.setVersion('1.0.0');
request.setFacts(factsAny);

const options = new ExecutionOptions();
options.setEnableTracing(true);
options.setMaxEffects(10);
request.setOptions(options);

client.executeRuleset(request, (error, response) => {
    if (error) {
        console.error('Execution failed:', error);
        return;
    }
    
    console.log(`Execution successful: ${response.getEffectsList().length} effects`);
    response.getEffectsList().forEach(effect => {
        console.log(`  ${effect.getVerbName()}: ${effect.getStatus()}`);
    });
});
```

## Ruleset Definition

### Creating a Ruleset

```go
// Define fact schema
factSchema := &runtime.Schema{
    Name: "CustomerFacts",
    Fields: map[string]*runtime.FieldType{
        "customer_id":    {Type: "string", Required: true},
        "email":          {Type: "string", Required: true},
        "account_status": {Type: "string", Required: true},
        "credit_score":   {Type: "int32", Required: false},
    },
    Required:    []string{"customer_id", "email", "account_status"},
    Description: "Customer management fact schema",
}

// Define effect schemas
effectSchemas := map[string]*runtime.Schema{
    "send_welcome_email": {
        Name: "WelcomeEmailEffect",
        Fields: map[string]*runtime.FieldType{
            "to":       {Type: "string", Required: true},
            "subject":  {Type: "string", Required: true},
            "template": {Type: "string", Required: true},
        },
        Required: []string{"to", "subject", "template"},
    },
}

// Define rules
rules := []runtime.CompiledRule{
    {
        Name:        "new_customer_welcome",
        Type:        runtime.RuleTypeList,
        Description: "Send welcome email to new customers",
        Priority:    10,
        Predicates: []runtime.CompiledPredicate{
            {Path: "account_status", Operator: "==", Value: "Active"},
            {Path: "registration_date", Operator: "within", Value: "24h"},
        },
        Effects: []runtime.CompiledEffect{
            {
                VerbName: "send_welcome_email",
                Args: map[string]interface{}{
                    "to":       "{{customer.email}}",
                    "subject":  "Welcome to our service!",
                    "template": "customer_welcome",
                },
            },
        },
    },
}

// Create ruleset
ruleset := &runtime.CompiledRuleset{
    Name:          "customer_management",
    Version:       "1.0.0",
    Description:   "Customer lifecycle management rules",
    FactSchema:    factSchema,
    EffectSchemas: effectSchemas,
    Rules:         rules,
    Dependencies:  []string{"email_service", "notification_service"},
    Capabilities:  []string{"send_email", "update_customer"},
}
```

### Registering a Ruleset

```go
// Create gRPC server
grpcServer, err := runtime.NewRulesetExecutionServer(execRuntime, ":8080")
if err != nil {
    log.Fatal(err)
}

// Register ruleset
if err := grpcServer.RegisterRuleset(ruleset); err != nil {
    log.Fatal(err)
}

// Enable hot reload
grpcServer.EnableHotReload(30 * time.Second)

// Start server
go grpcServer.Start()
```

## Advanced Features

### Streaming Execution

For long-running rules that need progress updates:

```go
stream, err := client.StreamExecution(context.Background(), request)
if err != nil {
    log.Fatal(err)
}

for {
    update, err := stream.Recv()
    if err == io.EOF {
        break
    }
    if err != nil {
        log.Fatal(err)
    }
    
    log.Printf("Phase: %s, Progress: %d%%", 
        update.Phase, update.ProgressPercent)
}
```

### Hot Reloading

The server automatically detects and reloads ruleset changes:

```go
// Server automatically:
// 1. Detects ruleset changes
// 2. Compiles new version
// 3. Validates against existing schema
// 4. Atomically swaps endpoints
// 5. Maintains zero downtime
```

### Error Handling

Comprehensive error information in responses:

```go
if !response.Success {
    log.Printf("Execution failed:")
    for _, err := range response.Errors {
        log.Printf("  Error: %s", err)
    }
    for _, warning := range response.Warnings {
        log.Printf("  Warning: %s", warning)
    }
}
```

## Benefits

### Type Safety
- **Compile-time Validation**: Schemas validated before registration
- **Runtime Type Checking**: Facts validated against schemas
- **Typed Interfaces**: Protocol Buffer guarantees across languages

### Language Interoperability
- **Any gRPC Language**: Go, Python, Java, C#, JavaScript, Rust, etc.
- **Consistent Interface**: Same API regardless of client language
- **Type Generation**: Auto-generated client stubs from protobuf

### Operational Excellence
- **Hot Reloading**: Update rules without downtime
- **Observability**: Built-in tracing and metrics
- **Scalability**: Stateless design enables horizontal scaling
- **Reliability**: Comprehensive error handling and recovery

### Developer Experience
- **Simple Interface**: Facts in → Effects out
- **Rich Metadata**: Comprehensive ruleset information
- **Streaming Support**: Real-time updates for long operations
- **Dynamic Discovery**: Service reflection for tooling

## Integration Patterns

### Microservices Architecture
```
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│   Order     │───▶│  Effectus   │───▶│  Payment    │
│  Service    │    │   gRPC      │    │  Service    │
└─────────────┘    └─────────────┘    └─────────────┘
        │                 │                 │
        ▼                 ▼                 ▼
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│ Inventory   │    │ Notification│    │  Shipping   │
│  Service    │    │   Service   │    │  Service    │
└─────────────┘    └─────────────┘    └─────────────┘
```

### Event-Driven Architecture
```
Kafka Topic → Effectus gRPC → Multiple Effects → Various Services
```

### API Gateway Integration
```
API Gateway → Business Logic → Effectus gRPC → Effect Execution
```

The gRPC execution interface provides a standardized, scalable, and type-safe way to execute business rules across any technology stack while maintaining the mathematical rigor and safety guarantees that make Effectus suitable for mission-critical systems. 