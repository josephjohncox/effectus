# Effectus Documentation

Read the [published documentation](https://josephjohncox.github.io/effectus/) or use this source index.

## Start here

1. Complete [Getting Started](GETTING_STARTED.md) to run the checked HTTP path.
2. Read [Integration Guide](INTEGRATION.md) to choose embedded or standalone mode.
3. Read [Basics](BASICS.md) for facts, rules, flows, verbs, and effects.
4. Read [Architecture](ARCHITECTURE.md) for the production data path.
5. Read [Runtime Guarantees](GUARANTEES.md) for implemented guarantees and limits.
6. Read [System Intent](SYSTEM_INTENT.md) for the design criteria.
7. Use the [Glossary](GLOSSARY.md) for shared terms.

## Build and use rules

- [Tutorials](TUTORIALS.md) contains short examples.
- [CLI Reference](COMMANDS.md) describes `effectusc` and `effectusd`.
- [Client Examples](CLIENT_EXAMPLES.md) shows API clients.
- [SourceBundle Extension Boundary](EXTENSION_SYSTEM.md) describes immutable bundle contents and executor descriptors.
- [Fact Sources](FACT_SOURCES.md) describes streaming and batch adapters.
- [gRPC Execution](GRPC_EXECUTION.md) describes the generated execution service.

## Operate the runtime

- [Runtime Configuration](RUNTIME_CONFIG.md) defines strict YAML and JSON configuration.
- [Runtime Lifecycle](LIFECYCLE.md) defines activation, refresh, drain, and shutdown.
- [Production Runbook](PRODUCTION_RUNBOOK.md) provides deployment and recovery procedures.
- [Durable Saga Protocol](DURABLE_SAGA_PROTOCOL.md) defines dispatch, leases, outcomes, and fencing.
- [Dependency Audit](DEPENDENCY_AUDIT.md) records dependency checks and remaining external actions.
- [Helm Chart](../charts/effectusd/README.md) describes Kubernetes deployment.

## Design references

- [Design Principles](design.md) records the main design choices.
- [Checked Compilation Flow](coherent_flow.md) maps source loading to checked execution.
- [Theory Notes](theory/README.md) contains non-normative semantic models.
- [Executable State Models](../formal/README.md) describes the checked TLA+ models.
- [Checked IR](../ir/README.md) defines the production artifact boundary.

## Examples

Use the [examples index](../examples/README.md) to find runnable examples and local infrastructure stacks.

## Authority order

Use this order when two documents appear to conflict:

1. Generated API schemas and checked migrations
2. [Runtime Guarantees](GUARANTEES.md)
3. [Runtime Lifecycle](LIFECYCLE.md) and [Durable Saga Protocol](DURABLE_SAGA_PROTOCOL.md)
4. Package and command references
5. Design and theory notes

Theory notes describe models and proof obligations. They do not override runtime behavior.

## Documentation rules

- Describe current behavior in the present tense.
- Label compatibility paths and planned work.
- Use one term for each runtime state.
- Link to the authoritative document instead of copying its contract.
- Add runnable commands only when the repository tests them.
