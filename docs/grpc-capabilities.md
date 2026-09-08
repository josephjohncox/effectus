# Shipped gRPC capabilities

This contract applies to `effectusd` and the servers built by `runtime.NewRulesetExecutionServerWithOptions`.
Generated Go clients and interfaces do not imply a server implementation.
Consumers can implement those interfaces in their own servers. Such implementations are outside this contract.

## Service and RPC matrix

Only `RulesetExecutionService` is registered by the shipped server.
`ExecuteRuleset` is its only implemented RPC.

| Service | RPC | Shipped behavior |
| --- | --- | --- |
| RulesetExecutionService | ExecuteRuleset | Supported admission, execution, and identity replay. |
| RulesetExecutionService | GetRulesetInfo | Reserved. Returns `Unimplemented`. |
| RulesetExecutionService | ListRulesets | Reserved. Returns `Unimplemented`. |
| RulesetExecutionService | RegisterRuleset | Reserved. Returns `Unimplemented`. |
| RulesetExecutionService | UnregisterRuleset | Reserved. Returns `Unimplemented`. |
| RulesetExecutionService | StreamExecution | Reserved. Returns `Unimplemented`. No execution stream exists. |
| RulesetExecutionService | ValidateSchema | Reserved. Returns `Unimplemented`. |
| RulesetExecutionService | GetSchemaVersion | Reserved. Returns `Unimplemented`. |
| FactRegistryService | RegisterFactSchema | Unregistered. Returns `Unimplemented`. |
| FactRegistryService | GetFactSchema | Unregistered. Returns `Unimplemented`. |
| FactRegistryService | ListFactSchemas | Unregistered. Returns `Unimplemented`. |
| FactRegistryService | ValidateFactData | Unregistered. Returns `Unimplemented`. |
| FactRegistryService | CheckCompatibility | Unregistered. Returns `Unimplemented`. |
| VerbRegistryService | RegisterVerbInterface | Unregistered. Returns `Unimplemented`. |
| VerbRegistryService | GetVerbInterface | Unregistered. Returns `Unimplemented`. |
| VerbRegistryService | ListVerbInterfaces | Unregistered. Returns `Unimplemented`. |
| VerbRegistryService | ValidateVerbCall | Unregistered. Returns `Unimplemented`. |
| VerbRegistryService | CheckInterfaceCompatibility | Unregistered. Returns `Unimplemented`. |
| VerbRegistryService | GenerateVerbCode | Unregistered. Returns `Unimplemented`. |

Reserved RPCs carry method or service deprecation markers in the protobuf descriptors.
`runtime/grpc_capabilities_test.go` tests every reserved RPC against a real server, including the stream.
The test also detects service or method additions that require a capability review.

## ExecuteRuleset fields

- `ruleset_name` and `version` must match the server registration.
- `namespace` and `idempotency_key` are required. The server trims whitespace before it computes identity.
- `typed_facts` supplies a `google.protobuf.Struct`.
  Its number representation cannot preserve every signed 64-bit integer exactly.
  Use HTTP JSON when exact integer text is required.
- The deprecated `facts` alias can wrap `google.protobuf.Struct` in `Any`.
  It cannot carry arbitrary message types. `typed_facts` takes precedence when both fields exist.
- `wait_mode` selects durable acceptance or terminal execution. Its unspecified value selects terminal execution.
- `generation_digest` optionally constrains the pinned replay artifact or the active generation for a new identity.
- `options.timeout_seconds` accepts a positive timeout or zero for the server limit.
  It cannot extend a shorter caller or server deadline. Negative values fail.
- Non-default `dry_run`, `max_effects`, `enable_tracing`, `capability_filter`, and schema-version options fail with `InvalidArgument`.
  Use HTTP `/v1/dry-run` or the supported Go dry-run API instead of `options.dry_run`.
- Any supplied `schema_validation` message fails with `InvalidArgument`.
- The deprecated `trace_id` field is ignored. It does not enable tracing or affect admission identity.

The response separates `durably_accepted` from `completed`.
`success` means successful completion only. `state` reports the durable disposition.
`generation_digest` reports the pinned generation.
The existing metadata map retains the state, digest, ruleset, and version.

Failed terminal calls return a sanitized `FailedPrecondition` status with an `ExecutionResponse` status detail.
Go clients can inspect `status.Convert(err).Details()`.
Accepted-only replay acknowledges a durable identity without reporting business failure as an RPC error.

`start_time` and `end_time` describe this call's observation interval.
They are not persisted execution timestamps. Replay produces new observation times.
`effects`, `warnings`, and `schema_info` are reserved and unpopulated.
The server does not return effect arguments or results through those fields.

## Compatibility policy

Reserved service names, method names, message names, and field numbers remain allocated.
Do not delete or reuse them to advertise a smaller implemented surface.
Deprecation marks unsupported behavior. It does not grant permission to remove wire identities.

The legacy `CompiledRuleset` registration payload is not the checked IR artifact produced by `effectusc`.
Use the bundle and checked-generation APIs for the supported compiler/runtime path.
The registry messages are not a durable schema registry or a Buf-backed control plane.

Protocol lint and compatibility checks accompany descriptor changes.
Broader transport behavior and shutdown limits are recorded in [M4 validation](audits/m4-transport-validation.md).
