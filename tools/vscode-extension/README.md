# Effectus Language Support for VS Code

This extension supports `.eff` and `.effx` rule files.

## Features

- Syntax highlighting
- Fact and verb completion
- Hover information
- Rule validation with effectusd or `effectusc`
- Rule formatting with `effectusc`
- Schema lineage in a webview
- Runtime hotload through the effectusd HTTP API

## Requirements

- VS Code 1.74 or later
- `effectusc` on `PATH`, or an explicit `effectus.lsp.serverPath`
- A workspace with `.eff`, `.effx`, or `.effectus/config.yaml`

The runtime hotload commands also require an effectusd API URL.

## Install a VSIX

```bash
code --install-extension effectus-language-support-*.vsix
```

Build a local VSIX from the repository root:

```bash
just vscode-dev-setup
just vscode-package
```

## Workspace layout

The default paths are:

```text
.effectus/
├── config.yaml
├── schemas/
└── verbs/
examples/
```

Run `Effectus: Initialize Effectus Workspace` to create these directories and the configuration file.

## Settings

```json
{
  "effectus.schemaPath": ".effectus/schemas",
  "effectus.verbSchemaPath": ".effectus/verbs",
  "effectus.factExamplesPath": "./examples",
  "effectus.lsp.enabled": true,
  "effectus.lsp.serverPath": "",
  "effectus.autoComplete.schemas": true,
  "effectus.validation.realtime": true,
  "effectus.lint.unsafe": "warn",
  "effectus.lint.verbs": "error",
  "effectus.runtime.apiUrl": "http://localhost:8080",
  "effectus.runtime.apiToken": ""
}
```

Leave `effectus.lsp.serverPath` empty to search the workspace, `PATH`, `GOPATH`, and `$HOME/go/bin`.

Store runtime tokens in local workspace settings or another private store. Do not commit a token.

## Commands

| Command | Purpose |
| --- | --- |
| `Effectus: Initialize Effectus Workspace` | Create the default workspace layout |
| `Effectus: Validate Current Rule` | Validate the open rule with effectusd, current diagnostics, or `effectusc` |
| `Effectus: Show Schema Lineage` | Open the schema lineage webview |
| `Effectus: Generate Schema Documentation` | Write schema documentation |
| `Effectus: Enable Runtime Hotload` | Connect hotload to the configured effectusd API |
| `Effectus: Disable Runtime Hotload` | Stop runtime hotload |
| `Effectus: Format Rule` | Format the open rule with `effectusc` |
| `Effectus: Refresh Effectus Schemas` | Reload schemas from the workspace |

The formatter sends a temporary copy to `effectusc format --stdout --write=false`. It applies one edit only after the command succeeds.

The validation command does not report success if no validator is available.

## Runtime hotload

Start effectusd with the rule hotload API:

```bash
EFFECTUS_API_TOKEN=devtoken \
EFFECTUS_SAGA_POSTGRES_DSN="postgres://effectus:password@localhost/effectus?sslmode=disable" \
  effectusd --bundle bundle.json --rules-hotload
```

Set `effectus.runtime.apiUrl` and `effectus.runtime.apiToken` in local VS Code settings.

The extension calls `/api/status`, `/api/rules/validate`, and `/api/rules/hotload`. It does not start a local server process.

Hotload validates and activates a candidate generation. It does not change an active generation in place.

Do not use runtime hotload for an OCI deployment. Production OCI deployments use signed, digest-pinned bundles.

## Develop the extension

From the repository root, run:

```bash
just vscode-install
just vscode-compile
just vscode-test
just vscode-package
```

Use watch mode while you edit TypeScript:

```bash
just vscode-watch
```

`npm test` runs static checks, unit tests, source activation tests, and packaged VSIX activation tests.

Linux test hosts require a display or `xvfb-run`.

## Troubleshoot

### The language server does not start

Make sure `effectusc` is executable and available on `PATH`. You can also set `effectus.lsp.serverPath` to an absolute path.

### Completion does not show schema fields

Check `effectus.schemaPath` and `effectus.verbSchemaPath`. Then reload the VS Code window.

### Runtime hotload fails

Check the API URL, token, effectusd logs, and `/readyz`. Make sure effectusd uses `--rules-hotload`.

## License

This extension uses the repository MIT license.
