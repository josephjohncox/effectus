# Using Protobuf Facts with Effectus

This directory contains example Protocol Buffer (protobuf) definitions for facts that can be used with Effectus rules.

## Overview

Effectus is designed to support facts defined in Protocol Buffers, enabling:

1. Strong typing and schema validation
2. Language-neutral representation
3. Efficient serialization/deserialization
4. Schema evolution
5. Forward and backward compatibility

## How it Works

1. Define your domain facts in `.proto` files
2. Generate Go code using protoc or Buf
3. Create a `ProtoFacts` wrapper around your generated messages
4. Pass these facts to the Effectus engine

## Example Proto Files

This directory contains several example proto files:

- `customer.proto` - Defines customer-related facts
- `order.proto` - Defines order-related facts 
- `facts.proto` - Aggregates all fact types into a single container

## Generating Go Code

To generate Go code from these proto files:

```bash
# Install Buf if not already installed
go install github.com/bufbuild/buf/cmd/buf@latest

# Generate code (from the parent directory)
cd examples/common/facts
buf generate
```

## Using Proto Facts in Rules

In your Effectus rule files (.eff, .effx), you can reference proto-defined facts using dot notation:

```
rule "CheckVIPCustomer" priority 100 {
    when {
        customer.vip == true
        order.total > 1000.0
    }
    then {
        ApplyDiscount amount:50.0 customer_id:customer.id
    }
}
```

## Example Usage in Go

```go
package main

import (
    "github.com/effectus/effectus-go/schema"
    "github.com/effectus/examples/common/facts/v1"
)

func main() {
    // Create a facts object from your protobuf message
    protoMsg := &facts.Facts{
        Customer: &facts.Customer{
            Id:   "CUST123",
            Name: "Example Customer",
            Vip:  true,
        },
        Order: &facts.Order{
            Id:    "ORD456",
            Total: 1500.0,
        },
    }
    
    // Wrap it in a ProtoFacts for use with Effectus
    factsObj := schema.NewProtoFacts(protoMsg)
    
    // Use it with the Effectus engine
    // ... (see proto_facts_example.go for a complete example)
}
```

## Benefits of Using Protocol Buffers

1. **Type Safety**: Proto definitions provide strong typing for fact fields
2. **Schema Evolution**: Add new fields without breaking existing rules
3. **Cross-Language Support**: Generate clients in multiple languages
4. **Performance**: More efficient than JSON for serialization/deserialization
5. **Documentation**: Proto files serve as self-documenting schemas
6. **Tooling**: Rich ecosystem of tools for validation, documentation, etc.

## Best Practices

1. Define a clear namespace structure for your facts
2. Version your proto packages (e.g., `facts.v1`, `facts.v2`)
3. Use a reserved block in your Facts message for future extensions
4. Consider using a schema registry or shared repository for your proto files 