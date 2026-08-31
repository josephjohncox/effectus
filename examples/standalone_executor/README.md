# Standalone Business Executor

This example runs Effectus as infrastructure. A separate Go service owns the business mutation.

The stack contains these services:

- `effectusd` compiles and executes the checked order rule.
- PostgreSQL stores Effectus execution state and business review records.
- `business-executor` receives invocation-aware HTTP calls from `effectusd`.
- The executor enforces the Effectus idempotency key in a PostgreSQL table.

## Run the complete path

Run this command from the repository root:

```bash
examples/standalone_executor/scripts/run.sh
```

Override both demonstration tokens when required:

```bash
EFFECTUS_API_TOKEN='api-token' \
EXECUTOR_TOKEN='executor-token' \
  examples/standalone_executor/scripts/run.sh
```

The script renders the executor token into a generated extension manifest under `out/standalone_executor`. The source manifest contains a non-secret placeholder.

The script performs these actions:

1. Builds a checked order-review bundle.
2. Builds the daemon and business executor images.
3. Starts PostgreSQL and applies Effectus migrations.
4. Starts `effectusd` and the business executor.
5. Submits one high-value order twice with the same idempotency key.
6. Confirms that both requests use one Effectus execution.
7. Confirms that the business database contains one review.

Open [http://127.0.0.1:18080/ui](http://127.0.0.1:18080/ui) after the script completes.

Inspect the business records:

```bash
export EXECUTOR_TOKEN='local-example-only'
curl --fail --silent \
  --header "X-Demo-Token: $EXECUTOR_TOKEN" \
  http://127.0.0.1:8090/reviews | python3 -m json.tool
```

Stop and remove the stack:

```bash
examples/standalone_executor/scripts/down.sh
```

## Integration contract

The verb manifest points to an HTTP endpoint:

```json
{
  "type": "http",
  "config": {
    "url": "http://business-executor:8090/reviews",
    "method": "POST",
    "timeout": "5s",
    "allowPrivateNetwork": true
  }
}
```

Effectus sends the declared verb arguments as the JSON body. It sends execution, saga, idempotency, hash, deadline, and fencing metadata in reserved headers.

The `executorhttp` package validates these headers and maps explicit outcomes to the Effectus protocol. The business handler must enforce the idempotency key before it changes external state.

The example stores these fields with each review:

- Execution ID
- Effect ID
- Idempotency key
- Argument hash
- Business result

A repeated idempotency key with the same argument hash returns the stored result. A repeated key with a different hash returns a permanent failure.

## Production changes

Replace the demonstration credentials before deployment. Use TLS and a secret manager for both service credentials.

Run PostgreSQL with backups and high availability. Restrict network access between `effectusd` and each executor.

Use an external fencing authority when a destination needs stale-writer rejection. Monitor blocked outcomes, retries, lease age, and executor latency.

This demo assumes that order IDs are globally unique. In a multi-tenant system, pass the tenant identity as a checked verb argument and include it in review IDs, database keys, and cancellation queries.
