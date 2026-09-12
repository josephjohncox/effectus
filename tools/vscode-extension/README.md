# Effectus Rule Files for VS Code

This extension provides syntax highlighting and snippets for `.eff` and `.effx`
files. It does not run Effectus commands or connect to a daemon.

The package contains declarative language configuration, a TextMate grammar,
snippets, and icons. It has no extension-host entry point or compiled runtime.
The supported editor range remains VS Code `^1.74.0`.

For development, use Node.js 22 or later and run `npm ci`, `npm test`, and
`npm run package` in this directory. Tests use Node's built-in test runner;
TypeScript and editor API type packages are not required. Packaging runs the
tests and excludes development files and any stale `out/` artifacts.

Use the supported command-line interface from a terminal with a SourceBundle:

```bash
effectusc check --bundle effectus.source-bundle.v1.json
effectusc compile --bundle effectus.source-bundle.v1.json --output checked.pb
effectusc inspect --bundle effectus.source-bundle.v1.json
```

For durable execution, deploy an immutable bundle with `effectusd` and use its
documented authenticated HTTP or gRPC admission interfaces.
