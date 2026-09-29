# Fact Sources

`effectusd` accepts facts through its documented HTTP admission API. It validates authentication, request limits, and `Idempotency-Key` before durable admission.

Kafka ingestion remains a daemon-operated transport when configured. It is not a public Go adapter library. The daemon records delivery and poison state in PostgreSQL; Kafka offset commits and external effects are not one atomic transaction.

The Kafka source's `commit_timeout` defaults to 10 seconds. It sets kafka-go's network timeout for consumer-group coordinator requests, including offset commits. The library does not support canceling an in-flight offset commit with a context, so this is a coordinator I/O deadline rather than a hard bound on the entire call. The broker's successful commit response determines success.

External Go programs can use `bundle`, `invocation`, and `embedded` for in-process execution, or `runtime` and its storage contracts for durable integrations. See the [Go API guide](go-api.md).

## HTTP admission

Send the facts and an idempotency key to the daemon endpoint. HTTP `202 Accepted` means PostgreSQL has durably admitted the execution. A retry with the same key and payload returns the same execution. A changed payload for that key fails.

See [Runtime configuration](RUNTIME_CONFIG.md), [Commands](COMMANDS.md), and [Runtime guarantees](GUARANTEES.md) for the complete daemon contract.
