# gRPC Execution

The shipped server registers `effectus.v1.RulesetExecutionService` before serving requests.
Only `ExecuteRuleset` is implemented.
The [capability matrix](grpc-capabilities.md) lists all 3 services and 19 RPCs, including reserved and unregistered capabilities.
Generated interfaces do not imply additional supported operations.

## Daemon configuration

Effectusd uses flags and supported environment variables, not a YAML configuration file.
Inbound gRPC is disabled by default because `--grpc-addr` defaults to an empty string.

Serve mode requires exactly one bundle source and PostgreSQL.
Set `EFFECTUS_POSTGRES_DSN` and `EFFECTUS_API_TOKEN` before starting this example.
The certificate must identify the host used by clients. Clients must trust its issuer.

```bash
: "${EFFECTUS_POSTGRES_DSN:?set the PostgreSQL DSN}"
: "${EFFECTUS_API_TOKEN:?set the API bearer token}"
effectusd --mode=serve --bundle order-review.json --http-addr= \
  --grpc-addr=127.0.0.1:9091 \
  --grpc-tls-cert=/run/tls/tls.crt --grpc-tls-key=/run/tls/tls.key
```

The database must have the required migrations. The default is migration validation, not automatic application.
See [Runtime Configuration](RUNTIME_CONFIG.md) for migration commands and source-bundle construction.

For local development only, replace the TLS flags with `--grpc-allow-insecure`.
Do not combine that flag with a certificate or key.
Plaintext mode does not disable bearer authentication.

## Authentication and TLS

The daemon uses one trimmed `EFFECTUS_API_TOKEN` for HTTP and gRPC.
Clients send gRPC metadata `authorization: Bearer TOKEN`.
If either listener is enabled, an empty token fails startup.
The daemon has no token-set, authentication-disable, or YAML authentication setting.

TLS requires both `--grpc-tls-cert` and `--grpc-tls-key` unless the explicit plaintext override is selected.
The daemon requires TLS 1.2 or later.
The HTTP admission listener is separate. Its gRPC TLS flags do not enable HTTPS.
Use an authenticated TLS-terminating boundary for HTTP when traffic leaves a trusted local environment.

## Limits and library-only options

The daemon uses these gRPC defaults and exposes no flags or environment variables to change them:

| Limit | Daemon value | Go library option |
| --- | --- | --- |
| Receive message size | 4 MiB | `MaxReceiveBytes` |
| Send message size | 4 MiB | `MaxSendBytes` |
| Execution duration | 30 seconds | `MaxExecutionDuration` |
| Concurrent RPCs | 128 | `MaxConcurrentRPCs` |

These fields belong to `runtime.RulesetExecutionServerOptions`.
Library zero values select the defaults. Negative values fail validation.
Library callers use `NewRulesetExecutionServerWithOptions` or the listener-based constructor.
They must configure generation identity, authentication, and transport policy before serving.

A library can provide a custom `GRPCAuthenticator`, a bearer-token set, or explicit `AllowUnauthenticated` policy.
`TLSConfig` and `AllowInsecureTransport` are separate transport choices.
These are library options, not daemon configuration keys.
An unauthenticated library setting does not imply plaintext transport, or the reverse.

## ExecuteRuleset

Supply a ruleset name, version, nonblank namespace, idempotency key, and object-valued `typed_facts`.
The legacy `facts` field accepts a compatible `google.protobuf.Struct` wrapper.
`typed_facts` takes precedence if both are supplied.

An optional `generation_digest` constrains the requested identity.
For a new admission it checks the active generation. For replay it checks the pinned historical generation.
An omitted digest does not force replay onto a replacement generation.

`google.protobuf.Struct` stores numbers as doubles.
It cannot preserve every signed 64-bit integer. The server cannot recover digits a client already rounded.

Only `ExecutionOptions.timeout_seconds` is supported.
Zero uses the server limit. A positive value cannot extend a shorter deadline. A negative value fails.
Non-default unsupported options and any `schema_validation` message return `InvalidArgument`.

The engine distinguishes durable admission from completed work.
Read `durably_accepted`, `completed`, `state`, and `generation_digest` rather than interpreting `success` as admission.
Unsuccessful terminal calls return sanitized `FailedPrecondition` with an `ExecutionResponse` status detail.
Accepted-only replay acknowledges the durable identity without returning its business failure as an admission error.

See [the capability matrix](grpc-capabilities.md) for field semantics and [client examples](CLIENT_EXAMPLES.md) for request shapes.
Internal causes do not appear in public status messages.

## Transport direction

Generated gRPC is an inbound API. Kafka is an inbound fact source.
The production daemon resolves outbound HTTP verb descriptors only.
It does not resolve outbound gRPC executor descriptors.
Outbound HTTP settings belong to each descriptor, not to gRPC server options.
