import * as vscode from 'vscode';
import * as http from 'http';
import * as https from 'https';

export type RuntimeConfig = {
    url: string;
    token?: string;
};

export interface RuntimeValidationResult {
    available: boolean;
    diagnostics: any[];
}

/** Manages the supported effectusd HTTP validation and hotload flow. */
export class HotReloadManager {
    private readonly statusBarItem: vscode.StatusBarItem;
    private running = false;

    constructor(context: vscode.ExtensionContext) {
        this.statusBarItem = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Right, 100);
        this.updateStatusBar();
        context.subscriptions.push(this.statusBarItem);
    }

    async startServer(): Promise<void> {
        if (this.running) {
            throw new Error('Runtime hotload is already enabled');
        }
        const runtime = this.getRuntimeConfig();
        if (!runtime) {
            throw new Error('Set effectus.runtime.apiUrl to an effectusd server before enabling runtime hotload');
        }

        await this.request(runtime, '/api/status', undefined, 'get');
        this.running = true;
        this.updateStatusBar();
    }

    async stopServer(): Promise<void> {
        this.running = false;
        this.updateStatusBar();
    }

    isRunning(): boolean {
        return this.running;
    }

    async notifyRuleChange(uri: vscode.Uri): Promise<void> {
        if (!this.running) {
            return;
        }
        const runtime = this.getRuntimeConfig();
        if (!runtime) {
            this.running = false;
            this.updateStatusBar();
            return;
        }

        try {
            const document = await vscode.workspace.openTextDocument(uri);
            await this.request(runtime, '/api/rules/hotload', this.buildRulePayload(uri, document.getText()));
        } catch (error) {
            console.error('Failed to hotload rule:', error);
        }
    }

    async validateRule(document: vscode.TextDocument): Promise<RuntimeValidationResult> {
        const runtime = this.getRuntimeConfig();
        if (!runtime) {
            return { available: false, diagnostics: [] };
        }
        const response = await this.request(
            runtime,
            '/api/rules/validate',
            this.buildRulePayload(document.uri, document.getText())
        );
        return { available: true, diagnostics: response?.diagnostics || [] };
    }

    dispose(): void {
        this.running = false;
        this.statusBarItem.dispose();
    }

    private getRuntimeConfig(): RuntimeConfig | undefined {
        const configuration = vscode.workspace.getConfiguration('effectus');
        const url = (configuration.get<string>('runtime.apiUrl') || '').trim();
        if (!url) {
            return undefined;
        }
        const token = (configuration.get<string>('runtime.apiToken') || '').trim();
        return { url, token };
    }

    private buildRulePayload(uri: vscode.Uri, content: string): any {
        return {
            path: uri.fsPath,
            format: uri.fsPath.toLowerCase().endsWith('.effx') ? 'effx' : 'eff',
            content,
            replace: false
        };
    }

    private updateStatusBar(): void {
        if (this.running) {
            this.statusBarItem.text = '$(server-process) Effectus Runtime';
            this.statusBarItem.tooltip = 'effectusd runtime hotload is enabled';
            this.statusBarItem.command = 'effectus.dev.stopServer';
        } else {
            this.statusBarItem.text = '$(server-environment) Effectus Runtime';
            this.statusBarItem.tooltip = 'Enable hotload against the configured effectusd runtime';
            this.statusBarItem.command = 'effectus.dev.startServer';
        }
        this.statusBarItem.backgroundColor = undefined;
        this.statusBarItem.show();
    }

    private async request(
        runtime: RuntimeConfig,
        endpoint: string,
        payload?: any,
        method: 'post' | 'get' = 'post'
    ): Promise<any> {
        const base = runtime.url.replace(/\/+$/, '');
        const url = new URL(`${base}${endpoint}`);
        if (url.protocol !== 'http:' && url.protocol !== 'https:') {
            throw new Error(`Unsupported runtime protocol: ${url.protocol}`);
        }
        const body = method === 'get' ? undefined : Buffer.from(JSON.stringify(payload ?? {}));
        const headers: Record<string, string | number> = { Accept: 'application/json' };
        if (runtime.token) {
            headers.Authorization = `Bearer ${runtime.token}`;
        }
        if (body) {
            headers['Content-Type'] = 'application/json';
            headers['Content-Length'] = body.length;
        }

        return new Promise((resolve, reject) => {
            const transport = url.protocol === 'https:' ? https : http;
            const request = transport.request(url, {
                method: method === 'get' ? 'GET' : 'POST',
                headers,
                timeout: 10_000
            }, response => {
                const chunks: Buffer[] = [];
                let size = 0;
                response.on('data', (chunk: Buffer) => {
                    size += chunk.length;
                    if (size > 1 << 20) {
                        response.destroy(new Error('Runtime response exceeds 1 MiB'));
                        return;
                    }
                    chunks.push(chunk);
                });
                response.on('error', reject);
                response.on('end', () => {
                    const content = Buffer.concat(chunks).toString('utf8');
                    if (!response.statusCode || response.statusCode < 200 || response.statusCode >= 300) {
                        reject(new Error(`Runtime request failed with HTTP ${response.statusCode ?? 0}`));
                        return;
                    }
                    if (!content) {
                        resolve(undefined);
                        return;
                    }
                    try {
                        resolve(JSON.parse(content));
                    } catch {
                        reject(new Error('Runtime returned invalid JSON'));
                    }
                });
            });
            request.on('timeout', () => request.destroy(new Error('Runtime request timed out')));
            request.on('error', reject);
            if (body) {
                request.write(body);
            }
            request.end();
        });
    }
}
