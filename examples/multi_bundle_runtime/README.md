# Multi-Bundle Runtime Example

This example demonstrates resolving multiple bundles from a manifest, loading their schemas/verbs/rules, and executing a merged list ruleset. It also includes a hot-reload loop that swaps bundle versions by changing the manifest.

## Run Once

```bash
go run ./examples/multi_bundle_runtime
```

Run from the repository root so the relative registry path in the manifest resolves correctly.

## Hot Reload Demo

```bash
./examples/multi_bundle_runtime/scripts/hot-reload.sh
```

The script starts the runtime in `--watch` mode, waits a few seconds, then swaps the manifest to point at `customer-core@1.1.0`.

## Layout

- `manifest.json`: active manifest (copied from `manifest.v1.json` by default)
- `manifest.v1.json` / `manifest.v2.json`: two bundle selections for hot reload
- `bundles/`: local file registry with bundle metadata + schema/verbs/rules
- `facts.json`: sample facts payload
- `main.go`: manifest resolver + compiler + execution
