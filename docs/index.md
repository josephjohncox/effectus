<div class="effectus-hero" markdown>

# Effectus

Effectus compiles typed rules into checked protobuf IR. The same execution engine supports process-local embedded use and PostgreSQL-backed durable execution.

[Start the walkthrough](GETTING_STARTED.md){ .md-button .md-button--primary }
[Read the guarantees](GUARANTEES.md){ .md-button }

</div>

## Use Effectus when

- You need static checks for facts, verbs, bindings, and declared types.
- You need one execution engine for inbound HTTP requests, Kafka records, gRPC calls, and recovery.
- You need an immutable startup generation and historical artifacts for replay and recovery.
- You need durable admission, saga state, recovery, and audit data in PostgreSQL.
- You need signed OCI bundles and explicit deployment boundaries.

## Execution boundary

Effectus controls admission and internal execution state. It does not make an external service transactional.

External destinations must coordinate deduplication with their business commit and enforce fencing when the contract requires it.
Fencing does not replace deduplication. Compensation is recovery work, not an ACID rollback.

Read [Runtime Guarantees](GUARANTEES.md) before a production deployment.

## Select a first-run path

<div class="grid cards" markdown>

- :material-language-go:{ .lg .middle } **Embedded Go**

    ---

    Run checked rules and business handlers in one Go process. The demo state is ephemeral.

    [:octicons-arrow-right-24: Run the embedded path](GETTING_STARTED.md#path-1-embedded-go)

- :material-docker:{ .lg .middle } **Durable Docker**

    ---

    Run `effectusd`, PostgreSQL, and a separate business executor. The demo proves restart-safe replay.

    [:octicons-arrow-right-24: Run the durable path](GETTING_STARTED.md#path-2-durable-docker)

</div>

## Production path

The production path has one checked boundary:

```text
.eff and .effx sources
        |
        v
compiler.CompileChecked
        |
        v
checked protobuf IR --> immutable generation
                                |
HTTP requests ------------------|
Kafka records ------------------+--> runtime.Engine.Execute
inbound gRPC calls -------------|             |
recovery of durable work -------|             v
                                  invocation executor
                                  (outbound HTTP in effectusd)
```

The daemon loads its admission generation at startup. Bundle changes require process replacement, not hot reload.
Replay and recovery can use the immutable artifact already pinned to a durable identity.
Embedded Go applications can register in-process executors; the daemon does not load those application bindings.
Unsupported production paths fail with explicit errors. Compatibility APIs do not replace the checked runtime boundary.

## Documentation map

| Task | Document |
| --- | --- |
| Choose library or daemon mode | [Integration guide](INTEGRATION.md) |
| Configure `effectusd` | [Runtime configuration](RUNTIME_CONFIG.md) |
| Call the HTTP API | [HTTP reference](HTTP_API.md) |
| Embed in Go | [Go API](go-api.md) |
| Integrate a gRPC client | [gRPC execution](GRPC_EXECUTION.md), [capability matrix](grpc-capabilities.md), and [client examples](CLIENT_EXAMPLES.md) |
| Add a source | [Fact sources](FACT_SOURCES.md) |
| Add an executor | [Extension system](EXTENSION_SYSTEM.md) |
| Understand durability | [Runtime guarantees](GUARANTEES.md) |
| Operate production | [Production runbook](PRODUCTION_RUNBOOK.md) |
| Understand sagas | [Durable saga protocol](DURABLE_SAGA_PROTOCOL.md) |
| Review architecture | [Architecture](ARCHITECTURE.md) |
