# Effectus Common Package

This package provides the core types, interfaces, and implementations used across the Effectus codebase.

## Architecture

The Effectus path and type system follows a simple, unified architecture:

### Types

The `Type` structure represents types in the Effectus type system:

- Primitive types (bool, int, float, string, etc.)
- Container types (lists and maps)
- Named types (user-defined types)
- Reference types (types defined elsewhere)

### Paths

Paths in Effectus navigate structured data:

- `Path` contains a namespace and elements with built-in type information
- `PathElement` supports named access, array indices, and map keys
- Parsing and resolution are handled through a single consistent API

### Facts

Facts are immutable typed data structures:

- `Facts` interface provides path-based access to data
- `BasicFacts` implementation ensures immutability through deep copying
- `WithData` creates a new Facts instance with modified data
- Resolution integrates seamlessly with type information

## Immutability

All components in this package embrace immutability:

- Path operations return new Path instances
- Facts are not modifiable after creation
- Type definitions are immutable

## Core APIs

```go
// Parse a path string
path, err := common.ParseString("app.users[0].name")

// Access data with paths
facts := common.NewBasicFacts(data, schema)
value, exists := facts.Get("app.users[0].name")

// Get detailed resolution results
value, result := facts.GetWithContext("app.users[0]")
if result.Exists && result.Type != nil {
    // Work with typed result
}

// Create a modified copy with new data
updatedData := prepareNewData()
newFacts := originalFacts.WithData(updatedData)
```

## Design Philosophy

1. **Simplicity**: Single implementations with no redundancy
2. **Immutability**: Values are never modified in place
3. **Type Safety**: Type information travels with paths
4. **Functional Style**: Operations return new values

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