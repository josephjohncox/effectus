# Checked Compilation Flow

This page describes the shipped daemon, not an abstract hot-reload design.
One startup source bundle defines the active generation for new admissions.

## Inputs

`effectusd` loads exactly one local bundle or one digest-pinned OCI bundle with signature verification.
It does not load extension directories or poll mutable tags.

A `bundle.SourceBundle` contains:

- `.eff` list-rule or `.effx` flow-rule sources
- Fact type declarations
- Verb contracts
- Durable executor descriptors
- Bundle name, version, and metadata

Declarations and descriptors belong to the bundle's immutable identity.
A mutable external registry does not supply missing executable definitions during admission.

## Compile and check

`compiler.CompileChecked` parses and checks each rule source against the bundle environment.
List rules preserve source effect order. Flow rules assign result slots in step order.

Compilation rejects unknown facts and verbs, unavailable predicate-function calls, invalid types, and invalid argument bindings.
It also rejects references to future result slots and unsupported nested saga boundaries.

The `ir` package validates the protobuf artifact before execution or storage.
It applies structural limits, checks environment and contract hashes, and rejects unknown protobuf fields.
`Checked.Marshal` produces deterministic bytes. `Checked.Digest` identifies the checked artifact.

See [Checked IR](https://github.com/josephjohncox/effectus/blob/main/ir/README.md) for the checker contract.

## Construct the startup generation

`runtime.CompileGeneration` resolves descriptors and builds an immutable executable generation.
The production daemon supports HTTP executor descriptors.
Generated gRPC is an inbound execution API, not an outbound executor descriptor.

Startup configures the engine, PostgreSQL stores, fencing provider, and recovery worker.
The daemon prepares enabled listeners before starting services.
If preparation fails, it closes listeners already acquired and releases owned resources.

A failed startup never replaces another process's active generation.
There is no candidate publication, generation-swap, refresh, or deployment-rollback phase in the current daemon.

## Admit work

HTTP, Kafka, generated gRPC, and recovery use `runtime.Engine.Execute`.
The engine records admission identity, normalized payload identity, ruleset, version, and pinned generation identity.
It records selected checked plans before external execution.

A matching retry returns the existing execution identity.
A conflicting payload for the same identity fails.
New admissions use the active generation. Existing identities retain their historical generation, even after a process replacement.

Accepted-only calls acknowledge durable admission, not successful business completion.
Terminal waits and replay preserve typed failure and blocked states.
See [the Go API guide](go-api.md) and [the gRPC capability matrix](grpc-capabilities.md).

## Execute and recover

The workflow runtime records each dispatch intent before invocation.
Completion requires current lease authority and an unexpired deadline.
Recovery keeps execution, plan, effect, and dispatch identities stable.
It consults persisted state and resolves the execution's pinned historical artifact when needed.

Completed results replay from durable state.
Unknown destination outcomes block by default.
A checked `SINK_GUARANTEED` step can retry a valid unknown outcome within its attempt limit, retaining the stable identity.
`KEY_REQUIRED` alone is insufficient. Unauthorized or exhausted unknown outcomes remain blocked.
Destination deduplication and fencing remain destination obligations, not guarantees created by metadata.
See [unknown-outcome handling](DURABLE_SAGA_PROTOCOL.md#unknown-outcomes).

## Replace and stop

A changed rule, contract, or descriptor requires a new source bundle and process replacement.
Keep historical artifacts and resolver support available for pending executions.
A process can resolve historical generations without replacing its active admission generation.

Shutdown first stops admission and cancels intake workers.
HTTP requests receive their configured grace period. Expiry cancels request contexts and closes connections, but does not terminate Go callbacks.
gRPC shutdown also waits for its handlers after forced cancellation.

The daemon joins handlers and workers before `Engine.Close` releases owned generations and resolver resources.
It closes the borrowed database after the engine finishes.
Non-cooperative callbacks can prolong this sequence beyond configured deadlines.

Read [Runtime Lifecycle](LIFECYCLE.md) for the full sequence.

## Compatibility boundary

The current embedded API also uses checked bundles and durable descriptors.
It does not accept anonymous Go continuations as production source.
Retained protobuf or Go compatibility declarations do not imply dynamic schema registration, extension-directory loading, or in-process plugins.
