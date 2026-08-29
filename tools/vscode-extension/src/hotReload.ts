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

/** Calls the effectusd candidate-validation endpoint. */
export class HotReloadManager {
    async validateRule(document: vscode.TextDocument): Promise<RuntimeValidationResult> {
        const runtime = this.getRuntimeConfig();
        if (!runtime) {
            return { available: false, diagnostics: [] };
        }
        try {
            const response = await this.request(
                runtime,
                '/api/rules/validate',
                this.buildRulePayload(document.uri, document.getText())
            );
            return { available: true, diagnostics: response?.diagnostics || [] };
        } catch {
            return { available: false, diagnostics: [] };
        }
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

    private async request(runtime: RuntimeConfig, endpoint: string, payload: any): Promise<any> {
        const base = runtime.url.replace(/\/+$/, '');
        const url = new URL(`${base}${endpoint}`);
        if (url.protocol !== 'http:' && url.protocol !== 'https:') {
            throw new Error(`Unsupported runtime protocol: ${url.protocol}`);
        }
        const body = Buffer.from(JSON.stringify(payload ?? {}));
        const headers: Record<string, string | number> = {
            Accept: 'application/json',
            'Content-Type': 'application/json',
            'Content-Length': body.length
        };
        if (runtime.token) {
            headers.Authorization = `Bearer ${runtime.token}`;
        }

        return new Promise((resolve, reject) => {
            const transport = url.protocol === 'https:' ? https : http;
            const request = transport.request(url, { method: 'POST', headers, timeout: 10_000 }, response => {
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
                    const status = response.statusCode || 0;
                    if ((status < 200 || status >= 300) && status !== 422) {
                        reject(new Error(`Runtime request failed with HTTP ${status}`));
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
            request.write(body);
            request.end();
        });
    }
}
