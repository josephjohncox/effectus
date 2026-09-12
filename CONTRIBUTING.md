# Contributing to Effectus

## Prerequisites

Use the Go version and toolchain specified in `go.mod`.
Install `just` for the repository workflows.
`just install` downloads Go dependencies and installs the pinned Buf, Go protobuf, gRPC, and SQLC generators into `.tools/bin`.
It does not install `golangci-lint`, MkDocs, Docker, Node.js, or npm.

Run commands from the repository root in a POSIX shell:

```bash
just install
export PATH="$PWD/.tools/bin:$PATH"
just --list
```

For Go-only edits, checked-in generated bindings let you run `go test ./...` without regeneration tools.
Documentation builds need the dependencies in `requirements-docs.txt`.
Install them in an isolated Python environment, then run `just docs`.
For the uncached renderer and negative-link tests, run:

```bash
EFFECTUS_REQUIRE_DOCS=1 go test -count=1 -timeout=90s ./internal/guardrails -run 'DocumentedStrictSite|DocumentationCopy'
```

This required gate fails if MkDocs is absent. Ordinary Go tests can skip the renderer checks without it.
The tests build temporary copies and reject missing pages, anchors, unlisted pages, and unsupported relative/absolute links.
They do not fetch remote URLs.

## Typical workflow

1. Create a branch from `main`.
2. Make a focused change and update its tests and documentation.
3. Run the relevant checks.
4. Describe the commands, results, and unavailable gates in the pull request.

| Command | Work |
| --- | --- |
| `just build` | Generate Go protobuf and SQLC bindings, then build `bin/effectusc` and `bin/effectusd`. |
| `just test` | Run root-module Go tests. |
| `just test-modules` | Test every reviewed Go module. |
| `just test-examples` | Check shared scenarios, embedded onboarding, tutorial dialects, and the live Go gRPC client. |
| `just guardrails` | Check inventories, public boundaries, dependencies, documented contracts, and surface budgets. |
| `just fmt` | Format Go and protobuf sources. |
| `just lint` | Run separately installed `golangci-lint` and Buf lint. |
| `just docs` | Build documentation in strict mode. |

The Go gRPC tests start their own matching authenticated service and test TLS separately.
Python validation requires the explicit interpreter setting and pinned requirements in the [client guide](docs/CLIENT_EXAMPLES.md).
An ordinary Go test run without that setting skips Python and does not establish Python coverage.

Collect coverage with the Go tools:

```bash
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
go tool cover -html=coverage.out -o coverage.html
```

No separate coverage recipe or coverage threshold is configured.
Coverage percentages are not evidence that every failure or concurrency path is tested.

## Protobuf changes

Use the repository's Go-only template for the generated Go bindings:

```bash
export PATH="$PWD/.tools/bin:$PATH"
buf format -w
buf lint
buf generate --template buf.gen.go.yaml
buf breaking --against .git#ref=HEAD
```

The broader `buf.gen.yaml` also configures other languages. It is not the default Go build template.
Review generated differences. Preserve wire numbers and reserved capabilities.
The `HEAD` comparison checks local changes, not compatibility against every released version.
For release review, also compare against the relevant release baseline.

## SQL changes and durable migrations

`runtime/sqlc.yaml` reads `runtime/queries` and `runtime/migrations` and writes `runtime/internal/db`.
Regenerate those bindings after changing their inputs:

```bash
(cd runtime && ../.tools/bin/sqlc generate)
```

`just build` runs the same SQLC generation step.
Do not edit generated bindings instead of their source queries.
No SQL formatter or separate SQL Just recipes are configured.

The durable daemon store uses `schema.MigrateSagaV2` and `schema.ValidateSagaV2`.
Those migrations are separate from the legacy schema used for SQLC bindings.
Changing inline durable-store SQL does not imply that SQLC generates it.
Test durable schema changes against an explicit disposable PostgreSQL database.

## PostgreSQL integration tests

Tagged tests can change database contents. Do not point them at a production or shared database.
Use a task-owned database, or inspect the repository fixture before starting it:

```bash
just setup-db
export DB_DSN='postgres://effectus:effectus@localhost:55433/effectus_saga?sslmode=disable'
just test-integration
```

The DSN above belongs to `tests/fixtures/postgres/docker-compose.yml`.
For another disposable database, export its DSN instead and omit `setup-db`.

`test-integration` rejects a missing `DB_DSN` before running Go commands.
It applies durable migrations with `--mode=migrate --database-migrations=apply`, then runs tagged tests serially by package.
Migration failure prevents the tests from starting.
The recipe passes the DSN through the environment and does not print it in command lines.
It neither selects an arbitrary database nor deletes existing Compose resources.

## VS Code extension

The supported recipes install locked npm dependencies before linting or testing:

```bash
just vscode-lint
just vscode-test
```

Use a compatible Node.js/npm installation for `tools/vscode-extension/package.json`.

## Pull requests

Include a concise summary, actual tests, and remaining risks.
Note protobuf and SQL regeneration when applicable.
Keep commits focused and subjects descriptive, with optional `fix:`, `chore:`, or `docs:` prefixes.
Do not refresh all inventories or increase a budget merely to make a guardrail pass.
