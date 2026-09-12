# List and Flow Tutorial

Read [the incremental tutorial](../../../docs/BASICS.md) from the repository checkout.
It explains facts, verb contracts, result bindings, ordering, bundles, resolvers, and execution outcomes.

From the repository root:

```bash
go run ./examples/embedded_orders/tutorial -dialect=eff
go run ./examples/embedded_orders/tutorial -dialect=effx
go test -race ./examples/embedded_orders/tutorial
```

Both runs create a review ticket, record it, and replay the same request without another business operation.
All state is process-local. This example starts no network server and requires no database.

The two rule files are embedded into the binary.
The optional `-bundle` output uses embedded executor descriptors for this program's explicit resolver.
The shipped daemon cannot resolve those descriptors. Use HTTP descriptors and destination services for a daemon deployment.
