import * as assert from 'assert';
import * as fs from 'fs';
import * as path from 'path';

suite('extension manifest', () => {
    const extensionRoot = path.resolve(__dirname, '../../..');
    const manifest = JSON.parse(fs.readFileSync(path.join(extensionRoot, 'package.json'), 'utf8'));
    const commandIds = new Set<string>(manifest.contributes.commands.map((entry: any) => entry.command));

    test('every menu command is contributed', () => {
        for (const entries of Object.values(manifest.contributes.menus || {}) as any[][]) {
            for (const entry of entries) {
                assert.ok(commandIds.has(entry.command), `${entry.command} is not contributed`);
            }
        }
    });

    test('unsupported public surface is absent', () => {
        const text = JSON.stringify(manifest);
        for (const unsupported of [
            'effectus.rule.test',
            'effectus.rule.testWithData',
            'effectus.testRule',
            'effectus.schema.explore',
            'effectus.lineage.show',
            'effectusSchemas',
            'effectusLineage',
            'effectusMetrics',
            'effectus.performance.showMetrics',
            'effectus.dev.startServer',
            'effectus.dev.stopServer'
        ]) {
            assert.ok(!text.includes(unsupported), `${unsupported} remains in the manifest`);
        }
    });

    test('the package excludes TypeScript build configuration', () => {
        const ignore = fs.readFileSync(path.join(extensionRoot, '.vscodeignore'), 'utf8');
        assert.match(ignore, /^tsconfig\*\.json$/m);
    });

    test('the package has no nonexistent Node server fallback', () => {
        const files = ['src/languageClient.ts', 'src/extension.ts', 'package.json', '.vscodeignore'];
        const text = files.map(file => fs.readFileSync(path.join(extensionRoot, file), 'utf8')).join('\n');
        assert.ok(!text.includes('server/server.js'));
    });

    test('missing-binary help names both discovery methods and both actions', () => {
        const source = fs.readFileSync(path.join(extensionRoot, 'src/languageClient.ts'), 'utf8');
        for (const expected of ['PATH', 'effectus.lsp.serverPath', 'Install Effectus', 'Open Settings']) {
            assert.ok(source.includes(expected), `missing-binary help does not include ${expected}`);
        }
    });
});
