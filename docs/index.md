<div class="effectus-hero" markdown>

# Effectus

Effectus compiles typed rules into checked protobuf IR. The runtime executes that IR through one durable engine.

[Start the walkthrough](GETTING_STARTED.md){ .md-button .md-button--primary }
[Read the guarantees](GUARANTEES.md){ .md-button }

</div>

## Use Effectus when

- You need static checks for facts, verbs, bindings, and declared types.
- You need one execution model across HTTP, Kafka, gRPC, and recovery.
- You need immutable runtime generations with atomic activation.
- You need durable admission, saga state, recovery, and audit data in PostgreSQL.
- You need signed OCI bundles and explicit deployment boundaries.

## Execution boundary

Effectus controls admission and internal execution state. It does not make an external service transactional.

External systems must enforce each supplied idempotency key or fencing token. Compensation is recovery work, not an ACID rollback.

Read [Runtime Guarantees](GUARANTEES.md) before a production deployment.

## Start here

<div class="grid cards" markdown>

- :material-rocket-launch-outline:{ .lg .middle } **Run the checked demo**

    ---

    Build a bundle, start PostgreSQL, run `effectusd`, and submit facts.

    [:octicons-arrow-right-24: Getting started](GETTING_STARTED.md)

- :material-connection:{ .lg .middle } **Choose an integration model**

    ---

    Embed the checked Go runtime or deploy `effectusd` with business executors.

    [:octicons-arrow-right-24: Integration guide](INTEGRATION.md)

- :material-console:{ .lg .middle } **Build and inspect bundles**

    ---

    Use `effectusc` to check, compile, format, graph, and package rules.

    [:octicons-arrow-right-24: CLI reference](COMMANDS.md)

- :material-server-security:{ .lg .middle } **Operate the runtime**

    ---

    Configure storage, authentication, probes, migrations, recovery, and shutdown.

    [:octicons-arrow-right-24: Production runbook](PRODUCTION_RUNBOOK.md)

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
checked protobuf IR
        |
        v
runtime.Engine.Execute
        |
        +--> HTTP
        +--> Kafka
        +--> generated gRPC
        +--> recovery
```

Unsupported production paths fail with explicit errors. Compatibility APIs do not replace the checked runtime boundary.

## Documentation map

| Task | Document |
| --- | --- |
| Choose library or daemon mode | [Integration guide](INTEGRATION.md) |
| Configure `effectusd` | [Runtime configuration](RUNTIME_CONFIG.md) |
| Integrate a client | [gRPC execution](GRPC_EXECUTION.md) and [client examples](CLIENT_EXAMPLES.md) |
| Add a source | [Fact sources](FACT_SOURCES.md) |
| Add an executor | [Extension system](EXTENSION_SYSTEM.md) |
| Understand durability | [Runtime guarantees](GUARANTEES.md) |
| Operate production | [Production runbook](PRODUCTION_RUNBOOK.md) |
| Understand sagas | [Durable saga protocol](DURABLE_SAGA_PROTOCOL.md) |
| Review architecture | [Architecture](ARCHITECTURE.md) |
