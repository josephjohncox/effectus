# Authenticated Go and Python gRPC Clients

Both clients execute the shared `order-review` scenario over the shipped gRPC service.
They default to verified TLS and use `EFFECTUS_API_TOKEN` for bearer authentication.
They print the explicit execution state, generation digest, and admission/completion fields.

Use the [client guide](../../docs/CLIENT_EXAMPLES.md) for prerequisites, Python bindings, service setup, options, output, and retry behavior.
Use the [gRPC reference](../../docs/GRPC_EXECUTION.md) for daemon configuration and supported request fields.

## Automated local gate

From the repository root, with the Go toolchain and Python 3.10 or later:

```bash
python3 -m venv .tools/grpc-example-venv
.tools/grpc-example-venv/bin/python -m pip install -r examples/grpc_execution/requirements.txt
EFFECTUS_EXAMPLE_PYTHON="$PWD/.tools/grpc-example-venv/bin/python" \
  go test -race -count=1 -timeout=90s -v ./examples/grpc_execution
```

The gate creates a matching loopback service, example executor, token, and test certificates.
It runs both client programs and checks authentication, cross-language replay, conflict rejection, TLS trust, and hostname verification.
It generates Python bindings from the current protos in a temporary directory.
No database, Docker stack, or operator credentials are used.

Without the interpreter variable, Go tests explicitly skip Python validation.
A selected interpreter with missing requirements fails instead of skipping.

The fixture uses in-memory state and counts executor calls.
It does not implement or certify durable destination deduplication, fencing, or crash recovery.
For a persistent daemon, follow the service setup in the client guide instead.
