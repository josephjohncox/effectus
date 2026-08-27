# Effectus Examples

The examples module contains library examples, daemon clients, and local service stacks.

Run commands from the `examples` directory unless a section says otherwise.

## Build all Go examples

```bash
go test ./...
go vet ./...
```

The examples have their own `go.mod`. Keep it tidy when you change an example.

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
| `fraud_e2e` | Run a complete fraud workflow with compensation |
| `flow_ui_demo` | Run flow-heavy rules with the status UI |
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

Start PostgreSQL and Redis for saga integration work:

```bash
docker compose -f saga_stack/docker-compose.yml up -d
```

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

```bash
just ui-demo
just ui-flow-demo
```

## Production differences

Production effectusd requires durable PostgreSQL workflow state. It uses checked IR and one shared execution engine.

The daemon rejects in-process plugins and legacy in-memory rule specifications. It requires digest-pinned and verified OCI bundles.

Read [Runtime Guarantees](../docs/GUARANTEES.md) before you adapt an example for production.
