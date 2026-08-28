import * as fs from 'fs';
import * as path from 'path';
import * as vscode from 'vscode';
import {
    LanguageClient,
    LanguageClientOptions,
    ServerOptions,
    TransportKind
} from 'vscode-languageclient/node';

export class EffectusLanguageClient {
    private client: LanguageClient | undefined;

    async start(): Promise<void> {
        try {
            const serverOptions: ServerOptions = this.getServerOptions();
            const clientOptions: LanguageClientOptions = this.getClientOptions();
            this.client = new LanguageClient(
                'effectusLanguageServer',
                'Effectus Language Server',
                serverOptions,
                clientOptions
            );
            await this.client.start();
            console.log('Effectus Language Server started successfully');
        } catch (error) {
            this.client = undefined;
            const detail = error instanceof Error ? error.message : String(error);
            console.error('Failed to start Effectus Language Server:', error);
            vscode.window.showErrorMessage(`Effectus Language Server is unavailable: ${detail}`);
        }
    }

    async stop(): Promise<void> {
        if (this.client) {
            await this.client.stop();
            this.client = undefined;
        }
    }

    private getServerOptions(): ServerOptions {
        const effectuscPath = this.findEffectuscBinary();
        if (!effectuscPath) {
            throw new Error('install effectusc on PATH/GOBIN or set effectus.lsp.serverPath');
        }
        return {
            command: effectuscPath,
            args: ['lsp'],
            transport: TransportKind.stdio
        };
    }

    private getClientOptions(): LanguageClientOptions {
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
                schemaPath: vscode.workspace.getConfiguration('effectus').get('schemaPath'),
                verbSchemaPath: vscode.workspace.getConfiguration('effectus').get('verbSchemaPath'),
                examplesPath: vscode.workspace.getConfiguration('effectus').get('factExamplesPath'),
                unsafeMode: vscode.workspace.getConfiguration('effectus').get('lint.unsafe'),
                verbMode: vscode.workspace.getConfiguration('effectus').get('lint.verbs'),
                validation: {
                    realtime: vscode.workspace.getConfiguration('effectus').get('validation.realtime')
                }
            },
            middleware: {
                provideCompletionItem: (document, position, context, token, next) => next(document, position, context, token),
                provideHover: (document, position, token, next) => next(document, position, token),
                handleDiagnostics: (uri, diagnostics, next) => {
                    const effectusDiagnostics = diagnostics.filter(d => d.source === 'effectus' || d.source === 'effectusc');
                    next(uri, effectusDiagnostics);
                }
            }
        };
    }

    private findEffectuscBinary(): string | undefined {
        const customPath = vscode.workspace.getConfiguration('effectus').get<string>('lsp.serverPath')?.trim();
        if (customPath) {
            if (this.isExecutable(customPath)) {
                return customPath;
            }
            throw new Error(`configured effectus.lsp.serverPath is not executable: ${customPath}`);
        }

        const executable = process.platform === 'win32' ? 'effectusc.exe' : 'effectusc';
        const candidates = (process.env.PATH || '').split(path.delimiter).filter(Boolean).map(dir => path.join(dir, executable));
        if (process.env.GOBIN) {
            candidates.push(path.join(process.env.GOBIN, executable));
        }
        if (process.env.GOPATH) {
            candidates.push(path.join(process.env.GOPATH, 'bin', executable));
        }
        if (process.env.HOME) {
            candidates.push(path.join(process.env.HOME, 'go', 'bin', executable));
        }
        return candidates.find(candidate => this.isExecutable(candidate));
    }

    private isExecutable(candidate: string): boolean {
        try {
            fs.accessSync(candidate, process.platform === 'win32' ? fs.constants.F_OK : fs.constants.X_OK);
            return fs.statSync(candidate).isFile();
        } catch {
            return false;
        }
    }

    public isRunning(): boolean {
        return this.client !== undefined;
    }

    public async sendRequest<T>(method: string, params?: any): Promise<T> {
        if (!this.client) {
            throw new Error('Language client is not running');
        }
        return this.client.sendRequest(method, params);
    }

    public sendNotification(method: string, params?: any): void {
        if (this.client) {
            this.client.sendNotification(method, params);
        }
    }
}
