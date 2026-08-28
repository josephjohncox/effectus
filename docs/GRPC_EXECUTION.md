# gRPC Execution

Effectus exposes one generated service from `effectus/v1/execution.proto`.

The server registers `effectus.v1.RulesetExecutionService` before it calls `Serve`.
The server does not use `UnknownServiceHandler` or a mutable method registry.

`runtime/ruleset_execution.proto` remains only as a deprecated schema compatibility artifact. Effectusd does not register or implement that legacy service.

## Execution RPC

`ExecuteRuleset` accepts a `google.protobuf.Struct` fact set.
The request must specify a ruleset name, version, idempotency key, and `typed_facts`.
The legacy `facts` `Any` field remains readable only for wire compatibility.

The optional `generation_digest` field pins the request to one generation.
The server rejects a digest that does not match the active generation.
The engine checks the digest again during admission.

The service sends all requests to `runtime.Engine.Execute`.
The response includes the execution state and generation digest.

Only `ExecutionOptions.timeout_seconds` is supported. `dry_run`, `max_effects`, `enable_tracing`, `capability_filter`, `min_schema_version`, and `max_schema_version` are unsupported. The `schema_validation` message is also unsupported. A non-default unsupported field returns `InvalidArgument` naming that field.

## Management RPCs

The generated service includes these reserved management RPCs:

- `GetRulesetInfo`
- `ListRulesets`
- `RegisterRuleset`
- `UnregisterRuleset`

Effectusd returns gRPC `Unimplemented` for each management RPC.
Effectusd does not support runtime gRPC method registration.

## Security and limits

Effectusd requires TLS unless `grpc.allow_insecure` is true.
TLS uses version 1.2 or later.

Effectusd uses the configured API write tokens for gRPC bearer authentication.
Authentication can be disabled only with the API authentication setting.

The server applies these limits:

- Maximum request size
- Maximum response size
- Maximum execution time
- Maximum concurrent RPC count

The server maps internal failures to fixed gRPC status messages.
It does not return internal error text to a client.

## Effectusd configuration

```yaml
grpc:
  addr: "0.0.0.0:8081"
  tls_cert: "/run/secrets/tls.crt"
  tls_key: "/run/secrets/tls.key"
  max_receive_bytes: 4194304
  max_send_bytes: 4194304
  max_execution_duration: "30s"
  max_concurrent: 128
```

The gRPC service uses the PostgreSQL execution ledger.
Set `database.dsn` before you enable the service.

## Outbound gRPC verbs

The outbound executor uses TLS by default.
Set `insecure: true` only for a trusted plaintext test endpoint.

A verb can supply a protobuf descriptor set with these fields:

- `descriptorSet`
- `requestType`
- `responseType`

The executor validates the descriptor set before the first call.
It uses dynamic protobuf messages for the unary request and response.

The executor retries only an explicitly retry-safe call.
It retries only transient gRPC status codes.
The connection pool replaces closed connections and closes all connections during runtime shutdown.
