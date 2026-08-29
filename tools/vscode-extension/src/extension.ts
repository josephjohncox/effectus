import * as vscode from 'vscode';
import { EffectusLanguageClient } from './languageClient';
import { SchemaProvider } from './schemaProvider';
import { HotReloadManager } from './hotReload';
import { LineageProvider } from './lineageProvider';
import { formatDocument, ValidationResult, validateWithCompilerOrDiagnostics } from './compilerOperations';

let languageClient: EffectusLanguageClient | undefined;
let schemaProvider: SchemaProvider;
let hotReloadManager: HotReloadManager;
let lineageProvider: LineageProvider;

export function activate(context: vscode.ExtensionContext): void {
    console.log('Effectus extension is now active');

    schemaProvider = new SchemaProvider(context);
    lineageProvider = new LineageProvider(context);
    hotReloadManager = new HotReloadManager();

    registerCommands(context);
    registerProviders(context);
    setupFileWatchers(context);
    initializeWorkspace();

    if (vscode.workspace.getConfiguration('effectus').get<boolean>('lsp.enabled', true)) {
        languageClient = new EffectusLanguageClient();
        void languageClient.start().catch(error => {
            console.error('Unexpected Effectus Language Server startup failure:', error);
        });
    }
}

export async function deactivate(): Promise<void> {
    await languageClient?.stop();
}

function registerCommands(context: vscode.ExtensionContext): void {
    context.subscriptions.push(
        vscode.commands.registerCommand('effectus.generateDocs', async () => {
            try {
                await schemaProvider.generateDocumentation();
                void vscode.window.showInformationMessage('Schema documentation generated successfully');
            } catch (error) {
                void vscode.window.showErrorMessage(`Failed to generate documentation: ${error}`);
            }
        }),
        vscode.commands.registerCommand('effectus.validateRule', async (): Promise<ValidationResult | undefined> => {
            const editor = vscode.window.activeTextEditor;
            if (!editor || !isEffectusFile(editor.document)) {
                void vscode.window.showWarningMessage('Please open an Effectus rule file (.eff or .effx)');
                return undefined;
            }

            try {
                const result = await validateRule(editor.document);
                if (result.status === 'unavailable') {
                    void vscode.window.showWarningMessage(
                        `Rule validation is unavailable. ${result.reason || ''} Configure effectus.runtime.apiUrl or effectus.lsp.serverPath.`
                    );
                } else if (result.diagnostics.length === 0) {
                    void vscode.window.showInformationMessage('Rule validation passed');
                } else {
                    void vscode.window.showWarningMessage(`Rule has ${result.diagnostics.length} validation issues`);
                }
                return result;
            } catch (error) {
                void vscode.window.showErrorMessage(`Validation failed: ${error}`);
                return undefined;
            }
        }),
        vscode.commands.registerCommand('effectus.showLineage', async () => {
            try {
                await lineageProvider.showLineageDiagram();
            } catch (error) {
                void vscode.window.showErrorMessage(`Failed to show lineage: ${error}`);
            }
        }),
        vscode.commands.registerCommand('effectus.rule.format', async () => {
            const editor = vscode.window.activeTextEditor;
            if (!editor || !isEffectusFile(editor.document)) {
                void vscode.window.showWarningMessage('Please open an Effectus rule file (.eff or .effx)');
                return;
            }
            try {
                await formatDocument(editor);
            } catch (error) {
                void vscode.window.showErrorMessage(`Formatting failed: ${error}`);
            }
        }),
        vscode.commands.registerCommand('effectus.workspace.init', async () => {
            try {
                await initializeEffectusWorkspace();
                void vscode.window.showInformationMessage('Effectus workspace initialized');
            } catch (error) {
                void vscode.window.showErrorMessage(`Workspace initialization failed: ${error}`);
            }
        }),
        vscode.commands.registerCommand('effectus.schema.refresh', async () => {
            await schemaProvider.refreshSchemas();
        })
    );
}

function registerProviders(context: vscode.ExtensionContext): void {
    context.subscriptions.push(
        vscode.languages.registerCompletionItemProvider('effectus', {
            provideCompletionItems(document, position) {
                return schemaProvider.provideCompletions(document, position);
            }
        }, '.'),
        vscode.languages.registerHoverProvider('effectus', {
            provideHover(document, position) {
                return schemaProvider.provideHover(document, position);
            }
        }),
        vscode.languages.registerDefinitionProvider('effectus', {
            provideDefinition(document, position) {
                return schemaProvider.provideDefinition(document, position);
            }
        })
    );
}

function setupFileWatchers(context: vscode.ExtensionContext): void {
    const schemaWatcher = vscode.workspace.createFileSystemWatcher('**/.effectus/schemas/**/*');
    schemaWatcher.onDidChange(() => schemaProvider.refreshSchemas());
    schemaWatcher.onDidCreate(() => schemaProvider.refreshSchemas());
    schemaWatcher.onDidDelete(() => schemaProvider.refreshSchemas());

    context.subscriptions.push(schemaWatcher);
}

function initializeWorkspace(): void {
    for (const folder of vscode.workspace.workspaceFolders || []) {
        const config = vscode.Uri.joinPath(folder.uri, '.effectus', 'config.yaml');
        void vscode.workspace.fs.stat(config).then(() => {
            void vscode.commands.executeCommand('setContext', 'effectus.workspaceInitialized', true);
            void schemaProvider.loadSchemas();
        }, () => {
            void vscode.commands.executeCommand('setContext', 'effectus.workspaceInitialized', false);
        });
    }
}

function isEffectusFile(document: vscode.TextDocument): boolean {
    return document.languageId === 'effectus'
        || document.fileName.endsWith('.eff')
        || document.fileName.endsWith('.effx');
}

function runtimeDiagnostics(diagnostics: any[]): vscode.Diagnostic[] {
    return diagnostics.map(diagnostic => {
        const line = Math.max((diagnostic.line || 1) - 1, 0);
        const column = Math.max((diagnostic.column || 1) - 1, 0);
        const severity = (diagnostic.severity || '').toLowerCase() === 'error'
            ? vscode.DiagnosticSeverity.Error
            : vscode.DiagnosticSeverity.Warning;
        const result = new vscode.Diagnostic(
            new vscode.Range(line, column, line, column + 1),
            diagnostic.message || 'Rule issue',
            severity
        );
        result.source = 'effectusd';
        return result;
    });
}

async function validateRule(document: vscode.TextDocument): Promise<ValidationResult> {
    const runtimeResult = await hotReloadManager.validateRule(document);
    if (runtimeResult.available) {
        return {
            status: 'validated',
            diagnostics: runtimeDiagnostics(runtimeResult.diagnostics),
            source: 'runtime'
        };
    }
    return validateWithCompilerOrDiagnostics(document);
}

async function initializeEffectusWorkspace(): Promise<void> {
    const workspaceFolder = vscode.workspace.workspaceFolders?.[0];
    if (!workspaceFolder) {
        throw new Error('No workspace folder open');
    }

    const effectusDirectory = vscode.Uri.joinPath(workspaceFolder.uri, '.effectus');
    const schemasDirectory = vscode.Uri.joinPath(effectusDirectory, 'schemas');
    const verbsDirectory = vscode.Uri.joinPath(effectusDirectory, 'verbs');
    const examplesDirectory = vscode.Uri.joinPath(workspaceFolder.uri, 'examples');

    await vscode.workspace.fs.createDirectory(effectusDirectory);
    await vscode.workspace.fs.createDirectory(schemasDirectory);
    await vscode.workspace.fs.createDirectory(verbsDirectory);
    await vscode.workspace.fs.createDirectory(examplesDirectory);

    const configContent = `# Effectus Workspace Configuration
schema_path: .effectus/schemas
verb_schema_path: .effectus/verbs
examples_path: ./examples
validation:
  real_time: true
`;
    await vscode.workspace.fs.writeFile(
        vscode.Uri.joinPath(effectusDirectory, 'config.yaml'),
        Buffer.from(configContent, 'utf8')
    );
    await vscode.commands.executeCommand('setContext', 'effectus.workspaceInitialized', true);
}
