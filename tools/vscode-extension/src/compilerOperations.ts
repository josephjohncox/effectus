import * as fs from 'fs/promises';
import * as os from 'os';
import * as path from 'path';
import * as vscode from 'vscode';
import { EffectuscResolution, resolveEffectusc, runEffectusc } from './effectusc';

export interface ValidationResult {
    status: 'validated' | 'unavailable';
    diagnostics: vscode.Diagnostic[];
    source?: 'runtime' | 'diagnostics' | 'compiler';
    reason?: string;
}

export function resolveConfiguredEffectusc(): EffectuscResolution {
    const configuration = vscode.workspace.getConfiguration('effectus');
    return resolveEffectusc({
        configuredPath: configuration.get<string>('lsp.serverPath'),
        workspaceFolder: vscode.workspace.workspaceFolders?.[0]?.uri.fsPath
    });
}

async function withTemporaryDocument<T>(
    document: vscode.TextDocument,
    operation: (filePath: string) => Promise<T>
): Promise<T> {
    const directory = await fs.mkdtemp(path.join(os.tmpdir(), 'effectus-vscode-'));
    const extension = document.fileName.toLowerCase().endsWith('.effx') ? '.effx' : '.eff';
    const filePath = path.join(directory, `document${extension}`);
    try {
        await fs.writeFile(filePath, document.getText(), { encoding: 'utf8', mode: 0o600 });
        return await operation(filePath);
    } finally {
        await fs.rm(directory, { recursive: true, force: true });
    }
}

export async function formatDocument(editor: vscode.TextEditor): Promise<void> {
    const document = editor.document;
    const version = document.version;
    const resolution = resolveConfiguredEffectusc();
    if (!resolution.path) {
        throw new Error(resolution.error || 'effectusc is unavailable');
    }

    const result = await withTemporaryDocument(document, filePath => runEffectusc(
        resolution.path!,
        ['format', '--stdout', '--write=false', filePath],
        { cwd: vscode.workspace.workspaceFolders?.[0]?.uri.fsPath }
    ));
    if (result.exitCode !== 0) {
        throw new Error(result.stderr.trim() || `effectusc format exited with code ${result.exitCode}`);
    }

    // effectusc uses fmt.Println for stdout, which adds one newline after the formatted text.
    const formatted = result.stdout.replace(/\r?\n$/, '');
    if (document.version !== version) {
        throw new Error('The document changed while effectusc was formatting it');
    }
    if (formatted === document.getText()) {
        return;
    }

    const lastLine = document.lineAt(document.lineCount - 1);
    const edit = new vscode.WorkspaceEdit();
    edit.replace(
        document.uri,
        new vscode.Range(0, 0, lastLine.lineNumber, lastLine.text.length),
        formatted
    );
    if (!await vscode.workspace.applyEdit(edit)) {
        throw new Error('VS Code rejected the formatting edit');
    }
}

function diagnosticFromCompilerOutput(document: vscode.TextDocument, output: string): vscode.Diagnostic[] {
    const diagnostics: vscode.Diagnostic[] = [];
    const pattern = /^(.*?):(\d+):(\d+):\s*(?:error|warning|info)?:?\s*(.+)$/i;
    for (const line of output.split(/\r?\n/).filter(Boolean)) {
        const match = line.match(pattern);
        if (!match) {
            continue;
        }
        const lineNumber = Math.max(Number(match[2]) - 1, 0);
        const column = Math.max(Number(match[3]) - 1, 0);
        const diagnostic = new vscode.Diagnostic(
            new vscode.Range(lineNumber, column, lineNumber, column + 1),
            match[4],
            /warning/i.test(line) ? vscode.DiagnosticSeverity.Warning : vscode.DiagnosticSeverity.Error
        );
        diagnostic.source = 'effectusc';
        diagnostics.push(diagnostic);
    }
    if (diagnostics.length === 0 && output.trim()) {
        const diagnostic = new vscode.Diagnostic(
            new vscode.Range(0, 0, 0, Math.min(document.lineAt(0).text.length, 1)),
            output.trim(),
            vscode.DiagnosticSeverity.Error
        );
        diagnostic.source = 'effectusc';
        diagnostics.push(diagnostic);
    }
    return diagnostics;
}

export async function validateWithCompilerOrDiagnostics(document: vscode.TextDocument): Promise<ValidationResult> {
    const currentDiagnostics = vscode.languages.getDiagnostics(document.uri).filter(diagnostic =>
        diagnostic.source === 'effectus' || diagnostic.source === 'effectusc'
    );
    if (currentDiagnostics.length > 0) {
        return { status: 'validated', diagnostics: currentDiagnostics, source: 'diagnostics' };
    }

    const resolution = resolveConfiguredEffectusc();
    if (!resolution.path) {
        return {
            status: 'unavailable',
            diagnostics: [],
            reason: resolution.error || 'effectusc is unavailable'
        };
    }

    const result = await withTemporaryDocument(document, filePath => runEffectusc(
        resolution.path!,
        ['typecheck', filePath],
        { cwd: vscode.workspace.workspaceFolders?.[0]?.uri.fsPath }
    ));
    if (result.exitCode === 0) {
        return { status: 'validated', diagnostics: [], source: 'compiler' };
    }
    const output = [result.stderr, result.stdout].filter(Boolean).join('\n')
        || `effectusc typecheck exited with code ${result.exitCode}`;
    return {
        status: 'validated',
        diagnostics: diagnosticFromCompilerOutput(document, output),
        source: 'compiler'
    };
}
