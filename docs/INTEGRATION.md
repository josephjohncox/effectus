# Integration Guide

Effectus supports two integration models. Choose one model for each service boundary.

## Choose an integration model

| Requirement | Embedded library | Standalone `effectusd` |
| --- | --- | --- |
| Run rules inside an existing Go process | Yes | No |
| Keep business handlers in the same process | Yes | No |
| Isolate rule execution from business services | No | Yes |
| Survive daemon or host restarts by default | No | Yes, with PostgreSQL |
| Use HTTP, Kafka, or generated gRPC admission | Custom host wiring | Built in |
| Deploy with the Effectus Helm chart | No | Yes |
| Best fit | Small Go services and local decisions | Shared business automation and durable workflows |

Use the embedded library for a local decision boundary. Use `effectusd` for durable cross-service execution.

## Embedded library

The `embedded` package hides compiler, loader, generation, and engine setup. It still uses checked IR and `runtime.Engine`.

This section uses the current embedded API. It requires the first published root
release that contains this branch. Published `v0.3.0` is not that release, so do
not combine its install command with the code below.

Set `EFFECTUS_VERSION` to the exact, immutable release tag that contains this
API, then install that tag:

```bash
: "${EFFECTUS_VERSION:?set this to the published release tag that contains the current embedded API}"
go get github.com/josephjohncox/effectus@"${EFFECTUS_VERSION}"
```

The command deliberately has no `@main` fallback. Pin the same release tag in
your module file and test the integration against that release before deployment.

Build one runtime during application startup:

```go
application, err := embedded.New("order-review", "1.0.0").
  AddFact("order.id", "").
  AddFact("order.total", 0.0).
  AddFact("order.risk_score", int64(0)).
  AddSource("order_review.eff", ruleSource).
  AddVerb(embedded.Verb{
    Name:         "RequestManualReview",
    ArgTypes:     map[string]string{"orderId": "string", "reason": "string"},
    RequiredArgs: []string{"orderId", "reason"},
    ReturnType:   "string",
    Capabilities: []string{"write", "create", "idempotent"},
    Resources: []embedded.Resource{{
      Name: "order_review",
      Capabilities: []string{"write", "create", "idempotent"},
    }},
    Handler: reviewService.RequestReview,
  }).
  Build(ctx)
if err != nil {
  return err
}
defer application.Close()
```

Execute facts with a tenant namespace and an idempotency key:

```go
result, err := application.Execute(ctx, embedded.Request{
  Namespace:      "merchant-42",
  IdempotencyKey: "order-200-created",
  Facts: map[string]any{
    "order": map[string]any{
      "id": "order-200", "total": 2499.00, "risk_score": int64(82),
    },
  },
})
```

`AddFact` values are type samples. They are not runtime defaults. Supply each fact that a selected predicate or verb argument requires.

Requests can use nested objects or dotted paths. If one request supplies both forms for the same path, the explicit dotted path takes precedence.

The same namespace, key, ruleset, version, and facts return the same execution ID. Different facts with the same identity fail.

The default embedded ledger and outbox are process-local. They do not survive an application restart.

The [Getting Started guide](GETTING_STARTED.md#path-1-embedded-go) contains the tested command and expected replay output.

Read [`examples/embedded_orders`](https://github.com/josephjohncox/effectus/tree/main/examples/embedded_orders) for the handler and runtime structure.

## Standalone business executor

In standalone mode, `effectusd` owns compilation, durable admission, dispatch, recovery, and compensation. Business services own business mutations.

```text
client or event source
        |
        v
     effectusd  ------> PostgreSQL execution state
        |
        | checked invocation with identity metadata
        v
business executor ----> business database or external API
```

A verb manifest connects each production verb to the canonical HTTP target.

### HTTP target

Declare the endpoint in an extension manifest:

```json
{
  "name": "RequestManualReview",
  "argTypes": {"orderId": "string", "reason": "string"},
  "requiredArgs": ["orderId", "reason"],
  "returnType": "string",
  "capabilities": ["write", "create", "idempotent"],
  "target": {
    "type": "http",
    "config": {
      "url": "https://orders.internal.example/reviews",
      "method": "POST",
      "timeout": "5s"
    }
  }
}
```

Effectus sends verb arguments as JSON. Reserved headers carry execution, effect, attempt, idempotency, contract, deadline, and fencing metadata.

### Go HTTP executor

The `executorhttp` package validates the request and writes protocol outcomes:

```go
handler, err := executorhttp.NewHandler(
  executorhttp.Options{},
  func(ctx context.Context, request invocation.Request) invocation.Outcome {
    stored, err := reviews.InsertOnce(
      ctx,
      request.Metadata.Saga.IdempotencyKey,
      request.ArgumentHash,
      request.Arguments,
    )
    if err != nil {
      return executorhttp.Unknown(err)
    }
    return executorhttp.Success(stored.ReviewID)
  },
)
```

Mount the handler on the URL from the verb manifest. Put service authentication outside this handler.

### Idempotency rule

The destination must enforce the Effectus idempotency key before it changes business state.

Store these values in the same transaction as the business mutation:

- Idempotency key
- Argument hash
- Business result
- Execution ID
- Effect ID

Return the stored result when the key and argument hash match. Return a permanent failure when the same key has different arguments.

Use globally unique business IDs. For multi-tenant systems, pass the tenant identity as a checked verb argument and include it in destination keys and queries.

Do not report a retryable failure after an unknown commit. Report `unknown_outcome` and let Effectus block the execution for operator review.

### Outcome mapping

| Helper | Meaning | Typical HTTP status |
| --- | --- | --- |
| `executorhttp.Success` | The operation committed | 200 |
| `executorhttp.Retryable` | The operation did not commit and can retry | 503 |
| `executorhttp.Permanent` | The request cannot succeed | 422 |
| `executorhttp.StaleFence` | The destination rejected a stale writer | 409 |
| `executorhttp.Unknown` | The commit result is not known | 500 |

The response also includes `X-Effectus-Outcome` for every failure.

## Run the durable example

The durable example uses `effectusd`, PostgreSQL, Docker Compose, and a separate business executor.

The [Getting Started guide](GETTING_STARTED.md#path-2-durable-docker) contains the tested command, restart proof, conflict output, and cleanup warning.

Read [`examples/standalone_executor`](https://github.com/josephjohncox/effectus/tree/main/examples/standalone_executor) for the service and file maps.

## Deployment structure

Use this structure for a standalone deployment:

1. Build and sign one immutable rules bundle.
2. Deploy `effectusd` with PostgreSQL and the bundle digest.
3. Deploy each business executor as a normal internal service.
4. Store executor credentials in Kubernetes Secrets.
5. Restrict network paths from `effectusd` to declared executor endpoints.
6. Make each destination enforce idempotency or fencing.
7. Monitor retries, unknown outcomes, blocked executions, and lease age.

Use the [Helm chart](https://github.com/josephjohncox/effectus/tree/main/charts/effectusd) for `effectusd`. Deploy business executors with their owning service charts.

## Test the boundary

Test these cases before production:

1. Submit the same idempotency key twice.
2. Submit different arguments with the same key.
3. Stop the executor before a request.
4. Drop the connection after the destination commits.
5. Restart `effectusd` during recovery.
6. Reject a stale fencing token at the destination.
7. Confirm that credentials do not appear in logs or artifacts.

Read [Runtime Guarantees](GUARANTEES.md) for the exact Effectus boundary.
