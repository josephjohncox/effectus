# Durable Order Review

This example is the durable first-run implementation. Use the [Getting Started guide](../../docs/GETTING_STARTED.md) for prerequisites, commands, output, ports, cleanup, and troubleshooting.

## Service map

| Service | Purpose | State |
| --- | --- | --- |
| `effectusd` | Admits the order and dispatches checked invocations | PostgreSQL |
| `migrate` | Applies the Effectus schema before daemon startup | Exits after success |
| `business-executor` | Applies the review mutation and enforces idempotency | PostgreSQL |
| `postgres` | Stores Effectus state and business review records | Named volume |

PostgreSQL has no host port. Containers connect to it at `postgres:5432`.

## File map

| Path | Purpose |
| --- | --- |
| `docker-compose.yml` | Defines the loopback-only service stack and migration order |
| `Dockerfile` | Builds the separate Go business executor |
| `executor/main.go` | Implements review creation, compensation, and business idempotency |
| `scripts/run.sh` | Derives the request from the shared scenario and runs replay, restart, and conflict checks |
| `scripts/down.sh` | Removes containers, the network, and the data volume |
| `../order_review/` | Owns the shared rule and scenario artifact |

## Integration boundary

Effectus sends verb arguments in the HTTP body. Reserved headers carry execution and saga identity.

The business executor stores the idempotency key and argument hash with the review. A matching replay returns the stored result.

A conflicting replay returns a permanent failure. An unknown database commit returns an unknown outcome.

The bundle generator creates immutable HTTP executor descriptors and writes the actual demo token only to the generated bundle under `out/standalone_executor`.

## Production changes

Use TLS and a secret manager. Restrict network access between `effectusd` and each executor.

Run PostgreSQL with backups and high availability. Monitor blocked outcomes, retries, lease age, and executor latency.
