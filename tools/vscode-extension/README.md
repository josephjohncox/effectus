# Effectus Rule Files for VS Code

This extension provides syntax highlighting and snippets for `.eff` and `.effx`
files. It does not run Effectus commands or connect to a daemon.

Use the supported command-line interface from a terminal with a SourceBundle:

```bash
effectusc check --bundle effectus.source-bundle.v1.json
effectusc compile --bundle effectus.source-bundle.v1.json --output checked.pb
effectusc inspect --bundle effectus.source-bundle.v1.json
```

For durable execution, deploy an immutable bundle with `effectusd` and use its
documented authenticated HTTP or gRPC admission interfaces.
