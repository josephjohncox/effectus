import * as vscode from 'vscode';
import {
    LanguageClient,
    LanguageClientOptions,
    ServerOptions
} from 'vscode-languageclient/node';
import { resolveConfiguredEffectusc } from './compilerOperations';

const installUrl = vscode.Uri.parse('https://github.com/effectus/effectus/releases/latest');

export function createServerOptions(executable: string): ServerOptions {
    return {
        command: executable,
        args: ['lsp']
    };
}

export class EffectusLanguageClient {
    private client: LanguageClient | undefined;
    private missingBinaryPromptShown = false;

    async start(): Promise<boolean> {
        const resolution = resolveConfiguredEffectusc();
        if (!resolution.path) {
            await this.showMissingBinaryPrompt(resolution.error);
            return false;
        }

        try {
            this.client = new LanguageClient(
                'effectusLanguageServer',
                'Effectus Language Server',
                createServerOptions(resolution.path),
                this.getClientOptions()
            );
            await this.client.start();
            console.log('Effectus Language Server started successfully');
            return true;
        } catch (error) {
            this.client = undefined;
            console.error('Failed to start Effectus Language Server:', error);
            await vscode.window.showErrorMessage(
                `Failed to start effectusc lsp: ${error instanceof Error ? error.message : String(error)}`
            );
            return false;
        }
    }

    async stop(): Promise<void> {
        if (this.client) {
            await this.client.stop();
            this.client = undefined;
        }
    }

    isRunning(): boolean {
        return this.client !== undefined;
    }

    async sendRequest<T>(method: string, params?: any): Promise<T> {
        if (!this.client) {
            throw new Error('Language client is not running');
        }
        return this.client.sendRequest(method, params);
    }

    sendNotification(method: string, params?: any): void {
        this.client?.sendNotification(method, params);
    }

    private async showMissingBinaryPrompt(detail?: string): Promise<void> {
        if (this.missingBinaryPromptShown) {
            return;
        }
        this.missingBinaryPromptShown = true;
        const choice = await vscode.window.showErrorMessage(
            `Effectus language support could not find effectusc on PATH or at effectus.lsp.serverPath. ${detail || ''}`.trim(),
            'Install Effectus',
            'Open Settings'
        );
        if (choice === 'Install Effectus') {
            await vscode.env.openExternal(installUrl);
        } else if (choice === 'Open Settings') {
            await vscode.commands.executeCommand('workbench.action.openSettings', 'effectus.lsp.serverPath');
        }
    }

    private getClientOptions(): LanguageClientOptions {
        const configuration = vscode.workspace.getConfiguration('effectus');
        return {
            documentSelector: [{ scheme: 'file', language: 'effectus' }],
            synchronize: {
                configurationSection: 'effectus',
                fileEvents: [
                    vscode.workspace.createFileSystemWatcher('**/.effectus/schemas/**/*'),
                    vscode.workspace.createFileSystemWatcher('**/*.{eff,effx}')
                ]
            },
            initializationOptions: {
                schemaPath: configuration.get('schemaPath'),
                verbSchemaPath: configuration.get('verbSchemaPath'),
                examplesPath: configuration.get('factExamplesPath'),
                unsafeMode: configuration.get('lint.unsafe'),
                verbMode: configuration.get('lint.verbs'),
                validation: { realtime: configuration.get('validation.realtime') }
            },
            middleware: {
                provideCompletionItem: (document, position, context, token, next) =>
                    next(document, position, context, token),
                provideHover: (document, position, token, next) =>
                    next(document, position, token),
                handleDiagnostics: (uri, diagnostics, next) => {
                    next(uri, diagnostics.filter(diagnostic =>
                        diagnostic.source === 'effectus' || diagnostic.source === 'effectusc'
                    ));
                }
            }
        };
    }
}
