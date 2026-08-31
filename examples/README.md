# Effectus Examples

The examples module contains library examples, daemon clients, and local service stacks.

Run commands from the repository root unless a section says otherwise.

## Test all Go examples

```bash
just test-examples
```

This recipe tests the parent examples module and all six nested Go modules. A parent-module `go test ./...` does not enter nested modules.

## Start with a realistic path

| Goal | Example | Command |
| --- | --- | --- |
| Embed checked rules in a Go service | `embedded_orders` | `cd examples && go run ./embedded_orders` |
| Run `effectusd` with a separate business service | `standalone_executor` | `examples/standalone_executor/scripts/run.sh` |
| Inspect a checked daemon through the UI | `flow_ui_demo` | `just ui-flow-demo-smoke` |
| Prove HTTP idempotent replay | `fraud_e2e` with `effectusd` | `just ui-demo-smoke` |
| Call the generated gRPC service | `grpc_execution` | `just grpc-execution-smoke` |

The standalone executor example uses PostgreSQL for both execution state and business idempotency. It runs the full HTTP executor protocol.

## Rule and compiler examples

| Directory | Purpose |
| --- | --- |
| `business_facts` | Register domain facts and functions |
| `business_rules` | Compile domain rules |
| `business_verbs` | Register static Go verb executors |
| `coherent_flow` | Connect extension loading, compilation, and execution |
| `expr` | Evaluate typed expressions |
| `flow` | Use flow bindings and step results |
| `list` | Use ordered list rules |
| `proto_driven_development` | Build schemas and rules from protobuf declarations |

These examples can use compatibility library APIs. They do not define the production daemon boundary.

## Runtime examples

| Directory | Purpose |
| --- | --- |
| `embedded_orders` | Embed checked rules and invocation-aware Go handlers |
| `standalone_executor` | Run `effectusd`, PostgreSQL, and a separate HTTP business executor |
| `fraud_e2e` | Compare the legacy embedded flow with the checked daemon smoke path |
| `flow_ui_demo` | Run flow-heavy checked rules with the status UI |
| `grpc_execution` | Call the generated ruleset execution service |
| `multi_bundle_runtime` | Resolve and activate multiple local bundle versions |
| `modern_sql_usage` | Use the current SQL adapter API |
| `adapter_library_usage` | Embed source adapters in a Go application |

Use the directory README when one exists. It lists required services and environment variables.

## Streaming and CDC examples

| Directory | Purpose |
| --- | --- |
| `amqp_streaming` | Consume AMQP deliveries |
| `grpc_streaming` | Consume a server-streaming gRPC source |
| `mysql_cdc` | Read MySQL binlog events |
| `postgres_cdc` | Read PostgreSQL logical changes |
| `cdc_all` | Run PostgreSQL, MySQL, and AMQP sources together |
| `cdc_stack` | Start the local CDC service stack |

CDC examples require database privileges, retention settings, and output plugins. They are not zero-configuration production templates.

## Durable workflow stack

Start the PostgreSQL durable runtime store:

```bash
just setup-db
```

This starts only PostgreSQL from `saga_stack/docker-compose.yml` and waits with `pg_isready`.

Stop the stack and delete its volumes:

```bash
docker compose -f saga_stack/docker-compose.yml down -v
```

## Warehouse examples

The `warehouse_sources` tree contains:

- Snowflake SQL configurations
- Iceberg and Trino configurations
- A local MinIO development stack
- An S3 Parquet reader example

Read [Warehouse Sources](warehouse_sources/README.md) before you start the local stack.

## UI demos

From the repository root, run:

For a cold-start proof that starts PostgreSQL, launches the checked daemon, and verifies idempotent replay, run:

```bash
just ui-demo-smoke
```

For interactive sessions, use `just ui-demo` or `just ui-flow-demo` after `just setup-db`.
Use `just ui-flow-demo-smoke` to prove flow readiness, baseline ingestion, and the stream script.
Use `just grpc-execution-smoke` for the generated gRPC client and daemon journey.

## Production differences

Production effectusd requires durable PostgreSQL workflow state. It uses checked IR and one shared execution engine.

The daemon rejects in-process plugins and legacy in-memory rule specifications. It requires digest-pinned and verified OCI bundles.

Read [Runtime Guarantees](../docs/GUARANTEES.md) before you adapt an example for production.
