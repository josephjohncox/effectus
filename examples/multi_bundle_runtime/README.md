# Multi-Bundle Runtime Example

This example resolves local bundles from a manifest. It loads their declarations and rules, then executes a merged list ruleset.

The watch mode swaps local bundle versions when the manifest changes. This is a library example, not production OCI polling.

## Run Once

```bash
cd examples && go run ./multi_bundle_runtime
```

Run the command as shown from the repository root. The example resolves its assets from its source directory.

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
