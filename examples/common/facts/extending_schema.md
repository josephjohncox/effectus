# Extending the Fact Schema

This guide shows how to extend the Effectus fact schema with new fact types without breaking existing rules.

## The Problem

When building a rule engine for evolving domains, you need the ability to:

1. Add new fact types without breaking existing rules
2. Maintain backward compatibility
3. Support independent versioning of fact modules

## Protocol Buffers: The Solution

Protocol Buffers (protobuf) offers an elegant solution:

1. **Composable Message Types**: Each fact domain is defined in its own message
2. **Aggregation**: A top-level `Facts` message contains all domain fact types
3. **Independent Evolution**: Each namespace can evolve separately
4. **Reserved Fields**: Ensure future extensions don't reuse field numbers

## How to Add a New Fact Type

### 1. Define the New Fact Type

Create a new `.proto` file with your fact type:

```proto
// product.proto
syntax = "proto3";
package facts.v1;

message Product {
  string id = 1;
  string name = 2;
  double price = 3;
  // ...other fields
}
```

### 2. Extend the Facts Message

Modify the top-level Facts message to include your new type:

```proto
// facts_extended.proto
syntax = "proto3";
package facts.v1;

import "examples/common/facts/v1/customer.proto";
import "examples/common/facts/v1/order.proto";
import "examples/common/facts/v1/product.proto";

message FactsExtended {
  // Original fact types
  Customer customer = 1;
  Order order = 2;
  
  // New fact type
  Product product = 3;
  
  // Reserved for future extensions
  reserved 4 to 100;
}
```

### 3. Generate the Code

Run the protobuf code generator:

```bash
buf generate
```

### 4. Use in Your Application

```go
// Create facts with the new product fact
facts := &factsv1.FactsExtended{
    Customer: &factsv1.Customer{
        Id: "CUST123",
        // ...
    },
    Order: &factsv1.Order{
        // ...
    },
    Product: &factsv1.Product{
        Id: "PROD456",
        Name: "Widget",
        Price: 29.99,
    },
}

// Create ProtoFacts from the extended message
factsObj := schema.NewProtoFacts(facts)
```

### 5. Write Rules Using the New Fact Type

```
rule "ProductDiscount" priority 50 {
    when {
        product.price > 20.0
        product.category == "electronics"
    }
    then {
        ApplyProductDiscount product_id:product.id discount:5.0
    }
}
```

## Backward Compatibility

One of the key benefits of this approach is backward compatibility:

1. **Existing Rules Still Work**: Rules that only use customer and order facts continue to work
2. **Gradual Adoption**: You can migrate fact producers/consumers at your own pace
3. **Zero-Value Semantics**: When fields aren't set, they default to zero/empty values
4. **Optional Fields**: All fields in proto3 are optional by default

## Version Evolution

For major changes that break compatibility:

1. Create a new package version (e.g., `facts.v2`)
2. Define new message types with the changes
3. Implement adapters between v1 and v2 if needed
4. Migrate rules gradually from v1 to v2 