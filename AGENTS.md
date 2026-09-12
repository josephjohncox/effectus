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
- `just install` installs the pinned generators into `.tools/bin`. Add that directory to `PATH` for direct tool commands, `just fmt`, and `just lint`.
- `just build` generates Go protobuf bindings and runtime SQLC bindings, then builds `bin/effectusc` and `bin/effectusd`.
- `just test` runs Go unit tests. Use `go test -coverprofile=coverage.out ./...` for coverage.
- `just test-examples` checks shared scenarios, embedded onboarding, the list/flow tutorial, and live Go gRPC authentication/TLS. Python requires the explicit interpreter setting in `docs/CLIENT_EXAMPLES.md`. A skipped Python gate is not Python validation.
- `just lint` runs `golangci-lint` plus `buf lint` for protobufs.
- `just fmt` runs `go fmt` and `buf format`.
- `just test-integration` requires an exported `DB_DSN` for an explicit disposable PostgreSQL database. It applies durable migrations in `--mode=migrate`, then runs tagged tests.
- `just vscode-lint` / `just vscode-test` validate the VS Code extension.
- `just docs` builds the full site with strict navigation/link/anchor checks. Install `requirements-docs.txt` first. The required renderer regression gate in [CONTRIBUTING.md](CONTRIBUTING.md) uses `EFFECTUS_REQUIRE_DOCS=1`; a skipped renderer test is not documentation validation.

## Coding Style & Naming Conventions

- Go: format with `go fmt` (tabs); prefer MixedCaps for exported identifiers and keep package/file naming consistent with existing modules.
- Protobuf: format with `buf format`; messages/enums use CamelCase, fields use snake_case.
- For Go protobuf generation, run `.tools/bin/buf generate --template buf.gen.go.yaml` with `.tools/bin` on `PATH`.
- SQLC reads `runtime/queries` and `runtime/migrations` through `runtime/sqlc.yaml`. Run `(cd runtime && ../.tools/bin/sqlc generate)` after changing those inputs.
- The daemon's durable store migrations use `schema.MigrateSagaV2`, not the legacy SQLC schema. No SQL formatter or separate SQL Just recipes are configured.

## Testing Guidelines

- Go tests live alongside code as `*_test.go` plus suites in `tests/`.
- Integration tests are tagged `integration` and can change database contents. Use a task-owned test database, not a shared or production database.
- `just setup-db` starts the repository test fixture without deleting existing data. Explicitly export its DSN before `just test-integration`.
- No explicit coverage threshold is enforced. After collecting `coverage.out`, use `go tool cover -html=coverage.out -o coverage.html`.

## Commit & Pull Request Guidelines

- Commit history mixes short imperative subjects and conventional prefixes like `fix:`, `chore:`, and `doc:` (with occasional plain phrases). Keep subjects concise; if you adopt a `type:` prefix, keep it consistent within the PR.
- PRs should include a clear summary, tests run (for example: `just test`, `just lint`), and note any required regeneration (protobufs or SQL). Include screenshots only when changing the VS Code extension UI.

## Configuration & Local Services

- The repository fixture uses the default connection described in `justfile`. `test-integration` still requires an explicit `DB_DSN` environment variable.
- See [CONTRIBUTING.md](CONTRIBUTING.md) for working generation and validation commands. Do not invent missing Just recipes or increase surface budgets to accommodate stale documentation.
