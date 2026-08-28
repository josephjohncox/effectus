import * as assert from 'assert';
import * as fs from 'fs';
import * as path from 'path';
import * as vscode from 'vscode';

export async function runCommandContract(): Promise<void> {
    const extension = vscode.extensions.getExtension('effectus.effectus-language-support');
    assert.ok(extension, 'packaged extension is installed');
    const packageRoot = extension.extensionPath;
    assert.ok(fs.existsSync(path.join(packageRoot, 'out', 'extension.js')), 'compiled activation entrypoint is packaged');
    assert.ok(fs.readdirSync(packageRoot).some(name => name.endsWith('.vsix')), 'VSIX was built before activation test');

    await vscode.workspace.getConfiguration('effectus').update('lsp.enabled', false, vscode.ConfigurationTarget.Workspace);
    await vscode.workspace.getConfiguration('effectus').update('runtime.apiUrl', 'http://127.0.0.1:1', vscode.ConfigurationTarget.Workspace);
    await extension.activate();

    const registered = new Set(await vscode.commands.getCommands(true));
    const contributed = (extension.packageJSON.contributes.commands as Array<{ command: string }>).map(item => item.command);
    assert.ok(contributed.length > 0);
    for (const command of contributed) {
        assert.ok(registered.has(command), `${command} is contributed but not registered`);
        await vscode.commands.executeCommand(command);
    }
}
