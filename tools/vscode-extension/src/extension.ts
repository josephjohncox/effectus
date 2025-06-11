import * as vscode from 'vscode';
import { EffectusLanguageClient } from './languageClient';
import { SchemaProvider } from './schemaProvider';
import { HotReloadManager } from './hotReload';
import { LineageProvider } from './lineageProvider';

let languageClient: EffectusLanguageClient;
let schemaProvider: SchemaProvider;
let hotReloadManager: HotReloadManager;
let lineageProvider: LineageProvider;

export function activate(context: vscode.ExtensionContext) {
    console.log('Effectus extension is now active');

    // Initialize providers
    schemaProvider = new SchemaProvider(context);
    lineageProvider = new LineageProvider(context);
    hotReloadManager = new HotReloadManager(context);

    // Initialize language client if LSP is enabled
    const config = vscode.workspace.getConfiguration('effectus');
    if (config.get('lsp.enabled')) {
        languageClient = new EffectusLanguageClient(context);
        languageClient.start();
    }

    // Register commands
    registerCommands(context);

    // Register providers
    registerProviders(context);

    // Setup file watchers
    setupFileWatchers(context);

    // Initialize workspace if .effectus directory exists
    initializeWorkspace(context);
}

export function deactivate(): Thenable<void> | undefined {
    if (languageClient) {
        return languageClient.stop();
    }
    
    if (hotReloadManager) {
        hotReloadManager.dispose();
    }
}

function registerCommands(context: vscode.ExtensionContext) {
    // Schema documentation command
    const generateDocsCommand = vscode.commands.registerCommand('effectus.generateDocs', async () => {
        try {
            await schemaProvider.generateDocumentation();
            vscode.window.showInformationMessage('Schema documentation generated successfully');
        } catch (error) {
            vscode.window.showErrorMessage(`Failed to generate documentation: ${error}`);
        }
    });

    // Rule validation command
    const validateRuleCommand = vscode.commands.registerCommand('effectus.validateRule', async () => {
        const editor = vscode.window.activeTextEditor;
        if (!editor || !isEffectusFile(editor.document)) {
            vscode.window.showWarningMessage('Please open an Effectus rule file (.eff or .effx)');
            return;
        }

        try {
            const diagnostics = await validateRule(editor.document);
            if (diagnostics.length === 0) {
                vscode.window.showInformationMessage('Rule validation passed');
            } else {
                vscode.window.showWarningMessage(`Rule has ${diagnostics.length} validation issues`);
            }
        } catch (error) {
            vscode.window.showErrorMessage(`Validation failed: ${error}`);
        }
    });

    // Test rule command
    const testRuleCommand = vscode.commands.registerCommand('effectus.testRule', async () => {
        const editor = vscode.window.activeTextEditor;
        if (!editor || !isEffectusFile(editor.document)) {
            vscode.window.showWarningMessage('Please open an Effectus rule file (.eff or .effx)');
            return;
        }

        try {
            await testRuleWithSyntheticData(editor.document);
        } catch (error) {
            vscode.window.showErrorMessage(`Test failed: ${error}`);
        }
    });

    // Show lineage command
    const showLineageCommand = vscode.commands.registerCommand('effectus.showLineage', async () => {
        try {
            await lineageProvider.showLineageDiagram();
        } catch (error) {
            vscode.window.showErrorMessage(`Failed to show lineage: ${error}`);
        }
    });

    // Hot reload commands
    const startServerCommand = vscode.commands.registerCommand('effectus.dev.startServer', async () => {
        try {
            await hotReloadManager.startServer();
            vscode.window.showInformationMessage('Effectus development server started');
        } catch (error) {
            vscode.window.showErrorMessage(`Failed to start server: ${error}`);
        }
    });

    const stopServerCommand = vscode.commands.registerCommand('effectus.dev.stopServer', async () => {
        try {
            await hotReloadManager.stopServer();
            vscode.window.showInformationMessage('Effectus development server stopped');
        } catch (error) {
            vscode.window.showErrorMessage(`Failed to stop server: ${error}`);
        }
    });

    // Format rule command
    const formatRuleCommand = vscode.commands.registerCommand('effectus.rule.format', async () => {
        const editor = vscode.window.activeTextEditor;
        if (!editor || !isEffectusFile(editor.document)) {
            vscode.window.showWarningMessage('Please open an Effectus rule file (.eff or .effx)');
            return;
        }

        try {
            await formatRule(editor);
        } catch (error) {
            vscode.window.showErrorMessage(`Formatting failed: ${error}`);
        }
    });

    // Initialize workspace command
    const initWorkspaceCommand = vscode.commands.registerCommand('effectus.workspace.init', async () => {
        try {
            await initializeEffectusWorkspace();
            vscode.window.showInformationMessage('Effectus workspace initialized');
        } catch (error) {
            vscode.window.showErrorMessage(`Workspace initialization failed: ${error}`);
        }
    });

    // Register all commands
    context.subscriptions.push(
        generateDocsCommand,
        validateRuleCommand,
        testRuleCommand,
        showLineageCommand,
        startServerCommand,
        stopServerCommand,
        formatRuleCommand,
        initWorkspaceCommand
    );
}

function registerProviders(context: vscode.ExtensionContext) {
    // Register completion provider
    const completionProvider = vscode.languages.registerCompletionItemProvider(
        'effectus',
        {
            provideCompletionItems(document: vscode.TextDocument, position: vscode.Position) {
                return schemaProvider.provideCompletions(document, position);
            }
        },
        '.' // Trigger on dot for field access
    );

    // Register hover provider
    const hoverProvider = vscode.languages.registerHoverProvider(
        'effectus',
        {
            provideHover(document: vscode.TextDocument, position: vscode.Position) {
                return schemaProvider.provideHover(document, position);
            }
        }
    );

    // Register definition provider
    const definitionProvider = vscode.languages.registerDefinitionProvider(
        'effectus',
        {
            provideDefinition(document: vscode.TextDocument, position: vscode.Position) {
                return schemaProvider.provideDefinition(document, position);
            }
        }
    );

    context.subscriptions.push(completionProvider, hoverProvider, definitionProvider);
}

function setupFileWatchers(context: vscode.ExtensionContext) {
    // Watch for schema changes
    const schemaWatcher = vscode.workspace.createFileSystemWatcher('**/.effectus/schemas/**/*');
    schemaWatcher.onDidChange(() => {
        schemaProvider.refreshSchemas();
    });
    schemaWatcher.onDidCreate(() => {
        schemaProvider.refreshSchemas();
    });
    schemaWatcher.onDidDelete(() => {
        schemaProvider.refreshSchemas();
    });

    // Watch for rule changes
    const ruleWatcher = vscode.workspace.createFileSystemWatcher('**/*.{eff,effx}');
    ruleWatcher.onDidChange((uri) => {
        if (hotReloadManager.isRunning()) {
            hotReloadManager.notifyRuleChange(uri);
        }
    });

    context.subscriptions.push(schemaWatcher, ruleWatcher);
}

function initializeWorkspace(context: vscode.ExtensionContext) {
    const workspaceFolders = vscode.workspace.workspaceFolders;
    if (!workspaceFolders) {
        return;
    }

    // Check if .effectus directory exists
    for (const folder of workspaceFolders) {
        const effectusConfig = vscode.Uri.joinPath(folder.uri, '.effectus', 'config.yaml');
        vscode.workspace.fs.stat(effectusConfig).then(() => {
            // Set context that workspace is initialized
            vscode.commands.executeCommand('setContext', 'effectus.workspaceInitialized', true);
            schemaProvider.loadSchemas();
        }, () => {
            // .effectus directory doesn't exist
            vscode.commands.executeCommand('setContext', 'effectus.workspaceInitialized', false);
        });
    }
}

// Helper functions
function isEffectusFile(document: vscode.TextDocument): boolean {
    return document.languageId === 'effectus' || 
           document.fileName.endsWith('.eff') || 
           document.fileName.endsWith('.effx');
}

async function validateRule(document: vscode.TextDocument): Promise<vscode.Diagnostic[]> {
    // TODO: Implement rule validation logic
    // This would call the Effectus compiler or LSP
    return [];
}

async function testRuleWithSyntheticData(document: vscode.TextDocument): Promise<void> {
    // TODO: Implement rule testing with synthetic data
    // This would generate test data based on schemas and execute the rule
    vscode.window.showInformationMessage('Rule testing functionality coming soon');
}

async function formatRule(editor: vscode.TextEditor): Promise<void> {
    // TODO: Implement rule formatting
    // This would format the rule according to Effectus style guidelines
    vscode.window.showInformationMessage('Rule formatting functionality coming soon');
}

async function initializeEffectusWorkspace(): Promise<void> {
    const workspaceFolders = vscode.workspace.workspaceFolders;
    if (!workspaceFolders) {
        throw new Error('No workspace folder open');
    }

    const workspaceRoot = workspaceFolders[0].uri;
    
    // Create .effectus directory structure
    const effectusDir = vscode.Uri.joinPath(workspaceRoot, '.effectus');
    const schemasDir = vscode.Uri.joinPath(effectusDir, 'schemas');
    const examplesDir = vscode.Uri.joinPath(workspaceRoot, 'examples');

    await vscode.workspace.fs.createDirectory(effectusDir);
    await vscode.workspace.fs.createDirectory(schemasDir);
    await vscode.workspace.fs.createDirectory(examplesDir);

    // Create default config
    const configContent = `# Effectus Workspace Configuration
schema_path: .effectus/schemas
examples_path: ./examples
hot_reload:
  enabled: false
  port: 9090
validation:
  real_time: true
performance:
  show_metrics: true
`;

    const configUri = vscode.Uri.joinPath(effectusDir, 'config.yaml');
    await vscode.workspace.fs.writeFile(configUri, Buffer.from(configContent, 'utf8'));

    // Set context
    vscode.commands.executeCommand('setContext', 'effectus.workspaceInitialized', true);
} 