import * as assert from 'assert';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { resolveEffectusc, runEffectusc } from '../../effectusc';

suite('effectusc resolver', () => {
    let directory: string;

    setup(() => {
        directory = fs.mkdtempSync(path.join(os.tmpdir(), 'effectusc-resolver-'));
    });

    teardown(() => {
        fs.rmSync(directory, { recursive: true, force: true });
    });

    function executable(name: string, body = '#!/bin/sh\nexit 0\n'): string {
        const file = path.join(directory, name);
        fs.writeFileSync(file, body, { mode: 0o755 });
        return file;
    }

    test('finds effectusc through PATH', () => {
        const binary = executable('effectusc');
        const result = resolveEffectusc({
            env: { PATH: directory },
            platform: process.platform
        });
        assert.strictEqual(result.path, binary);
        assert.strictEqual(result.source, 'path');
    });

    test('the explicit setting wins over PATH', () => {
        executable('effectusc');
        const configured = executable('configured-effectusc');
        const result = resolveEffectusc({
            configuredPath: configured,
            env: { PATH: directory },
            platform: process.platform
        });
        assert.strictEqual(result.path, configured);
        assert.strictEqual(result.source, 'setting');
    });

    test('an invalid explicit setting does not fall back', () => {
        executable('effectusc');
        const missing = path.join(directory, 'missing');
        const result = resolveEffectusc({
            configuredPath: missing,
            env: { PATH: directory },
            platform: process.platform
        });
        assert.strictEqual(result.path, undefined);
        assert.match(result.error || '', /effectus\.lsp\.serverPath/);
    });

    test('finds effectusc through explicit GOBIN', () => {
        const binary = executable('effectusc');
        const result = resolveEffectusc({
            env: { PATH: '', GOBIN: directory },
            platform: process.platform
        });
        assert.strictEqual(result.path, binary);
        assert.strictEqual(result.source, 'gobin');
    });

    test('does not construct paths from missing HOME or GOPATH', () => {
        const result = resolveEffectusc({ env: { PATH: '' }, platform: process.platform });
        assert.strictEqual(result.path, undefined);
        assert.ok(!result.error?.includes('undefined'));
    });

    test('runs the resolved executable without a shell', async () => {
        const binary = executable('effectusc', '#!/bin/sh\nprintf "%s" "$1"\n');
        const result = await runEffectusc(binary, ['lsp']);
        assert.strictEqual(result.exitCode, 0);
        assert.strictEqual(result.stdout, 'lsp');
    });
});
