# Learn Effectus with Two Executable Rules

This tutorial starts with facts and finishes with replay and compiler diagnostics.
It requires Go 1.26 or later and the repository checkout. Use the patched toolchain pinned in `go.mod`.
Run commands from the repository root. No database, Docker, Python, or network service is needed.

The tutorial uses process-local executor implementations identified by explicit embedded descriptors.
It is not a production daemon deployment. Its ledger, business state, and deduplication records disappear when the process exits.

## 1. Facts are the input

A fact is a typed value that a predicate or argument can read.
The default tutorial input is:

```json
{
  "order": {
    "id": "order-200",
    "total": 2499,
    "risk_score": 82
  }
}
```

The environment declares these paths:

| Path | Type | Use |
| --- | --- | --- |
| `order.id` | `string` | Identifies the order sent to a verb. |
| `order.total` | `float` | Selects orders above the tutorial threshold. |
| `order.risk_score` | `int` | Selects high-risk orders. |

A nested input object can supply a dotted fact path.
Declarations describe the types. They do not supply the input values.
The compiler rejects a rule that reads an undeclared path.

## 2. Verb contracts describe operations

A verb is an operation the runtime can invoke after a predicate matches.
A contract states its argument names, argument types, required arguments, and result type.
The contract is not the implementation.

| Verb | Required arguments | Result |
| --- | --- | --- |
| `RequestManualReview` | `orderId: string`, `reason: string` | `string` ticket |
| `RecordReview` | `orderId: string`, `ticket: string` | `void` |

The tutorial returns `ticket:order-200` from the first operation.
The second operation records that ticket.
It rejects a ticket that the earlier operation did not create.

The environment and descriptor definitions are in [bundle.go](https://github.com/josephjohncox/effectus/blob/main/examples/embedded_orders/tutorial/bundle.go).

## 3. Write a list rule with `.eff`

This is the checked-in `examples/embedded_orders/tutorial/rules/review.eff`:

```eff
rule "ReviewAndRecord" priority 10 {
  when {
    order.total > 1000 || order.risk_score > 75
  }
  then {
    ticket = RequestManualReview(orderId: order.id, reason: "value_or_risk")
    RecordReview(orderId: order.id, ticket: $ticket)
  }
}
```

`when` selects work. `then` contains the ordered invocations.
The first call binds its result to `ticket`.
The later `$ticket` argument reads that result slot, not an input fact.

Run it:

```bash
go run ./examples/embedded_orders/tutorial -dialect=eff
```

The program executes the request, then repeats the same identity.
Its JSON output has `completed: true`, equal execution and replay IDs, and these operations:

```json
[
  {"verb": "RequestManualReview", "ticket": "ticket:order-200"},
  {"verb": "RecordReview", "ticket": "ticket:order-200"}
]
```

Replay does not add two more business operations.
This proves replay within this process, not persistence across process restarts.

## 4. Express the same dependency with `.effx`

This is the checked-in `examples/embedded_orders/tutorial/rules/review.effx`:

```effx
flow "ReviewAndRecord" priority 10 {
  when {
    order.total > 1000 || order.risk_score > 75
  }
  steps {
    ticket = RequestManualReview(orderId: order.id, reason: "value_or_risk")
    RecordReview(orderId: order.id, ticket: $ticket)
  }
}
```

A flow uses `steps` to state its ordered work.
Here, the two dialects select the same operations and use the same result dependency.
They are distinct source artifacts, not aliases for the same artifact digest.

```bash
go run ./examples/embedded_orders/tutorial -dialect=effx
```

The operation list must match the `.eff` run.
Compare replay IDs within each run. The tutorial uses a different namespace for each dialect.

### Ordering rules

- A step can use only results bound by earlier steps.
- A result binding cannot be redefined.
- A `void` result cannot be bound to a value.
- Named argument order does not change argument meaning.
- Higher-priority plans precede lower-priority plans.
- Equal-priority plans use deterministic source-path and declaration order, not the caller's source-slice order.

The compiler turns these dependencies into checked IR result slots.
It does not infer that external operations commute or can execute in parallel safely.

## 5. Change the facts, not the rule

Use a total and risk score below both thresholds:

```bash
go run ./examples/embedded_orders/tutorial -dialect=effx -total=25 -risk-score=10
```

The result is completed with an empty `operations` array.
No matching plan is a valid result, not a compiler failure.
Changing facts does not require a new bundle. Changing a rule or contract does.

## 6. Bundle identity and resolver binding

`bundle.New` combines sources, the declaration environment, executor descriptors, name, and version.
Each descriptor contains an explicit resolver ID and reference.
The tutorial registers `example/language-tutorial/v1` through `invocation.NewRegistry`.
`embedded.Open` compiles the bundle and resolves its descriptors once.

The resolver returns an `invocation.Executor`, not an anonymous callback stored in IR.
The executor in [executor.go](https://github.com/josephjohncox/effectus/blob/main/examples/embedded_orders/tutorial/executor.go) protects its local business commit and deduplication records with one mutex.
It receives invocation identity and hashes from the engine.
The runtime owns resources returned by resolvers and releases them on closure.

Print and check a canonical source bundle:

```bash
mkdir -p out/tutorial
go run ./examples/embedded_orders/tutorial -dialect=effx -bundle > out/tutorial/review.bundle.json
go build -o out/tutorial/effectusc ./cmd/effectusc
out/tutorial/effectusc check --bundle out/tutorial/review.bundle.json
out/tutorial/effectusc compile --bundle out/tutorial/review.bundle.json --output out/tutorial/review.checked.pb
out/tutorial/effectusc inspect --bundle out/tutorial/review.bundle.json
```

`review.bundle.json` contains sources and bindings. `review.checked.pb` contains checked IR bytes.
The shipped daemon consumes source bundles, not this compiled output file.
This tutorial bundle uses embedded descriptors that the daemon's HTTP-only resolver registry does not support.
For daemon execution, bind operations to supported HTTP descriptors and real destination services.
See [Integration](INTEGRATION.md).

## 7. Read a compiler diagnostic

These commands intentionally fail before invoking an executor:

```bash
go run ./examples/embedded_orders/tutorial -diagnostic=unknown-fact
go run ./examples/embedded_orders/tutorial -diagnostic=type-mismatch
go run ./examples/embedded_orders/tutorial -dialect=effx -diagnostic=future-binding
```

| Diagnostic case | Change | Correction |
| --- | --- | --- |
| `unknown-fact` | Reads `order.missing`. | Use a declared fact path with the expected type. |
| `type-mismatch` | Supplies `42` for a string argument. | Supply a compatible string value. |
| `future-binding` | Moves the consumer before the producer. | Bind the result before using it. |

Inspect standard error and the nonzero exit status.
Do not fix a type error by bypassing checked compilation.
Predicate functions and nested saga boundaries are not implemented by this tutorial.
Unavailable function calls fail compilation rather than becoming runtime plugin lookups.

## 8. Distinguish outcomes

The example uses terminal waits, so it returns completed work or a typed failure.
Accepted-only execution instead acknowledges a durable identity without promising business completion.
An HTTP 202 from the daemon is accepted-only, unlike this example's completed result.

An executor reports success, a known-not-committed retryable failure, permanent failure, an unknown outcome, or stale fencing.
An unknown outcome means the destination might have committed.
Do not convert it into a blind retry or assume compensation proves rollback.
Destination deduplication must coordinate with the business commit. Fencing does not replace it.

The automated tutorial tests compile both dialects, check IR round trips and result slots, execute ordering and replay, and reject invalid inputs.
Run them with:

```bash
go test -race ./examples/embedded_orders/tutorial
```

Continue with [the Go API](go-api.md), [Runtime Lifecycle](LIFECYCLE.md), and [Runtime Guarantees](GUARANTEES.md).
