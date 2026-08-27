# Common Package

The `common` package contains shared fact, path, argument, sorting, and execution values.

It is not the production execution engine. Use `runtime.Engine` for durable checked execution.

## Facts

`BasicFacts` provides path-based access to a copied data snapshot and its type system.

```go
facts := common.NewBasicFacts(data, typeSystem)
value, exists := facts.Get("order.customer.id")
```

`WithData` creates another fact value. Callers must not depend on mutable input maps after construction.

## Paths

Use the shared path parser for structured access:

```go
parsed, err := common.ParseString("app.users[0].name")
```

The parser supports named fields, list indexes, and map keys. The schema type system validates rule paths during compilation.

## Arguments

Argument helpers normalize literals, fact paths, and result references for compatibility executors.

The checked compiler uses distinct protobuf variants for these sources. Do not collapse them into an untyped map before checking.

## Ordering

Shared sorting helpers preserve source order for equal priorities.

Stable ordering is part of checked plan selection. It does not make external operations deterministic.

## Execution values

The package includes compatibility execution types used by list and flow libraries.

Production transports use checked IR and `runtime.Engine.Execute`. Legacy Go continuations cannot enter a production generation.

## Test

```bash
go test ./common
```
