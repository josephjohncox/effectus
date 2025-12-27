import * as vscode from 'vscode';
import axios from 'axios';
import WebSocket from 'ws';

type RuntimeConfig = {
    url: string;
    token?: string;
};

export class HotReloadManager {
    private context: vscode.ExtensionContext;
    private webSocket: WebSocket | undefined;
    private serverProcess: any;
    private statusBarItem: vscode.StatusBarItem;
    private isServerRunning = false;

    constructor(context: vscode.ExtensionContext) {
        this.context = context;
        this.statusBarItem = vscode.window.createStatusBarItem(
            vscode.StatusBarAlignment.Right,
            100
        );
        this.updateStatusBar();
        context.subscriptions.push(this.statusBarItem);
    }

    async startServer(): Promise<void> {
        if (this.isServerRunning) {
            throw new Error('Development server is already running');
        }

        const config = vscode.workspace.getConfiguration('effectus');
        const runtime = this.getRuntimeConfig();
        const port = config.get<number>('hotReload.port', 9090);

        try {
            if (runtime) {
                await this.pingRuntime(runtime);
            } else {
                await this.startEffectusDevServer(port);
                await this.connectWebSocket(port);
            }
            this.isServerRunning = true;
            this.updateStatusBar();
            
            const label = runtime ? 'Effectus runtime hot reload enabled' : `Effectus development server started on port ${port}`;
            vscode.window.showInformationMessage(label);
        } catch (error) {
            this.isServerRunning = false;
            this.updateStatusBar();
            throw error;
        }
    }

    async stopServer(): Promise<void> {
        if (!this.isServerRunning) {
            return;
        }

        try {
            if (this.webSocket) {
                this.webSocket.close();
                this.webSocket = undefined;
            }

            if (this.serverProcess) {
                this.serverProcess.kill();
                this.serverProcess = undefined;
            }

            this.isServerRunning = false;
            this.updateStatusBar();
            
            const runtime = this.getRuntimeConfig();
            vscode.window.showInformationMessage(runtime ? 'Effectus runtime hot reload stopped' : 'Effectus development server stopped');
        } catch (error) {
            console.error('Error stopping development server:', error);
        }
    }

    isRunning(): boolean {
        return this.isServerRunning;
    }

    async notifyRuleChange(uri: vscode.Uri): Promise<void> {
        if (!this.isServerRunning) {
            return;
        }

        const runtime = this.getRuntimeConfig();
        if (runtime) {
            await this.pushRuleToRuntime(uri, runtime);
            return;
        }

        if (!this.webSocket || this.webSocket.readyState !== WebSocket.OPEN) {
            return;
        }

        try {
            const document = await vscode.workspace.openTextDocument(uri);
            const content = document.getText();
            
            const message = {
                type: 'rule_changed',
                file: uri.fsPath,
                content: content,
                timestamp: new Date().toISOString()
            };

            this.webSocket.send(JSON.stringify(message));
        } catch (error) {
            console.error('Failed to notify rule change:', error);
        }
    }

    dispose(): void {
        this.stopServer();
        if (this.statusBarItem) {
            this.statusBarItem.dispose();
        }
    }

    private async startEffectusDevServer(port: number): Promise<void> {
        return new Promise((resolve, reject) => {
            const { spawn } = require('child_process');
            
            // Try to start the effectusc development server
            const effectuscPath = this.findEffectuscBinary();
            if (!effectuscPath) {
                reject(new Error('effectusc binary not found. Please ensure Effectus is installed.'));
                return;
            }

            const workspaceRoot = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
            if (!workspaceRoot) {
                reject(new Error('No workspace folder open'));
                return;
            }

            this.serverProcess = spawn(effectuscPath, ['dev', '--port', port.toString()], {
                cwd: workspaceRoot,
                stdio: 'pipe'
            });

            let startupComplete = false;

            this.serverProcess.stdout?.on('data', (data: Buffer) => {
                const output = data.toString();
                console.log('Effectus dev server:', output);
                
                if (output.includes('Development server started') || output.includes(`listening on :${port}`)) {
                    if (!startupComplete) {
                        startupComplete = true;
                        resolve();
                    }
                }
            });

            this.serverProcess.stderr?.on('data', (data: Buffer) => {
                const output = data.toString();
                console.error('Effectus dev server error:', output);
            });

            this.serverProcess.on('error', (error: Error) => {
                if (!startupComplete) {
                    reject(new Error(`Failed to start development server: ${error.message}`));
                }
            });

            this.serverProcess.on('exit', (code: number) => {
                console.log(`Effectus dev server exited with code ${code}`);
                this.isServerRunning = false;
                this.updateStatusBar();
                
                if (code !== 0 && !startupComplete) {
                    reject(new Error(`Development server exited with code ${code}`));
                }
            });

            // Timeout after 10 seconds
            setTimeout(() => {
                if (!startupComplete) {
                    reject(new Error('Timeout waiting for development server to start'));
                }
            }, 10000);
        });
    }

    private async connectWebSocket(port: number): Promise<void> {
        return new Promise((resolve, reject) => {
            const wsUrl = `ws://localhost:${port}/ws`;
            this.webSocket = new WebSocket(wsUrl);

            if (this.webSocket) {
                this.webSocket.on('open', () => {
                    console.log('Connected to Effectus development server WebSocket');
                    resolve();
                });

                this.webSocket.on('message', (data: WebSocket.Data) => {
                    try {
                        const message = JSON.parse(data.toString());
                        this.handleWebSocketMessage(message);
                    } catch (error) {
                        console.error('Error parsing WebSocket message:', error);
                    }
                });

                this.webSocket.on('error', (error: Error) => {
                    console.error('WebSocket error:', error);
                    reject(error);
                });

                this.webSocket.on('close', () => {
                    console.log('WebSocket connection closed');
                    this.webSocket = undefined;
                });

                // Timeout after 5 seconds
                setTimeout(() => {
                    if (this.webSocket?.readyState !== WebSocket.OPEN) {
                        reject(new Error('Timeout connecting to WebSocket'));
                    }
                }, 5000);
            } else {
                reject(new Error('Failed to create WebSocket'));
            }
        });
    }

    private handleWebSocketMessage(message: any): void {
        switch (message.type) {
            case 'rule_compiled':
                this.showRuleCompilationResult(message);
                break;
            case 'rule_test_result':
                this.showRuleTestResult(message);
                break;
            case 'schema_updated':
                this.handleSchemaUpdate(message);
                break;
            case 'error':
                this.showError(message);
                break;
            default:
                console.log('Unknown WebSocket message type:', message.type);
        }
    }

    private showRuleCompilationResult(message: any): void {
        if (message.success) {
            this.statusBarItem.text = `$(check) Rule compiled successfully`;
            this.statusBarItem.tooltip = `Rule ${message.rule} compiled at ${new Date(message.timestamp).toLocaleTimeString()}`;
            this.statusBarItem.backgroundColor = new vscode.ThemeColor('statusBarItem.prominentBackground');
            
            // Clear the success indicator after 3 seconds
            setTimeout(() => {
                this.updateStatusBar();
            }, 3000);
        } else {
            vscode.window.showErrorMessage(`Rule compilation failed: ${message.error}`);
        }
    }

    private showRuleTestResult(message: any): void {
        if (message.success) {
            vscode.window.showInformationMessage(
                `Rule test passed: ${message.tests_passed}/${message.total_tests} tests`
            );
        } else {
            vscode.window.showWarningMessage(
                `Rule test failed: ${message.error}`
            );
        }
    }

    private handleSchemaUpdate(message: any): void {
        // Notify schema provider to refresh
        vscode.commands.executeCommand('effectus.schema.refresh');
        
        vscode.window.showInformationMessage(
            `Schema ${message.schema} has been updated`
        );
    }

    private showError(message: any): void {
        vscode.window.showErrorMessage(`Development server error: ${message.error}`);
    }

    private updateStatusBar(): void {
        const runtime = this.getRuntimeConfig();
        if (this.isServerRunning) {
            const mode = runtime ? 'Runtime' : 'Dev';
            this.statusBarItem.text = `$(server-process) Effectus ${mode}`;
            this.statusBarItem.tooltip = runtime
                ? 'Effectus runtime hot reload is active'
                : 'Effectus development server is running';
            this.statusBarItem.command = 'effectus.dev.stopServer';
            this.statusBarItem.backgroundColor = undefined;
        } else {
            const label = runtime ? 'Runtime' : 'Dev';
            this.statusBarItem.text = `$(server-environment) Effectus ${label}`;
            this.statusBarItem.tooltip = runtime
                ? 'Enable hot reload against effectusd'
                : 'Start Effectus development server';
            this.statusBarItem.command = 'effectus.dev.startServer';
            this.statusBarItem.backgroundColor = undefined;
        }
        this.statusBarItem.show();
    }

    public async validateRule(document: vscode.TextDocument): Promise<any[]> {
        const runtime = this.getRuntimeConfig();
        if (!runtime) {
            return [];
        }
        const payload = this.buildRulePayload(document.uri, document.getText(), false);
        const response = await this.postRule(runtime, '/api/rules/validate', payload);
        return response?.diagnostics || [];
    }

    private getRuntimeConfig(): RuntimeConfig | undefined {
        const config = vscode.workspace.getConfiguration('effectus');
        const url = (config.get<string>('runtime.apiUrl') || '').trim();
        if (!url) {
            return undefined;
        }
        const token = (config.get<string>('runtime.apiToken') || '').trim();
        return { url, token };
    }

    private async pingRuntime(runtime: RuntimeConfig): Promise<void> {
        await this.postRule(runtime, '/api/status', undefined, 'get');
    }

    private async pushRuleToRuntime(uri: vscode.Uri, runtime: RuntimeConfig): Promise<void> {
        try {
            const document = await vscode.workspace.openTextDocument(uri);
            const payload = this.buildRulePayload(uri, document.getText(), false);
            await this.postRule(runtime, '/api/rules/hotload', payload);
        } catch (error) {
            console.error('Failed to hotload rule:', error);
        }
    }

    private buildRulePayload(uri: vscode.Uri, content: string, replace: boolean): any {
        const path = uri.fsPath;
        const ext = path.toLowerCase().endsWith('.effx') ? 'effx' : 'eff';
        return {
            path,
            format: ext,
            content,
            replace
        };
    }

    private async postRule(runtime: RuntimeConfig, endpoint: string, payload?: any, method?: 'post' | 'get'): Promise<any> {
        const base = runtime.url.replace(/\/+$/, '');
        const url = `${base}${endpoint}`;
        const headers: Record<string, string> = {};
        if (runtime.token) {
            headers['Authorization'] = `Bearer ${runtime.token}`;
        }
        if (method === 'get') {
            const response = await axios.get(url, { headers });
            return response.data;
        }
        const response = await axios.post(url, payload, { headers });
        return response.data;
    }

    private findEffectuscBinary(): string | undefined {
        // Same logic as in languageClient.ts
        const config = vscode.workspace.getConfiguration('effectus');
        const customPath = config.get<string>('lsp.serverPath');
        if (customPath) {
            return customPath;
        }

        const commonPaths = [
            vscode.workspace.workspaceFolders?.[0]?.uri.fsPath + '/bin/effectusc',
            'effectusc',
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
} 
