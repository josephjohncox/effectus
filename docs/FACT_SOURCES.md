# Fact Sources

`effectusd` accepts facts through its documented HTTP admission API. It validates authentication, request limits, and `Idempotency-Key` before durable admission.

Kafka ingestion remains a daemon-operated transport when configured. It is not a public Go adapter library. The daemon records delivery and poison state in PostgreSQL; Kafka offset commits and external effects are not one atomic transaction.

External Go programs can use `bundle`, `invocation`, and `embedded` for in-process execution, or `runtime` and its storage contracts for durable integrations. See the [Go API guide](go-api.md).

## HTTP admission

Send the facts and an idempotency key to the daemon endpoint. HTTP `202 Accepted` means PostgreSQL has durably admitted the execution. A retry with the same key and payload returns the same execution. A changed payload for that key fails.

See [Runtime configuration](RUNTIME_CONFIG.md), [Commands](COMMANDS.md), and [Runtime guarantees](GUARANTEES.md) for the complete daemon contract.
