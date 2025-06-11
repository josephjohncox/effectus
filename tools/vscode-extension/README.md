# Effectus Language Support for VS Code

This extension provides comprehensive language support for Effectus rule files (`.eff` and `.effx`) with schema-aware features, hot reload capabilities, and visual schema lineage.

## Features

### 🎯 **Language Support**
- **Syntax Highlighting** - Full syntax highlighting for Effectus rule language
- **IntelliSense** - Schema-aware autocompletion for facts, verbs, and fields
- **Hover Information** - Rich documentation for schemas and fields
- **Go to Definition** - Navigate to schema definitions
- **Error Squiggles** - Real-time validation and error reporting

### 📊 **Schema Awareness**
- **Fact & Verb Schemas** - Automatic discovery of protobuf schemas
- **Field Completion** - Smart completions based on schema definitions
- **Type Information** - Detailed type information in hover tooltips
- **Schema Documentation** - Auto-generated documentation from schemas

### 🔄 **Hot Reload Development**
- **Live Updates** - Automatic rule reloading during development
- **Development Server** - Built-in server for rapid iteration
- **WebSocket Connection** - Real-time communication with Effectus runtime
- **Status Indicators** - Visual feedback for compilation and testing

### 🕸️ **Schema Lineage**
- **Relationship Visualization** - Interactive diagrams showing fact/verb/rule relationships
- **Data Flow Analysis** - Understanding how data flows through your rules
- **Clickable Navigation** - Jump to related schemas and rules
- **Export Capabilities** - Generate documentation with lineage diagrams

## Getting Started

### Prerequisites
- VS Code 1.74.0 or later
- Effectus CLI (`effectusc`) installed and available in PATH
- Node.js (for extension development)

### Installation

#### From VSIX Package
1. Download the latest `.vsix` file from releases
2. Install using VS Code: `code --install-extension effectus-language-support-*.vsix`
3. Or use Command Palette: `Extensions: Install from VSIX...`

#### From Source
```bash
# Clone the Effectus repository
git clone https://github.com/effectus/effectus.git
cd effectus

# Build and install the extension
just vscode-dev-setup
just vscode-package
just vscode-install-local
```

### Workspace Setup

1. **Initialize Effectus Workspace** - Use Command Palette: `Effectus: Initialize Effectus Workspace`
2. **Configure Schemas** - Place schema files in `.effectus/schemas/`
3. **Create Rules** - Start writing `.eff` or `.effx` files

The extension will automatically detect Effectus workspaces and provide enhanced features.

## Configuration

### Extension Settings

```json
{
  "effectus.schemaPath": ".effectus/schemas",
  "effectus.factExamplesPath": "./examples",
  "effectus.lsp.enabled": true,
  "effectus.autoComplete.schemas": true,
  "effectus.validation.realtime": true,
  "effectus.hotReload.enabled": false,
  "effectus.hotReload.port": 9090,
  "effectus.lineage.showInHover": true,
  "effectus.performance.showMetrics": true
}
```

### Workspace Configuration

Create `.effectus/config.yaml`:
```yaml
schema_path: .effectus/schemas
examples_path: ./examples
hot_reload:
  enabled: false
  port: 9090
validation:
  real_time: true
performance:
  show_metrics: true
```

## Commands

| Command | Description |
|---------|-------------|
| `Effectus: Initialize Effectus Workspace` | Set up directory structure and config |
| `Effectus: Validate Current Rule` | Validate the currently open rule file |
| `Effectus: Test Rule with Synthetic Data` | Test rule with generated data |
| `Effectus: Show Schema Lineage` | Open interactive lineage diagram |
| `Effectus: Generate Schema Documentation` | Create markdown documentation |
| `Effectus: Start Development Server` | Enable hot reload functionality |
| `Effectus: Stop Development Server` | Disable hot reload |
| `Effectus: Format Rule` | Format current rule file |

## Usage Examples

### Basic Rule with IntelliSense

```effectus
rule "user_purchase_notification" {
  when {
    user_event.type == "purchase" and
    user_event.amount > 100
  }
  then {
    emit send_email {
      to: user_event.user_id
      template: "purchase_confirmation"
      data: {
        amount: user_event.amount
        items: user_event.items
      }
    }
  }
}
```

As you type, the extension provides:
- Autocompletion for `user_event` fields based on schemas
- Type checking for comparisons and assignments
- Hover documentation for verbs like `send_email`

### Hot Reload Development

1. Start development server: `Ctrl+Shift+P` → `Effectus: Start Development Server`
2. Edit rule files - changes are automatically compiled and tested
3. View real-time feedback in status bar
4. WebSocket connection provides immediate error reporting

### Schema Lineage Visualization

1. Open lineage view: `Ctrl+Shift+P` → `Effectus: Show Schema Lineage`
2. Interactive diagram shows relationships between:
   - **Facts** (green) - Input data structures
   - **Verbs** (blue) - Actions/outputs
   - **Rules** (orange) - Business logic
3. Click nodes to navigate to definitions
4. Zoom and pan for large schema sets

## Development

### Building from Source

```bash
# Install dependencies
just vscode-install

# Compile TypeScript
just vscode-compile

# Watch mode for development
just vscode-watch

# Package extension
just vscode-package
```

### Extension Structure

```
tools/vscode-extension/
├── src/
│   ├── extension.ts          # Main extension entry point
│   ├── schemaProvider.ts     # Schema-aware IntelliSense
│   ├── languageClient.ts     # LSP integration
│   ├── hotReload.ts         # Development server integration
│   └── lineageProvider.ts   # Schema visualization
├── syntaxes/
│   └── effectus.tmLanguage.json  # Syntax highlighting
├── snippets/
│   └── effectus.json        # Code snippets
└── package.json             # Extension manifest
```

## Troubleshooting

### Common Issues

**Extension not activating:**
- Check that `.eff` or `.effx` files exist in workspace
- Verify VS Code version >= 1.74.0

**IntelliSense not working:**
- Ensure schemas exist in `.effectus/schemas/`
- Check schema files are valid YAML/JSON
- Restart extension: `Developer: Reload Window`

**Hot reload not connecting:**
- Verify `effectusc` is in PATH
- Check development server port (default 9090) is available
- Review WebSocket connection in Developer Console

**Schema lineage empty:**
- Confirm schema files follow expected format
- Check that rules reference schemas correctly
- Verify workspace is properly initialized

### Debug Mode

1. Open extension host: `Help` → `Toggle Developer Tools`
2. Check console for extension logs
3. Use `Developer: Reload Window` to restart extension

## Contributing

1. Fork the repository
2. Create feature branch: `git checkout -b feature/awesome-feature`
3. Make changes in `tools/vscode-extension/`
4. Test: `just vscode-compile && just vscode-test`
5. Submit pull request

## License

This extension is part of the Effectus project and is licensed under the MIT License.

## Links

- [Effectus Documentation](https://effectus.dev/docs)
- [GitHub Repository](https://github.com/effectus/effectus)
- [Issue Tracker](https://github.com/effectus/effectus/issues)
- [VS Code Extension API](https://code.visualstudio.com/api) 