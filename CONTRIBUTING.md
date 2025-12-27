# Contributing to Effectus

Thanks for helping improve Effectus! This repo is Go-first with protobuf and optional SQL tooling.

## Prerequisites
- Go (use the version in `go.mod`).
- `just` for common workflows (`brew install just` on macOS).
- Optional: `buf`, `sqlc`, and `goose` (see `just install` and `just install-sql-tools`).

## Typical Workflow
1. Create a branch from `main`.
2. Make focused changes and keep docs/examples in sync.
3. Run the relevant checks before opening a PR.

## Commands
- `just build` — generate protobufs and build `bin/effectusc` / `bin/effectusd`.
- `just test` — run unit tests.
- `just test-coverage` — generate `coverage.out` and `coverage.html`.
- `just lint` — run `golangci-lint` and `buf lint`.
- `just fmt` — format Go + protobuf sources.

## Protobuf / SQL Updates
- Protobuf changes: run `just buf-format` and `just buf-generate`.
- SQL changes (runtime): run `just sql-generate` and `just sql-validate`.

## VS Code Extension
- `cd tools/vscode-extension && npm install`
- `just vscode-lint` / `just vscode-test`

## Pull Requests
- Include a concise summary and the tests you ran (e.g., `just test`, `just lint`).
- Note regeneration steps if you touched protobufs or SQL.
- Keep commits small and descriptive (e.g., `fix:`, `chore:`, `docs:` or short imperative subjects).
