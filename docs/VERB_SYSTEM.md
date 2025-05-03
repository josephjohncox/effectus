# Verb System

The Verb System in Effectus provides the mechanism for rules to perform actions (effects) on the outside world. This document explains the design, implementation, and usage of the verb system.

## Overview

Verbs are the "effects" in Effectus - they represent operations that rules can trigger. Examples include sending emails, updating records in a database, or calling external APIs. The verb system is designed with the following goals:

1. **Type Safety**: Verbs specify their argument and return types
2. **Capability Control**: Verbs declare what capabilities they require
3. **Extensibility**: New verbs can be added via plugins or direct registration
4. **Compensation**: Verbs can specify inverse operations for saga-style compensation

## Components

### VerbSpec

A verb specification defines the contract of a verb:

```go
type VerbSpec struct {
    Name        string                    // Unique name of the verb
    ArgTypes    map[string]*Type          // Argument names and their types
    ReturnType  *Type                     // Return type (if any)
    Capability  VerbCapability            // Required capability level
    Inverse     string                    // Name of compensating verb (if any)
    Description string                    // Human-readable description
    Executor    VerbExecutor              // Implementation of the verb
}
```

### VerbCapability

Verbs specify what level of system access they require:

```go
type VerbCapability int

const (
    CapReadOnly VerbCapability = iota      // Read-only operations
    CapModify                              // Modify existing resources
    CapCreateResource                       // Create new resources
    CapDeleteResource                       // Delete resources
)
```

### VerbExecutor

The executor implements the actual functionality of a verb:

```go
type VerbExecutor interface {
    Execute(ctx context.Context, args map[string]interface{}) (interface{}, error)
}
```

### VerbRegistry

The registry manages all available verbs:

```go
type VerbRegistry struct {
    verbs      map[string]*VerbSpec       // All registered verbs
    verbHash   string                     // Hash of all verb specs (for consistency checks)
    typeSystem *TypeSystem                // Reference to type system for validation
}
```

## Verb Definition

Verbs can be defined in JSON format:

```json
[
  {
    "name": "sendEmail",
    "arg_types": {
      "to": {"primType": 1, "name": "string"},
      "subject": {"primType": 1, "name": "string"},
      "body": {"primType": 1, "name": "string"}
    },
    "return_type": {"primType": 1, "name": "string"},
    "capability": 2,
    "inverse": "logFailedEmail",
    "description": "Sends an email to the specified recipient"
  }
]
```

## Verb Implementation

### Direct Implementation

Verbs can be implemented directly in Go:

```go
type EmailSender struct {}

func (s *EmailSender) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
    to := args["to"].(string)
    subject := args["subject"].(string)
    body := args["body"].(string)
    
    // Send email implementation
    // ...
    
    return "message-id-12345", nil
}

// Register with the verb registry
verbRegistry.RegisterVerb(&VerbSpec{
    Name: "sendEmail",
    // ... other fields
    Executor: &EmailSender{},
})
```

### Plugin Implementation

Verbs can also be implemented as plugins:

```go
// In plugin file (compiled as a separate .so file)
package main

import (
    "context"
    "github.com/effectus/effectus-go/schema"
)

type EmailPlugin struct {}

func (p *EmailPlugin) GetVerbs() []*schema.VerbSpec {
    return []*schema.VerbSpec{
        {
            Name: "sendEmail",
            // ... other fields
            Executor: &EmailSender{},
        },
    }
}

// This is the symbol that Effectus will look for
var VerbPlugin EmailPlugin
```

## Usage in Rules

Verbs are used in the `then` clause of rules:

```
rule new_customer_welcome {
    when {
        customer.isNew == true
    }
    then {
        sendEmail(
            to: customer.email,
            subject: "Welcome to our service",
            body: "Thank you for joining our service..."
        )
    }
}
```

## Type Checking

The verb system validates that:

1. All verbs used in rules are registered
2. All required arguments are provided
3. Argument types match the verb's specification
4. Return values are used correctly

## Verb Hash

The verb registry computes a hash of all registered verb specifications (excluding the actual implementations). This hash is used to ensure that:

1. The runtime has compatible verb definitions with the bundle
2. Bundles can declare which verb set they require

```go
func (vr *VerbRegistry) GetVerbHash() string {
    return vr.verbHash
}
```

## Plugin System

The verb system supports loading plugins from `.so` files:

```go
func (vr *VerbRegistry) LoadVerbPlugins(dir string) error {
    // Scan directory for .so files
    // Load each plugin
    // Register verbs from plugins
}
```

This allows for separation between the core system and specific implementations.

## Capability-Based Security

Verbs declare their capability requirements, and the runtime can restrict which capabilities are allowed. This provides a security mechanism to limit what rules can do:

```go
// When creating the executor
executor := eval.NewListExecutor(verbReg, eval.WithCapabilityRestriction(schema.CapModify))
```

With this restriction, any verb requiring `CapCreateResource` or `CapDeleteResource` will be rejected.

## Saga Compensation

For transactional integrity, verbs can specify an inverse operation:

```json
{
  "name": "createOrder",
  "inverse": "cancelOrder",
  "capability": 2
}
```

If a rule execution fails after `createOrder` has been executed, the system will automatically call `cancelOrder` to compensate.

## Conclusion

The Verb System provides a flexible, type-safe way to define and execute effects from rules. By separating verb specifications from implementations and supporting plugins, it allows for a clean architecture that can be extended without modifying the core system. 