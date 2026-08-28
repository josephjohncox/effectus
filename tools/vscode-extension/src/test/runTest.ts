import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { spawnSync } from 'child_process';
import {
    downloadAndUnzipVSCode,
    resolveCliArgsFromVSCodeExecutablePath,
    runTests
} from '@vscode/test-electron';

const extensionRoot = path.resolve(__dirname, '../..');
const testsPath = path.resolve(__dirname, 'suite');
const mode = process.argv[2] || 'source';

async function main(): Promise<void> {
    if (process.platform === 'linux' && !process.env.DISPLAY && !process.env.EFFECTUS_TEST_XVFB) {
        const result = spawnSync('xvfb-run', ['-a', process.execPath, __filename, mode], {
            stdio: 'inherit',
            env: { ...process.env, EFFECTUS_TEST_XVFB: '1' }
        });
        if (result.error) {
            throw new Error(`A display is required for VS Code tests and xvfb-run failed: ${result.error.message}`);
        }
        process.exitCode = result.status || 0;
        return;
    }

    const temporaryRoot = fs.mkdtempSync(path.join(os.tmpdir(), `effectus-vscode-${mode}-`));
    const workspace = path.join(temporaryRoot, 'workspace');
    const userData = path.join(temporaryRoot, 'user-data');
    const extensions = path.join(temporaryRoot, 'extensions');
    fs.mkdirSync(path.join(workspace, '.vscode'), { recursive: true });
    fs.writeFileSync(
        path.join(workspace, '.vscode', 'settings.json'),
        JSON.stringify({ 'effectus.lsp.enabled': false })
    );

    try {
        const vscodeExecutablePath = await downloadAndUnzipVSCode();
        let developmentPath = extensionRoot;

        if (mode === 'package') {
            const vsix = path.join(temporaryRoot, 'effectus.vsix');
            packageExtension(vsix);
            installExtension(vscodeExecutablePath, vsix, userData, extensions);
            developmentPath = createHarness(temporaryRoot);
        } else if (mode !== 'source') {
            throw new Error(`Unknown test mode: ${mode}`);
        }

        await runTests({
            vscodeExecutablePath,
            extensionDevelopmentPath: developmentPath,
            extensionTestsPath: testsPath,
            extensionTestsEnv: { EFFECTUS_TEST_MODE: mode },
            launchArgs: [
                workspace,
                `--user-data-dir=${userData}`,
                `--extensions-dir=${extensions}`,
                '--disable-workspace-trust',
                '--skip-welcome',
                '--skip-release-notes'
            ]
        });
    } finally {
        fs.rmSync(temporaryRoot, { recursive: true, force: true });
    }
}

function packageExtension(output: string): void {
    const vsce = path.join(extensionRoot, 'node_modules', '@vscode', 'vsce', 'vsce');
    const result = spawnSync(process.execPath, [vsce, 'package', '--out', output], {
        cwd: extensionRoot,
        encoding: 'utf8'
    });
    if (result.status !== 0) {
        throw new Error(`VSIX packaging failed:\n${result.stdout}\n${result.stderr}`);
    }
    if (!fs.existsSync(output)) {
        throw new Error('VSIX packaging did not create the requested output');
    }
}

function installExtension(executable: string, vsix: string, userData: string, extensions: string): void {
    const [cli, ...baseArgs] = resolveCliArgsFromVSCodeExecutablePath(executable);
    const result = spawnSync(cli, [
        ...baseArgs,
        '--install-extension', vsix,
        '--force',
        `--user-data-dir=${userData}`,
        `--extensions-dir=${extensions}`
    ], {
        encoding: 'utf8',
        shell: process.platform === 'win32'
    });
    if (result.status !== 0) {
        throw new Error(`VSIX installation failed:\n${result.stdout}\n${result.stderr}`);
    }
}

function createHarness(temporaryRoot: string): string {
    const harness = path.join(temporaryRoot, 'harness');
    fs.mkdirSync(harness, { recursive: true });
    fs.writeFileSync(path.join(harness, 'package.json'), JSON.stringify({
        name: 'effectus-package-test-harness',
        displayName: 'Effectus Package Test Harness',
        version: '1.0.0',
        publisher: 'effectus-tests',
        engines: { vscode: '^1.74.0' },
        main: './extension.js',
        activationEvents: ['*']
    }));
    fs.writeFileSync(path.join(harness, 'extension.js'), 'exports.activate = function () {};\n');
    return harness;
}

void main().catch(error => {
    console.error(error);
    process.exitCode = 1;
});
