# Integration Guide

Start with [the executable language tutorial](BASICS.md) before choosing a deployment boundary.
Both supported paths consume immutable source bundles and checked verb contracts.
They differ in who owns storage, recovery, and executor resolution.

## Components and responsibilities

| Component | Responsibility |
| --- | --- |
| Fact producer | Supplies input values and a stable logical request identity. |
| Bundle producer | Combines source files, environment declarations, executor descriptors, name, and version. |
| Checked compiler | Validates predicates, types, bindings, and supported operations. Produces checked IR. |
| Resolver registry | Maps explicit descriptor resolver IDs to trusted executor implementations. |
| Engine | Admits identities, records execution state, and dispatches planned work. |
| Destination executor | Classifies outcomes and coordinates deduplication with the business commit. |

A contract does not install an executor.
An executor implementation does not declare a rule's types.
Bind the two through a descriptor in `bundle.Spec.Executors`.
Do not put credentials, fencing grants, or idempotency metadata in caller-controlled verb arguments.

## Embedded Go

Use `bundle.New` or `bundle.Parse`, then `invocation.NewRegistry` and `embedded.Open`.
The registry resolves each descriptor to an `invocation.Executor` and optional owned resource.
Explicit embedded descriptors can select in-process Go implementations.
This is different from the removed root callback facade or a runtime plugin-directory loader.

Run the small shared order-review example:

```bash
go run ./examples/embedded_orders
```

Then run the result-binding flow:

```bash
go run ./examples/embedded_orders/tutorial -dialect=effx
```

The programs use default process-local ledgers and outboxes.
Their replay records do not survive process exit, and they start no recovery worker.
An accepted-only embedded call needs a later terminal call or explicitly managed recovery to make progress.

Use a context with the lifetime and deadline of the operation.
Keep request values stable while the call copies them.
An executor or resolver can receive concurrent calls, so protect shared application state.
Stop incoming work before `Runtime.Close` and close separately owned dependencies afterward.
See [the Go API](go-api.md) for ownership and lower-level durable engine setup.

## Durable daemon

Use `effectusd` when admission, dispatch, and recovery must survive process restart.
Its production resolver registry supports HTTP executor descriptors.
A tutorial bundle that uses an embedded descriptor is valid compiler input but cannot execute in this daemon.
Replace those bindings with supported HTTP descriptors for your destination services.

Before starting the daemon:

1. Prepare PostgreSQL and choose an explicit migration policy.
2. Build or obtain one immutable source bundle.
3. Start the destination services identified by its HTTP descriptors.
4. Configure the destination credentials in those descriptors.
5. Configure the independent inbound API bearer token.

Treat bundles containing authentication headers as confidential artifacts.
Destination authentication and the daemon's inbound API authentication are separate boundaries.

The following assumes an existing `order-review.bundle.json` and a migrated database:

```bash
: "${EFFECTUS_POSTGRES_DSN:?set the PostgreSQL DSN}"
: "${EFFECTUS_API_TOKEN:?set the API bearer token}"
go run ./cmd/effectusd --mode=serve --bundle order-review.bundle.json --http-addr=127.0.0.1:8080
```

The daemon compiles and resolves its active generation before serving requests.
HTTP, Kafka, and gRPC share the engine and durable stores.
PostgreSQL is the durable authority, not a local result cache.
Process replacement changes the active admission generation. The daemon has no hot-reload or automatic rollback path.
Historical replay and recovery retain pinned artifact identity and can resolve historical generations.

The [Docker onboarding](GETTING_STARTED.md#path-2-durable-docker) includes an HTTP destination and restart/replay checks.
Read its prerequisites and resource-ownership rules before running its scripts.
See [Runtime Configuration](RUNTIME_CONFIG.md) for migrations, flags, authentication, and transport setup.

## Admission is not business completion

HTTP execution uses accepted-only waits. HTTP 202 does not promise that a destination completed its work.
Use the same namespace, ruleset/version, idempotency key, and logical content when retrying an admission.
A conflicting request must use a genuinely different logical identity, not a random key to bypass an error.

Terminal Go and gRPC calls can return typed failed or blocked states.
A deadline or lost connection does not prove that a destination did not commit.
Persist destination deduplication with the business mutation and enforce fencing when stale owners can threaten correctness.
See [Runtime Guarantees](GUARANTEES.md) and [gRPC Execution](GRPC_EXECUTION.md).

## v0.3 compatibility

`compat/v03` preserves frozen request and callback vocabulary for external consumers.
It owns its callback adaptation and does not restore the removed root facade.
New applications should use `bundle`, `embedded`, and `invocation` directly.
