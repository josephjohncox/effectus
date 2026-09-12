import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { describe, it } from 'node:test';

const root = path.resolve(import.meta.dirname, '..');
const readJSON = (name) => JSON.parse(fs.readFileSync(path.join(root, name), 'utf8'));
const manifest = readJSON('package.json');

describe('extension manifest', () => {
    it('declares syntax support without an extension-host runtime', () => {
        assert.ok(manifest.contributes.languages.length > 0);
        assert.ok(manifest.contributes.grammars.length > 0);
        assert.ok(manifest.contributes.snippets.length > 0);
        for (const entry of ['main', 'browser', 'activationEvents']) {
            assert.equal(manifest[entry], undefined);
        }
        for (const contribution of ['commands', 'menus', 'configuration']) {
            assert.equal(manifest.contributes[contribution], undefined);
        }
    });

    it('loads the language configuration, grammar, snippets, and icons from the manifest', () => {
        const language = manifest.contributes.languages.find(({ id }) => id === 'effectus');
        assert.ok(language);
        assert.deepEqual(language.extensions, ['.eff', '.effx']);
        assert.ok(readJSON(language.configuration).brackets.length > 0);
        for (const icon of Object.values(language.icon)) {
            assert.match(fs.readFileSync(path.join(root, icon), 'utf8'), /<svg\b/);
        }
        for (const grammar of manifest.contributes.grammars) {
            assert.equal(grammar.language, language.id);
            const content = readJSON(grammar.path);
            assert.equal(content.scopeName, grammar.scopeName);
            assert.ok(content.patterns.length > 0);
        }
        for (const snippets of manifest.contributes.snippets) {
            assert.equal(snippets.language, language.id);
            assert.ok(Object.keys(readJSON(snippets.path)).length > 0);
        }
    });

    it('does not advertise removed CLI or daemon APIs', () => {
        const text = JSON.stringify(manifest).toLowerCase();
        for (const unsupported of ['lsp', 'typecheck', 'hotreload', 'hot-reload', 'rule.format', '/api/rules']) {
            assert.ok(!text.includes(unsupported), `${unsupported} remains in the extension contract`);
        }
    });
});
