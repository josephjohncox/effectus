import * as assert from 'assert';
import * as fs from 'fs';
import * as http from 'http';
import * as path from 'path';
import * as vscode from 'vscode';
import { ValidationResult } from '../../compilerOperations';
import { EffectusLanguageClient } from '../../languageClient';

const extensionId = 'effectus.effectus-language-support';
const mode = process.env.EFFECTUS_TEST_MODE || 'source';

suite(`Effectus extension (${mode})`, () => {
    let extension: vscode.Extension<any>;
    let workspaceRoot: string;

    suiteSetup(async () => {
        workspaceRoot = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath || '';
        assert.ok(workspaceRoot, 'test workspace is open');
        await vscode.workspace.getConfiguration('effectus').update(
            'lsp.enabled',
            false,
            vscode.ConfigurationTarget.Workspace
        );
        extension = vscode.extensions.getExtension(extensionId)!;
        assert.ok(extension, `${extensionId} is installed`);
        await extension.activate();
    });

    teardown(async () => {
        await vscode.commands.executeCommand('workbench.action.closeAllEditors');
    });

    test('activates with LSP disabled and registers every contributed command', async () => {
        assert.ok(extension.isActive);
        const registered = new Set(await vscode.commands.getCommands(true));
        const contributed = extension.packageJSON.contributes.commands.map((entry: any) => entry.command);
        for (const command of contributed) {
            assert.ok(registered.has(command), `${command} is not registered after activation`);
        }
        for (const entries of Object.values(extension.packageJSON.contributes.menus || {}) as any[][]) {
            for (const entry of entries) {
                assert.ok(contributed.includes(entry.command), `${entry.command} is not contributed`);
            }
        }
    });

    test('invokes every contributed command without command-not-found failures', async () => {
        const oldPath = process.env.PATH;
        if (mode === 'package' && process.platform !== 'win32') {
            const pathDirectory = path.join(workspaceRoot, 'package-path-bin');
            fs.mkdirSync(pathDirectory, { recursive: true });
            const marker = path.join(workspaceRoot, 'package-path-lsp-args');
            writeFakeLanguageServer(path.join(pathDirectory, 'effectusc'), marker);
            process.env.PATH = `${pathDirectory}${path.delimiter}${oldPath || ''}`;
            await setServerPath('');
            const client = new EffectusLanguageClient();
            assert.strictEqual(await client.start(), true);
            assert.strictEqual(fs.readFileSync(marker, 'utf8'), 'lsp');
            await client.stop();
        }
        try {
            for (const entry of extension.packageJSON.contributes.commands) {
                await vscode.commands.executeCommand(entry.command);
            }
        } finally {
            process.env.PATH = oldPath;
        }
    });

    test('initializes the documented workspace layout', async () => {
        await vscode.commands.executeCommand('effectus.workspace.init');
        for (const relative of [
            '.effectus/config.yaml',
            '.effectus/schemas',
            '.effectus/verbs',
            'examples'
        ]) {
            assert.ok(fs.existsSync(path.join(workspaceRoot, relative)), `${relative} was not created`);
        }
    });

    if (process.platform !== 'win32') {
        test('starts effectusc lsp from PATH and lets the explicit path win', async () => {
            const oldPath = process.env.PATH;
            const pathDirectory = path.join(workspaceRoot, 'path-bin');
            fs.mkdirSync(pathDirectory, { recursive: true });
            const pathMarker = path.join(workspaceRoot, 'path-lsp-args');
            writeFakeLanguageServer(path.join(pathDirectory, 'effectusc'), pathMarker);
            process.env.PATH = `${pathDirectory}${path.delimiter}${oldPath || ''}`;
            await setServerPath('');

            const pathClient = new EffectusLanguageClient();
            assert.strictEqual(await pathClient.start(), true);
            assert.strictEqual(fs.readFileSync(pathMarker, 'utf8'), 'lsp');
            await pathClient.stop();

            const explicitMarker = path.join(workspaceRoot, 'explicit-lsp-args');
            const explicit = writeFakeLanguageServer(
                path.join(workspaceRoot, 'explicit-effectusc'),
                explicitMarker
            );
            await setServerPath(explicit);
            const explicitClient = new EffectusLanguageClient();
            assert.strictEqual(await explicitClient.start(), true);
            assert.strictEqual(fs.readFileSync(explicitMarker, 'utf8'), 'lsp');
            await explicitClient.stop();
            process.env.PATH = oldPath;
        });

        test('handles a missing LSP binary without constructing a fallback client', async () => {
            await setServerPath(path.join(workspaceRoot, 'missing-lsp-effectusc'));
            const client = new EffectusLanguageClient();
            const startup = client.start();
            await new Promise(resolve => setTimeout(resolve, 100));
            await vscode.commands.executeCommand('notifications.clearAll');
            assert.strictEqual(await startup, false);
            assert.strictEqual(client.isRunning(), false);
        });

        test('formats dirty content and leaves it unchanged after compiler failure', async () => {
            const compiler = writeFakeCompiler('fake-effectusc');
            await setServerPath(compiler);
            const document = await openRule('format.eff', 'unformatted\n');
            await vscode.commands.executeCommand('effectus.rule.format');
            assert.strictEqual(document.getText(), 'rule "formatted" priority 0 {\n}\n');

            const failure = writeFakeCompiler('failing-effectusc', true);
            await setServerPath(failure);
            await replaceDocument(document, 'must stay unchanged\n');
            await vscode.commands.executeCommand('effectus.rule.format');
            assert.strictEqual(document.getText(), 'must stay unchanged\n');
        });

        test('returns effectusd success and 422 diagnostics', async () => {
            const server = http.createServer((request, response) => {
                const chunks: Buffer[] = [];
                request.on('data', chunk => chunks.push(chunk));
                request.on('end', () => {
                    const payload = JSON.parse(Buffer.concat(chunks).toString('utf8'));
                    response.setHeader('Content-Type', 'application/json');
                    if (payload.content.includes('INVALID')) {
                        response.statusCode = 422;
                        response.end(JSON.stringify({ diagnostics: [{ line: 1, column: 1, severity: 'error', message: 'runtime invalid rule' }] }));
                        return;
                    }
                    response.statusCode = 200;
                    response.end(JSON.stringify({ diagnostics: [] }));
                });
            });
            await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
            const address = server.address();
            assert.ok(address && typeof address !== 'string');
            await vscode.workspace.getConfiguration('effectus').update('runtime.apiUrl', `http://127.0.0.1:${address.port}`, vscode.ConfigurationTarget.Workspace);
            try {
                const document = await openRule('runtime-validation.eff', 'VALID\n');
                const success = await vscode.commands.executeCommand<ValidationResult>('effectus.validateRule');
                assert.strictEqual(success.source, 'runtime');
                assert.strictEqual(success.diagnostics.length, 0);

                await replaceDocument(document, 'INVALID\n');
                const failure = await vscode.commands.executeCommand<ValidationResult>('effectus.validateRule');
                assert.strictEqual(failure.source, 'runtime');
                assert.strictEqual(failure.diagnostics.length, 1);
                assert.match(failure.diagnostics[0].message, /runtime invalid rule/);
            } finally {
                await vscode.workspace.getConfiguration('effectus').update('runtime.apiUrl', '', vscode.ConfigurationTarget.Workspace);
                await new Promise<void>((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
            }
        });

        test('reports compiler validation success and diagnostics separately', async () => {
            await setServerPath(writeFakeCompiler('validation-effectusc'));
            const valid = await openRule('valid.eff', 'VALID\n');
            const success = await vscode.commands.executeCommand<ValidationResult>('effectus.validateRule');
            assert.strictEqual(success.status, 'validated');
            assert.strictEqual(success.source, 'compiler');
            assert.strictEqual(success.diagnostics.length, 0);

            await replaceDocument(valid, 'INVALID\n');
            const failure = await vscode.commands.executeCommand<ValidationResult>('effectus.validateRule');
            assert.strictEqual(failure.status, 'validated');
            assert.strictEqual(failure.source, 'compiler');
            assert.strictEqual(failure.diagnostics.length, 1);
            assert.match(failure.diagnostics[0].message, /invalid rule/);
        });

        test('does not report success when validation is unavailable', async () => {
            await setServerPath(path.join(workspaceRoot, 'missing-effectusc'));
            await openRule('unavailable.eff', 'VALID\n');
            const result = await vscode.commands.executeCommand<ValidationResult>('effectus.validateRule');
            assert.strictEqual(result.status, 'unavailable');
            assert.strictEqual(result.diagnostics.length, 0);
            assert.match(result.reason || '', /effectus\.lsp\.serverPath/);
        });
    }

    function writeFakeLanguageServer(executable: string, marker: string): string {
        const script = `#!/usr/bin/env node
const fs = require('fs');
fs.writeFileSync(${JSON.stringify(marker)}, process.argv.slice(2).join(' '));
let buffer = Buffer.alloc(0);
process.stdin.on('data', chunk => {
  buffer = Buffer.concat([buffer, chunk]);
  while (true) {
    const boundary = buffer.indexOf('\\r\\n\\r\\n');
    if (boundary < 0) return;
    const header = buffer.slice(0, boundary).toString();
    const match = /Content-Length: (\\d+)/i.exec(header);
    if (!match) process.exit(2);
    const length = Number(match[1]);
    if (buffer.length < boundary + 4 + length) return;
    const message = JSON.parse(buffer.slice(boundary + 4, boundary + 4 + length).toString());
    buffer = buffer.slice(boundary + 4 + length);
    if (message.method === 'initialize') reply(message.id, { capabilities: {} });
    if (message.method === 'shutdown') reply(message.id, null);
    if (message.method === 'exit') process.exit(0);
  }
});
function reply(id, result) {
  const body = JSON.stringify({ jsonrpc: '2.0', id, result });
  process.stdout.write('Content-Length: ' + Buffer.byteLength(body) + '\\r\\n\\r\\n' + body);
}
`;
        fs.writeFileSync(executable, script, { mode: 0o755 });
        return executable;
    }

    function writeFakeCompiler(name: string, failFormat = false): string {
        const executable = path.join(workspaceRoot, name);
        const script = `#!/bin/sh
case "$1" in
  format)
    ${failFormat ? 'echo "format failed" >&2; exit 2' : "printf 'rule \"formatted\" priority 0 {\\n}\\n\\n'; exit 0"}
    ;;
  typecheck)
    file="$2"
    if grep -q INVALID "$file"; then
      echo "$file:1:1: error: invalid rule" >&2
      exit 1
    fi
    exit 0
    ;;
  lsp)
    exit 0
    ;;
esac
exit 3
`;
        fs.writeFileSync(executable, script, { mode: 0o755 });
        return executable;
    }

    async function setServerPath(value: string): Promise<void> {
        await vscode.workspace.getConfiguration('effectus').update(
            'lsp.serverPath',
            value,
            vscode.ConfigurationTarget.Workspace
        );
    }

    async function openRule(name: string, content: string): Promise<vscode.TextDocument> {
        const file = path.join(workspaceRoot, name);
        fs.writeFileSync(file, 'saved content\n');
        const document = await vscode.workspace.openTextDocument(file);
        await vscode.window.showTextDocument(document);
        await replaceDocument(document, content);
        assert.ok(document.isDirty, 'the test document must be unsaved');
        return document;
    }

    async function replaceDocument(document: vscode.TextDocument, content: string): Promise<void> {
        const last = document.lineAt(document.lineCount - 1);
        const edit = new vscode.WorkspaceEdit();
        edit.replace(document.uri, new vscode.Range(0, 0, last.lineNumber, last.text.length), content);
        assert.ok(await vscode.workspace.applyEdit(edit));
    }
});
