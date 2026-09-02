# Repository guardrails

Run the same surface gate as CI:

```bash
just guardrails
```

The gate freezes and checks:

- every discovered `go.mod`; v0.3 compatibility packages are part of the root module;
- public packages and exported declarations from every repository module;
- immediate example directories and top-level product directories;
- visible Just recipes only; private helpers remain callable but do not expand the user interface;
- forbidden direct import edges from `forbidden-dependencies.tsv`;
- dated Go deprecations, CLI command and flag documentation contracts, and canonical HTTP-executor documentation claims.

The guardrail tests include negative fixtures for inventory growth, dependency edges, stale deprecations, stale executor claims, and visible recipe additions.

Do not refresh an inventory to silence a failure. Remove accidental growth first. For an approved change, review the new surface and run:

```bash
go run ./internal/guardrails/cmd snapshot
just guardrails
```

The root-module v0.3 compatibility policy is in [`docs/COMPATIBILITY.md`](../docs/COMPATIBILITY.md).
