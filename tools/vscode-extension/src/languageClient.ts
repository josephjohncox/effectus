import * as vscode from 'vscode';
import {
    LanguageClient,
    LanguageClientOptions,
    ServerOptions,
    TransportKind
} from 'vscode-languageclient/node';

export class EffectusLanguageClient {
    private client: LanguageClient | undefined;
    private context: vscode.ExtensionContext;

    constructor(context: vscode.ExtensionContext) {
        this.context = context;
    }

    async start(): Promise<void> {
        const serverOptions: ServerOptions = this.getServerOptions();
        const clientOptions: LanguageClientOptions = this.getClientOptions();

        this.client = new LanguageClient(
            'effectusLanguageServer',
            'Effectus Language Server',
            serverOptions,
            clientOptions
        );

        try {
            await this.client.start();
            console.log('Effectus Language Server started successfully');
        } catch (error) {
            console.error('Failed to start Effectus Language Server:', error);
            vscode.window.showErrorMessage(
                'Failed to start Effectus Language Server. Some features may not work.'
            );
        }
    }

    async stop(): Promise<void> {
        if (this.client) {
            await this.client.stop();
            this.client = undefined;
        }
    }

    private getServerOptions(): ServerOptions {
        // Try to find the effectusc binary
        const effectuscPath = this.findEffectuscBinary();
        
        if (effectuscPath) {
            // Use the effectusc binary with LSP mode
            return {
                command: effectuscPath,
                args: ['lsp'],
                transport: TransportKind.stdio
            };
        } else {
            // Fallback: try to use a Node.js implementation or embedded server
            const serverModule = vscode.Uri.joinPath(
                this.context.extensionUri,
                'server',
                'server.js'
            );

            return {
                run: {
                    module: serverModule.fsPath,
                    transport: TransportKind.ipc
                },
                debug: {
                    module: serverModule.fsPath,
                    transport: TransportKind.ipc,
                    options: {
                        execArgv: ['--nolazy', '--inspect=6009']
                    }
                }
            };
        }
    }

    private getClientOptions(): LanguageClientOptions {
        return {
            documentSelector: [
                { scheme: 'file', language: 'effectus' }
            ],
            synchronize: {
                // Synchronize configuration changes
                configurationSection: 'effectus',
                // Watch for changes to schema files
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
                // Custom middleware for handling Effectus-specific features
                provideCompletionItem: (document, position, context, token, next) => {
                    // Add custom completion logic here if needed
                    return next(document, position, context, token);
                },
                provideHover: (document, position, token, next) => {
                    // Add custom hover logic here if needed
                    return next(document, position, token);
                },
                handleDiagnostics: (uri, diagnostics, next) => {
                    // Custom diagnostic handling
                    const effectusDiagnostics = diagnostics.filter(d => 
                        d.source === 'effectus' || d.source === 'effectusc'
                    );
                    next(uri, effectusDiagnostics);
                }
            }
        };
    }

    private findEffectuscBinary(): string | undefined {
        const config = vscode.workspace.getConfiguration('effectus');
        
        // Check if user specified a custom path
        const customPath = config.get<string>('lsp.serverPath');
        if (customPath) {
            return customPath;
        }

        // Try common locations
        const commonPaths = [
            // In workspace bin directory
            vscode.workspace.workspaceFolders?.[0]?.uri.fsPath + '/bin/effectusc',
            // In PATH
            'effectusc',
            // In Go bin directory
            process.env.GOPATH + '/bin/effectusc',
            process.env.HOME + '/go/bin/effectusc'
        ];

        for (const path of commonPaths) {
            if (path && this.fileExists(path)) {
                return path;
            }
        }

        return undefined;
    }

    private fileExists(path: string): boolean {
        try {
            const fs = require('fs');
            return fs.existsSync(path);
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
