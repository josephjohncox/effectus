# Tutorials

Use one of the supported first-run paths.

## Embedded Go

Use `embedded.New` to build checked rules and register invocation-aware Go handlers in a trusted process. Run the complete example:

```bash
go run ./examples/embedded_orders
```

The embedded path is process-local. Use the daemon path when execution state must survive a restart.

## Durable HTTP executor

Run the standalone order-review example from the repository root:

```bash
export EXECUTOR_TOKEN='local-executor-token'
examples/standalone_executor/scripts/run.sh
```

The example starts `effectusd`, PostgreSQL, and a separate HTTP business executor. The executor receives immutable invocation identity and returns an explicit outcome. See [Getting Started](GETTING_STARTED.md) for cleanup and replay verification.

Production outbound verb execution uses the canonical HTTP executor target. The generated gRPC service is an inbound API, and OCI distributes source bundles; neither is an outbound executor resolver.
