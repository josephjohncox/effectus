import * as assert from 'assert';
import * as fs from 'fs';
import * as path from 'path';

suite('extension manifest', () => {
    const root = path.resolve(__dirname, '../../..');
    const manifest = JSON.parse(fs.readFileSync(path.join(root, 'package.json'), 'utf8'));

    test('declares only syntax support', () => {
        assert.ok(manifest.contributes.languages.length > 0);
        assert.ok(manifest.contributes.grammars.length > 0);
        assert.ok(manifest.contributes.snippets.length > 0);
        assert.deepStrictEqual(manifest.activationEvents, ['onLanguage:effectus']);
        assert.strictEqual(manifest.contributes.commands, undefined);
        assert.strictEqual(manifest.contributes.menus, undefined);
        assert.strictEqual(manifest.contributes.configuration, undefined);
    });

    test('does not advertise removed CLI or daemon APIs', () => {
        const text = [
            JSON.stringify(manifest),
            fs.readFileSync(path.join(root, 'src', 'extension.ts'), 'utf8')
        ].join('\n').toLowerCase();
        for (const unsupported of ['lsp', 'typecheck', 'hotreload', 'hot-reload', 'rule.format', '/api/rules']) {
            assert.ok(!text.includes(unsupported), `${unsupported} remains in the extension contract`);
        }
    });
});
