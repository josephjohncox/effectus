# Effectus Language Support for VS Code

This extension supports `.eff` and `.effx` rule files.

## Features

- Syntax highlighting
- Fact and verb completion
- Hover information
- Rule validation
- Rule formatting
- Synthetic-data tests
- Schema lineage views
- Development hotload through the effectusd API

## Requirements

- VS Code 1.74 or later
- `effectusc` on `PATH`
- A workspace with `.eff`, `.effx`, or `.effectus/config.yaml`

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
```

Run `Effectus: Initialize Effectus Workspace` from the Command Palette to create this layout.

## Settings

```json
{
  "effectus.schemaPath": ".effectus/schemas",
  "effectus.verbSchemaPath": ".effectus/verbs",
  "effectus.factExamplesPath": "./examples",
  "effectus.lsp.enabled": true,
  "effectus.autoComplete.schemas": true,
  "effectus.validation.realtime": true,
  "effectus.lint.unsafe": "warn",
  "effectus.lint.verbs": "error",
  "effectus.hotReload.enabled": false,
  "effectus.runtime.apiUrl": "http://localhost:8080",
  "effectus.runtime.apiToken": ""
}
```

Store runtime tokens in VS Code secret or local workspace settings. Do not commit them.

## Commands

| Command | Purpose |
| --- | --- |
| `Effectus: Initialize Effectus Workspace` | Create the default workspace layout |
| `Effectus: Validate Current Rule` | Validate the open rule file |
| `Effectus: Test Rule with Synthetic Data` | Test the open rule with generated facts |
| `Effectus: Show Schema Lineage` | Open the lineage view |
| `Effectus: Generate Schema Documentation` | Write schema documentation |
| `Effectus: Start Development Server` | Start development hotload support |
| `Effectus: Stop Development Server` | Stop development hotload support |
| `Effectus: Format Rule` | Format the open rule file |

## Development hotload

Start effectusd with the rule hotload API enabled:

```bash
EFFECTUS_API_TOKEN=devtoken \
EFFECTUS_SAGA_POSTGRES_DSN="postgres://effectus:password@localhost/effectus?sslmode=disable" \
  effectusd --bundle bundle.json --rules-hotload
```

Set `effectus.runtime.apiUrl` and `effectus.runtime.apiToken` in local VS Code settings.

Hotload validates and activates a candidate generation. It does not mutate an active generation in place.

Do not use development hotload as an OCI deployment mechanism. Production OCI deployments use signed, digest-pinned bundles.

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

## Troubleshoot

### The extension does not start

Make sure the workspace contains an `.eff` file, an `.effx` file, or `.effectus/config.yaml`.

### Completion does not show schema fields

Check `effectus.schemaPath` and `effectus.verbSchemaPath`. Then reload the VS Code window.

### Runtime hotload fails

Check the API URL, token, effectusd logs, and `/readyz`. Make sure effectusd started with `--rules-hotload`.

## License

This extension uses the repository MIT license.
