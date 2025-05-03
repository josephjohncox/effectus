# Effectus Path Handling System

This document describes the path handling system in Effectus, designed to provide a robust and type-safe way to access structured data.

## Core Architecture

The path handling system is built around three key components:

1. **Structured Paths**: Type-safe structures for representing paths with namespace and elements
2. **Fact Providers**: Interfaces for accessing data through paths
3. **Type System**: Schema information to validate and type-check paths

All components are designed to be immutable, ensuring thread safety and predictable behavior in concurrent environments.

## Package Structure

The system is organized in a clean dependency hierarchy:

```
pathutil → schema/types → schema/registry → other packages
```

Key packages:

- `pathutil`: Core path types and interfaces with no external dependencies
- `schema/types`: Type system and typed providers
- `schema/registry`: Schema management and registration

## Main Components

### Path and PathElement (pathutil)

A path consists of a namespace and a sequence of path elements:

```go
// Example: app.user.profile.email
path := pathutil.NewPath("app", []pathutil.PathElement{
    pathutil.NewElement("user"),
    pathutil.NewElement("profile"),
    pathutil.NewElement("email"),
})

// Example: app.users[0].roles["admin"]
path := pathutil.NewPath("app", []pathutil.PathElement{
    pathutil.NewElement("users").WithIndex(0),
    pathutil.NewElement("roles").WithStringKey("admin"),
})
```

### FactProvider Interface (pathutil)

The core interface for data access:

```go
type FactProvider interface {
    Get(path Path) (interface{}, bool)
    GetWithContext(path Path) (interface{}, *ResolutionResult)
}
```

### Registry (pathutil)

Routes requests to namespace-specific providers:

```go
registry := pathutil.NewRegistry()
registry.Register("app", appProvider)
registry.Register("system", systemProvider)

// Resolves to appropriate provider based on namespace
value, exists := registry.Get(path)
```

### MemoryProvider (pathutil)

Reference implementation for in-memory data:

```go
data := map[string]interface{}{
    "user": map[string]interface{}{
        "name": "John",
        "age":  30,
    },
}

provider := pathutil.NewMemoryProvider(data)
```

### Type System (schema/types)

Schema information with type definitions:

```go
typeSystem := types.NewTypeSystem()

userType := types.NewObjectType()
userType.AddProperty("name", types.NewStringType())
userType.AddProperty("age", types.NewIntType())

typeSystem.RegisterType("User", userType)
typeSystem.RegisterFactType("app.user", userType)
```

### TypedProvider (schema/types)

Enhances FactProvider with type information:

```go
typedProvider := types.NewTypedProvider(provider, typeSystem)
value, result := typedProvider.GetWithContext(path)
// result.Type contains type information
```

### SchemaRegistry (schema/registry)

Central registry for schema management:

```go
registry := registry.NewSchemaRegistry(typeSystem)
registry.RegisterLoader(registry.NewJSONSchemaLoader("app"))
registry.LoadSchema("schemas/user.json")
registry.RegisterProvider("app", provider)
```

## Usage Examples

### Basic Path Resolution

```go
// Parse a path string
path, _ := pathutil.ParseString("app.user.profile.email")

// Create resolver
resolver := pathutil.NewPathResolver(false)

// Resolve against a provider
value, exists := resolver.Resolve(provider, path)
```

### With Type Information

```go
// Create typed provider
typedProvider := types.NewTypedProvider(provider, typeSystem)

// Resolve with type information
value, result := resolver.ResolveWithContext(typedProvider, path)
if result.Exists && result.Type != nil {
    // Use type information...
}
```

### Loading Schemas

```go
// Create registry
registry := registry.NewSchemaRegistry(nil)

// Register loader
loader := registry.NewJSONSchemaLoader("app")
registry.RegisterLoader(loader)

// Load schema
registry.LoadSchema("schemas/user.json")

// Create provider from data
provider, _ := loader.CreateFactProvider("data/user.json")
registry.RegisterProvider("app", provider)
```

## Design Principles

1. **Immutability**: All data structures are immutable to ensure thread safety
2. **Structured paths**: No string parsing during resolution
3. **Namespaced providers**: Clean separation of concerns
4. **Type information**: Type checking and validation at runtime
5. **Clean dependencies**: One-way dependency flow 