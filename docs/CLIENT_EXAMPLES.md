# gRPC Client Examples

Clients use `effectus.v1.RulesetExecutionService` from `effectus/v1/execution.proto`.
Generate client bindings with the repository Buf configuration.

## Go client

The complete compile-tested form is in [`examples/grpc_execution/main.go`](https://github.com/josephjohncox/effectus/blob/main/examples/grpc_execution/main.go).

```go
facts, err := structpb.NewStruct(map[string]any{
    "order_id": "order-42",
    "total_cents": 12500,
})
if err != nil {
    return err
}

connection, err := grpc.NewClient(
    "effectus.example:8081",
    grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
)
if err != nil {
    return err
}
defer connection.Close()

ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)

response, err := effectusv1.NewRulesetExecutionServiceClient(connection).ExecuteRuleset(
    ctx,
    &effectusv1.ExecutionRequest{
        RulesetName:   "orders",
        Version:       "1.0.0",
        Namespace:     "tenant-a",
        IdempotencyKey: "order-42",
        TypedFacts:     facts,
        WaitMode: effectusv1.ExecutionWaitMode_EXECUTION_WAIT_MODE_TERMINAL,
    },
)
if err != nil {
    return err
}
```

Use the same idempotency key when you retry one logical request.
Set `generation_digest` when the client requires one exact generation.
Only `options.timeout_seconds` is supported. `schema_validation` and all other `ExecutionOptions` fields are unsupported and return field-specific `InvalidArgument` errors.

## Python request

```python
request = execution_pb2.ExecutionRequest(
    ruleset_name="orders",
    version="1.0.0",
    namespace="tenant-a",
    idempotency_key="order-42",
    typed_facts=struct_pb2.Struct(fields={
        "order_id": struct_pb2.Value(string_value="order-42"),
        "total_cents": struct_pb2.Value(number_value=12500),
    }),
    wait_mode=execution_pb2.EXECUTION_WAIT_MODE_TERMINAL,
)

response = stub.ExecuteRuleset(
    request,
    timeout=5,
    metadata=(("authorization", "Bearer " + token),),
)
```

Use a secure channel with the server certificate.

## Status codes

- `Unauthenticated` means that authentication failed.
- `NotFound` means that the requested ruleset version is unavailable.
- `InvalidArgument` means that the request is invalid.
- `AlreadyExists` means that the idempotency identity conflicts with another request.
- `FailedPrecondition` means that the generation constraint failed.
- `Canceled` means that the client canceled the call.
- `DeadlineExceeded` means that the execution deadline expired.
- `ResourceExhausted` means that the request exceeded a server limit.
- `Unavailable` means that the checked generation is unavailable.
- `Internal` means that execution failed.

Always set a client deadline.
The server also applies its configured maximum execution duration.

Management RPCs return `Unimplemented`.
