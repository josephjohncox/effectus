# Generated gRPC execution client

This client calls the stable `RulesetExecutionService`. The local smoke starts PostgreSQL, applies migrations, builds a matching checked bundle, starts `effectusd` with explicit plaintext localhost gRPC, and runs the client.

From the repository root:

```bash
just grpc-execution-smoke
```

To call an existing local daemon from the examples module:

```bash
cd examples
go run ./grpc_execution \
  --address 127.0.0.1:8081 \
  --token local-demo-token \
  --ruleset orders \
  --version 1.0.0
```

Plaintext transport is for localhost development only. Use TLS credentials in production.
