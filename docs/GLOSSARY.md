# Glossary

These terms describe the current checked runtime. [Theory terms](theory/README.md) do not add runtime features.

## Language and artifacts

- **Fact**: An input value at a declared path, such as `order.risk_score`. The checked environment declares its type.
- **Environment**: The fact, verb, function, and type declarations used during checking. A declaration does not supply an implementation.
- **Predicate**: An expression that decides whether a plan matches the input facts.
- **Function**: A declared expression operation. Current checked compilation rejects function calls without an available implementation; there is no built-in function library to call.
- **Verb**: A named operation with declared arguments and a result type. An executor supplies its behavior.
- **Binding**: A name for an earlier verb result within a plan. Later steps can consume it through `$name`; a void result cannot be bound.
- **Rule**: An `.eff` declaration with `rule`, `when`, and `then` sections. Its predicate selects an ordered sequence of verb steps.
- **Flow**: An `.effx` declaration with `flow`, `when`, and `steps` sections. It supports ordered result bindings, not arbitrary branching.
- **Ruleset**: A named and versioned collection of source declarations compiled together.
- **Source bundle**: The executable source input, environment declarations, and executor descriptors packaged for checked compilation. It is not daemon runtime configuration.
- **Checked IR**: The validated protobuf intermediate representation produced by the compiler. `effectusc compile` writes this artifact; `effectusd --bundle` instead consumes a source bundle.
- **Generation**: An immutable checked artifact with resolved executor resources. The daemon has one startup generation for new admissions.
- **Generation digest**: The artifact identity used to constrain admission and resolve historical execution. It is not the execution ID.
- **Extension**: A source-bundle declaration or executor binding resolved during compilation/startup. It is not a hot-reload plugin directory.

See [Basics](BASICS.md), [Go API](go-api.md), and [Extension System](EXTENSION_SYSTEM.md).

## Execution and ownership

- **Fact source**: Inbound data supplied to execution. The daemon supports HTTP requests and Kafka records; inbound gRPC calls also use the execution engine.
- **Executor**: The implementation of a verb invocation. The daemon uses outbound HTTP descriptors. Embedded Go applications can register in-process implementations.
- **Execution identity**: Namespace, idempotency key, ruleset name, and version. Matching replay retains the original execution ID and pinned artifact.
- **Namespace**: A component of execution identity. It is not a tenant authorization policy.
- **Admission**: Recording a logical execution identity. Durable admission does not imply successful business completion.
- **Accepted-only wait**: Return after admission or lookup of an existing durable disposition. The HTTP execution endpoint uses this mode, including unsuccessful historical replay.
- **Terminal wait**: Wait for completed, failed, or blocked disposition. Failed and blocked results are not successful completion.
- **Replay**: Reuse of an existing identity with matching logical inputs. Different inputs conflict instead of creating a second execution.
- **Recovery**: Resuming eligible durable work with an execution lease and its historical artifact. It is separate from changing the startup generation.
- **Lease**: Time-bounded ownership checked by the durable store. Expired or lost authority does not permit continued dispatch.
- **Fencing**: A destination check that rejects stale ownership when required by the contract. It does not prevent duplicate business effects by itself.
- **Idempotency**: Destination deduplication coordinated with the business commit. An HTTP header alone does not provide this guarantee.
- **Unknown outcome**: The runtime cannot determine whether an external operation committed. Blind retry can duplicate effects.
- **Inverse verb / compensation**: An application-defined recovery operation. It is not an automatic reversal or an ACID rollback.
- **Capability**: A term in the abstract models for permitted operations. It is not a general runtime permission or concurrency-policy engine.

See [HTTP API](HTTP_API.md), [gRPC Execution](GRPC_EXECUTION.md), [Runtime Lifecycle](LIFECYCLE.md), and [Runtime Guarantees](GUARANTEES.md).
