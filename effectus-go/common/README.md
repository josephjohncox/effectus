# Effectus Common Package

This package provides shared type definitions and interfaces used across the Effectus codebase. 

## Architecture Overview

The Effectus path and type system has been unified to reduce redundancy and improve consistency. The key components are:

### Types

The `common.Type` structure represents types in the Effectus type system. These types can be:

- Primitive types (bool, int, float, string, etc.)
- Container types (lists and maps)
- Named types (user-defined types)
- Reference types (types defined elsewhere)

### Path Resolution

Paths in Effectus are used to navigate structured data. The path system consists of:

1. A common `Path` structure in the `path` package that contains type information
2. Path elements that can include array indices and map keys
3. A unified path resolver that can resolve paths against data structures

### Interfaces

Several interfaces are defined to enable consistent interaction with different implementations:

- `SchemaInfo` - For type information about paths
- `Facts` - For accessing data via paths
- `ResolutionResult` - For detailed results of path resolution

## Design Principles

1. **Centralized Type Definitions**: Define common types once to avoid duplication
2. **Embedded Type Information**: Attach type information directly to paths
3. **Consistent Resolution**: Use a single resolution mechanism throughout the system
4. **Interface-Based Design**: Allow different implementations to work together

## Implementation Details

- The base `path` package provides core path functionality without dependencies
- The `common` package defines types and interfaces used across packages
- Schema packages use these common types to perform validation and resolution

## Usage Examples

```go
// Create a path with type information
element := path.NewElement("user").WithType(&common.Type{PrimType: common.TypeObject})
userPath := path.NewPath("app", []path.PathElement{element})

// Resolve a path against data
resolver := path.NewPathResolver(nil, false)
result, _ := resolver.ResolvePath("app.user", data)
if result.Exists {
    // Process the result...
}
``` 