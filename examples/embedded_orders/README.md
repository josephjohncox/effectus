# Embedded Order Review

This example is the embedded first-run implementation. Use the [Getting Started guide](../../docs/GETTING_STARTED.md) for the tested command and expected output.

## Runtime behavior

The application performs these actions:

1. It loads the shared order-review rule.
2. It declares typed order facts.
3. It registers an invocation-aware Go handler.
4. It compiles the rule to checked IR.
5. It derives the embedded request and idempotency key from the shared scenario artifact.
6. It replays the idempotency key without a duplicate review.

The default ledger and outbox are process-local. The application creates no persistent state.

## File map

| File | Purpose |
| --- | --- |
| `main.go` | Defines the handler, builds the checked application, and checks replay |
| `../../internal/demo/orderreview/` | Provides the internal demo reader for the shared scenario assets |
| `../order_review/rules/order_review.eff` | Contains the shared checked rule |
| `../order_review/data/order.json` | Contains the shared idempotency key and HTTP request |

Use the durable path when execution state must survive a process restart.
