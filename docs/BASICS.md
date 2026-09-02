# Basics

Effectus evaluates rules from an immutable SourceBundle. A producer builds the
bundle with `bundle.New`; `effectusc check`, `compile`, and `inspect` validate
or describe that fixed input.

The daemon accepts one bundle at startup. `POST /v1/execute` requires bearer
authentication, an `Idempotency-Key` header, and a JSON body with `namespace`
and `facts`. `universe` is accepted as a compatibility alias when `namespace`
is empty. New clients should send `namespace`.

The daemon returns HTTP 202 after durable admission, not after an external verb
finishes. Retry the same logical request with the same key and content. A
changed payload for that identity returns HTTP 409.

See [Commands](COMMANDS.md), [Lifecycle](LIFECYCLE.md), and
[Guarantees](GUARANTEES.md) for the supported contract.
