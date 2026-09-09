# Authenticated gRPC Clients

The Go and Python programs in `examples/grpc_execution` call the shipped `ExecuteRuleset` service.
They use the same checked-in order-review scenario, not unrelated request fragments.
They request terminal completion and print the explicit response fields.

## Prerequisites

Run the commands from the repository root.
You need the repository's Go toolchain, Python 3.10 or later with `venv` and pip, and the pinned example requirements.
Package installation needs network access or a suitable package cache.
The race tests need a supported Go race-detector toolchain.

The automated gate needs no PostgreSQL, Docker, existing daemon, certificate files, or API token.
It creates its own loopback listeners, token, temporary certificates, in-memory engine, and example executor.
It closes the clients and service before closing the engine.
It does not establish durable destination behavior, daemon deployment readiness, or crash recovery.

## Install the Python tools

These packages belong to an isolated example environment. They do not change daemon dependencies.

```bash
python3 -m venv .tools/grpc-example-venv
.tools/grpc-example-venv/bin/python -m pip install -r examples/grpc_execution/requirements.txt
```

Use the venv executable path directly. Do not resolve its symlink to the base Python interpreter.
The base interpreter does not automatically use the venv's installed packages.

## Run both clients and test TLS

```bash
EFFECTUS_EXAMPLE_PYTHON="$PWD/.tools/grpc-example-venv/bin/python" \
  go test -race -count=1 -timeout=90s -v ./examples/grpc_execution
```

This is the automated onboarding gate. It:

- Generates Python bindings from the current `common.proto` and `execution.proto` into a fresh temporary directory
- Starts a matching authenticated gRPC service using the shared order-review rule
- Runs the actual Go main function and Python program in subprocesses from unrelated working directories
- Checks missing/incorrect credentials, successful completion, replay, and conflicting logical content
- Requires both languages to return the same execution ID and generation digest, with one executor call
- Runs both languages over verified TLS and explicit plaintext
- Separately rejects untrusted roots, a wrong hostname, and automatic fallback from TLS to plaintext
- Checks help, invalid options, deadline bounds, and token redaction

Without `EFFECTUS_EXAMPLE_PYTHON`, ordinary Go tests explicitly skip the Python gate.
That skip is not Python validation. A selected but unusable interpreter fails the gate rather than skipping it.
`just test-examples` also runs the Go client tests. Set the variable above and use the uncached command for the complete gate.

## Call an existing service

The automated fixture is a test service, not a long-running daemon command.
For a deployed or local daemon, follow [gRPC Execution](GRPC_EXECUTION.md).
That path needs a matching source bundle, PostgreSQL migrations, working executor bindings, an API token, and appropriate transport configuration.
Do not reuse a Docker-only executor hostname from the durable example on an unrelated host network.

Both clients default to this shared scenario:

| Setting | Value |
| --- | --- |
| Address | `127.0.0.1:9091` |
| Ruleset and version | `order-review`, `1.0.0` |
| Namespace | `merchant-42` |
| Idempotency key | `order-200-created` |
| Order | `order-200`, total `2499`, currency `USD`, risk score `82` |
| Transport | TLS with system trust roots |
| Client deadline | 10 seconds |

The program reads `examples/order_review/data/order.json` from this checkout.
The matching rule uses `order.id`, `order.total`, and `order.risk_score`.
The server's verb binding must implement that rule's contract.
A different bundle can require different facts. Changing only `--ruleset` does not construct those facts.

Set `EFFECTUS_API_TOKEN` to the server's configured token before running either client.
Prefer that environment variable to `--token`, which exposes the token in process arguments.
Help output does not print the environment token.

### Go

For a server whose certificate chains to your supplied CA and identifies the requested host:

```bash
: "${EFFECTUS_API_TOKEN:?set the server API token}"
go run ./examples/grpc_execution \
  --address=127.0.0.1:9091 --ca-file=/path/to/ca.pem
```

Omit `--ca-file` to use system trust roots.
For an explicitly configured local plaintext server, omit the CA flag and pass `--allow-insecure`.
There is no automatic plaintext fallback or hostname-verification bypass.

### Python

Generate the bindings beside their proto package before running the program manually:

```bash
.tools/grpc-example-venv/bin/python -m grpc_tools.protoc -I. \
  --python_out=. --pyi_out=. --grpc_python_out=. \
  effectus/v1/common.proto effectus/v1/execution.proto
```

The generated Python files are ignored by Git. Regenerate them after changing the protos.
Set `PYTHONPATH` to the repository root so Python can import `effectus.v1`:

```bash
: "${EFFECTUS_API_TOKEN:?set the server API token}"
PYTHONPATH="$PWD" .tools/grpc-example-venv/bin/python examples/grpc_execution/client.py \
  --address=127.0.0.1:9091 --ca-file=/path/to/ca.pem
```

Python also defaults to verified TLS. Use `--allow-insecure` only for an explicitly configured local plaintext server.
Do not combine that flag with `--ca-file` in either client.

## Results, replay, and options

A successful terminal call emits a single JSON object:

```json
{
  "execution_id":"<opaque execution ID>",
  "state":"EXECUTION_STATE_COMPLETED",
  "generation_digest":"<pinned generation digest>",
  "durably_accepted":true,
  "completed":true,
  "success":true
}
```

Run the same command again to replay the same logical identity.
Use `--order-id=other` with the same key to exercise an identity conflict.
Use a new key only for a genuinely different logical operation.

Both programs accept `--address`, `--token`, `--ruleset`, `--version`, `--namespace`, `--idempotency-key`, `--order-id`, `--ca-file`, and `--allow-insecure`.
Go's `--timeout` uses duration syntax, such as `5s`. Python's `--timeout` uses seconds, such as `5`.
Both require a positive deadline no longer than five minutes. The server can impose a shorter deadline.
Both exit 0 on success/help, 2 for command syntax errors, and 1 for validation or RPC errors.

These small clients print only the RPC status code on failure. They do not print remote status details or wrapped destination errors.
They do not interpret an RPC error as proof that admission or an external effect failed to commit.
Applications that need a failed execution's durable disposition must inspect the approved `ExecutionResponse` status detail.
For Go, use `status.Convert(err).Details()`. See [gRPC Execution](GRPC_EXECUTION.md) for the response and wait contract.

`google.protobuf.Struct` stores numbers as doubles. It cannot preserve every signed int64 value.
The shared example's numeric values are representable. That does not establish exact transport for arbitrary integers.

## Status codes

| Status | Meaning |
| --- | --- |
| `Unauthenticated` | Authentication failed. |
| `NotFound` | The requested ruleset or execution is unavailable. |
| `InvalidArgument` | Invalid request, unsupported option, or schema-validation request. |
| `AlreadyExists` | Conflicting content for the same logical identity. |
| `FailedPrecondition` | Generation mismatch or unsuccessful terminal execution. Terminal failures include durable response details. |
| `Canceled` / `DeadlineExceeded` | The call ended without proving noncommit. |
| `ResourceExhausted` | A server limit was exceeded. |
| `Unavailable` | An unavailable dependency or another recovery owner. |
| `Aborted` | An optimistic state conflict. Retry the same identity when appropriate. |
| `Internal` | An unexpected internal failure, not necessarily a business failure. |
| `Unimplemented` | A reserved or unregistered RPC. |

Python prints uppercase status names, such as `ALREADY_EXISTS`. Go prints names such as `AlreadyExists`.
The [capability matrix](grpc-capabilities.md) defines the supported RPC and field surface.
