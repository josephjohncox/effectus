# Repository Guidelines

## Project Structure & Module Organization
- `effectus/` holds protobuf API definitions (see `effectus/v1/*.proto`).
- Go source lives at the repo root (compiler, runtime, adapters, schema, etc.), with CLI entrypoints in `cmd/`.
- `cmd/` contains the `effectusc` and `effectusd` CLIs.
- `examples/` contains runnable samples and reference configurations.
- `docs/` captures architecture, design, and usage notes.
- `tools/vscode-extension/` hosts the VS Code extension code.
- `bin/` is the local build output directory (ignored by git).

## Build, Test, and Development Commands
- `just` or `just --list` shows all available workflows.
- `just build` runs protobuf generation and builds `bin/effectusc` and `bin/effectusd`.
- `just test` runs Go unit tests; `just test-coverage` writes `coverage.out`.
- `just lint` runs `golangci-lint` plus `buf lint` for protobufs.
- `just fmt` runs `go fmt` and `buf format`.
- `just test-integration` runs integration tests against Postgres (uses `DB_DSN`; defaults are in `justfile`).
- `just vscode-lint` / `just vscode-test` validate the VS Code extension.

## Coding Style & Naming Conventions
- Go: format with `go fmt` (tabs); prefer MixedCaps for exported identifiers and keep package/file naming consistent with existing modules.
- Protobuf: format with `buf format`; messages/enums use CamelCase, fields use snake_case.
- SQL (runtime): run `just sql-format` when changing SQL, and regenerate with `just sql-generate`.

## Testing Guidelines
- Go tests live alongside code as `*_test.go` plus suites in `tests/`.
- Integration tests are tagged `integration` and require a Postgres instance; use `just setup-test-db` then `just test-integration`.
- No explicit coverage threshold is enforced; use `just test-coverage` for a report.

## Commit & Pull Request Guidelines
- Commit history mixes short imperative subjects and conventional prefixes like `fix:`, `chore:`, and `doc:` (with occasional plain phrases). Keep subjects concise; if you adopt a `type:` prefix, keep it consistent within the PR.
- PRs should include a clear summary, tests run (for example: `just test`, `just lint`), and note any required regeneration (protobufs or SQL). Include screenshots only when changing the VS Code extension UI.

## Configuration & Local Services
- Database tasks use `DB_DSN` (see `justfile` defaults). For local setup, use `just setup-db` or `just setup-test-db`.
