# Embedded Order Review

This example embeds the checked Effectus runtime in a Go service. It does not start `effectusd`.

The application performs these actions:

1. Declares typed order facts.
2. Registers an invocation-aware Go business handler.
3. Compiles an embedded `.eff` rule into checked IR.
4. Executes a high-value order through `runtime.Engine`.
5. Replays the same idempotency key without a duplicate review.

Run the example from the repository root:

```bash
cd examples
go run ./embedded_orders
```

The output contains one execution ID and one review:

```json
{
  "completed": true,
  "execution_id": "...",
  "replayed_execution": "...",
  "review_count": 1
}
```

Use embedded mode when the host application owns process lifecycle and business handlers. The default embedded stores are process-local.

Use `effectusd` when executions must survive a process or host restart. See the [standalone executor example](../standalone_executor/README.md).
