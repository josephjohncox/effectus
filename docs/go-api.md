# Supported Go API

Start with `bundle`, `invocation`, and `embedded` for in-process execution.
Use `executorhttp` when implementing a remote business operation.
Use `runtime` and the storage contracts when you need durable storage, recovery, or server control.
An exported declaration is not evidence of an implemented product feature.

## Recommended entry path

1. Read a source bundle with `bundle.Parse`, or construct one with `bundle.New`.
2. Register descriptor resolvers with `invocation.NewRegistry`.
3. Open the bundle with `embedded.Open(ctx, source, resolvers)`.
4. Call `Runtime.Execute` with a namespace, stable idempotency key, and facts.
5. Inspect both the result and the error.
6. Close the runtime after its callers finish.

`embedded.Open` compiles and resolves the bundle once.
It creates an in-memory ledger and outbox. It does not start a recovery worker.
Its default stores do not survive process restart.
Use terminal waits for ordinary embedded calls.
An accepted-only call needs a later terminal call or a separately managed recovery worker to advance execution.

Embedded execution requires durable descriptors, not anonymous Go callbacks.
The embedded wrapper constrains execution to its active generation.
For historical replay across generation replacement, use the lower-level engine and a durable artifact resolver.

## Package map and inventory

The reviewed inventory contains 1,768 declarations across 16 package paths.
Of these, 1,248 are generated declarations. Counts include methods, constants, variables, aliases, and types.
The counts describe the source inventory, not supported feature totals.
`guardrails/public-api.txt` and the compatibility tests preserve the declared surface.

| Package suffix | Role and recommended use |
| --- | --- |
| `bundle` | Immutable source bundles, descriptor bindings, serialization, and source identity. |
| `invocation` | Executor outcomes, durable descriptors, resolver registration, and outbound HTTP invocation. |
| `embedded` | Small in-process entry point with default in-memory stores. |
| `executorhttp` | HTTP receiver for one business executor. The business operation supplies durable idempotency. |
| `compiler` | Source-to-checked-IR compilation without executing business operations. |
| `ir` | Checked artifacts, closed values, type normalization, environments, and validation limits. |
| `runtime` | Immutable generations, durable execution, replay, recovery, and authenticated gRPC server construction. |
| `schema` | Store implementations, dispatcher, migration helpers, identity helpers, and retained compatibility types. Not every export is a recommended runtime API. |
| `schema/ledger` | Execution, artifact, admission, and recovery-lease contracts. Implement these for custom durable storage. |
| `schema/workflow` | Outbox, saga, step, and dispatch contracts. Implement these for custom workflow storage. |
| `schema/fencing` | Fencing provider contracts and fencing support. Destination enforcement remains necessary. |
| `schema/expression` | Narrow expression compatibility helpers, not an alternative execution engine. |
| `gen/effectus/v1` | Generated messages and service interfaces. Only the documented execution RPC is implemented by the shipped server. |
| `compat/v03/embedded` | Retained v0.3 import compatibility. Prefer the root package for new code. |
| `compat/v03/invocation` | Retained v0.3 import compatibility. Prefer the root package for new code. |
| `compat/v03/executorhttp` | Retained v0.3 import compatibility. Prefer the root package for new code. |

## Ownership and concurrency

Source bundles, checked artifacts, and generations define immutable executable identity.
Their copy-returning views must not become an alternate mutation path.
Use `ir.ValidationDefaults()` to obtain validation settings.
Do not mutate the deprecated `ir.DefaultLimits` variable to configure another caller.

A successfully constructed engine owns its generation.
`Engine.Generation()` returns a borrowed generation. Do not close that borrowed value.
`Runtime.Engine()` also returns a borrowed engine owned by the embedded runtime.

A generation owns the resource closers returned by its resolvers.
An artifact resolver transfers ownership of its returned generation, even when it also returns an error.
It must not return another engine's borrowed generation.
Resolver implementations and executors can receive concurrent calls. Their shared state must be safe for concurrent access.

Configure an engine before its first execution.
Do not mutate published executor fields, authentication policy, or caller inputs while another call uses them.
The engine coalesces same-identity calls in one process and consults durable state on each call.
Database concurrency still requires store-level compare-and-swap and lease enforcement.

`Engine.Close` waits for entered calls and closes owned generations once.
It does not close borrowed stores, database handles, or fencing providers.
Stop server admission and recovery workers first. Close the engine next, then close borrowed dependencies.
Do not call `Close` from the engine's own executor or server shutdown from one of its handlers.

## Durable engine setup

Use `runtime.CompileGeneration` and `runtime.NewEngine` for the lower-level path.
Configure workflow storage with `Engine.ConfigureWorkflow`.
Configure durable execution storage and historical resolution with `Engine.ConfigureLedger`.
Arrange recovery through `runtime.RecoveryWorker` when admitted work must survive interrupted callers.

New engines start with an in-memory execution ledger.
Merely constructing an engine does not configure a persistent deployment or start recovery.
Use the supplied PostgreSQL store for the maintained durable implementation, or implement the documented contracts.
Schema aliases of ledger and workflow types retain the same contracts. They are not separate persistence models.

New admissions must match the active immutable generation.
Replays use their pinned artifact identity. Do not recompute historical identities using a replacement generation.
Fencing tokens, lease deadlines, and optimistic revisions are authority, not advisory metadata.

## Contexts, outcomes, and errors

Pass a non-nil context to every context-bearing operation.
Cancellation is cooperative. A destination or callback can continue after cancellation.
An interrupted RPC does not prove that a business operation failed to commit.

`WaitAccepted` acknowledges a durable identity without waiting for business completion.
It does not report a prior business failure as an admission error.
`WaitTerminal` returns failed or blocked dispositions through `runtime.TerminalExecutionError`.
Use `errors.As` for its state and `errors.Is` for wrapped sentinels.
Do not expose its underlying cause through an untrusted transport.

The state sequence starts with admission, then accepted/running work, followed by completion, failure, or a blocked terminal state.
Blocked states do not mean successful completion. Unknown outcomes must not become blind retries.
`ExecuteResult.DurablyAccepted` and `Completed` answer different questions.
Both first execution and replay retain this distinction.

Retry transient storage failures with the same identity when retry is appropriate.
Do not retry identity conflicts under a new key to conceal a payload mismatch.
A new key is a new business operation.

## Exports that do not imply supported features

- `BufIntegration`, its configuration structs, `VerbSchema`, `FactSchema`, and related metadata remain compatibility support.
  They do not implement a runtime schema registry, retention policy, privacy engine, or capability authorization.
  See [the legacy Buf contract](buf-compatibility.md).
- Generated fact-registry, verb-registry, management, and streaming RPCs are reserved or unregistered.
  See [the complete gRPC matrix](grpc-capabilities.md).
- Generated `CompiledRuleset` is a legacy registration payload, not the checked artifact produced by `effectusc`.
- Generated request options and response fields do not imply tracing, schema negotiation, effect streaming, or dynamic registration.
- `runtime.NewGeneration` is a low-level integration boundary, not the normal application entry point.
  Do not use it to bypass checked artifacts, descriptor identity, or production validation.
- The legacy two-argument gRPC constructor cannot infer an authentication policy.
  Use `NewRulesetExecutionServerWithOptions` and provide that policy explicitly.

No import path or protobuf identity is removed by this guide.
It narrows the recommended entry path instead of inventing implementations for the full exported surface.
