# v0.3 Go Compatibility Packages

The frozen v0.3 compatibility packages are introduced by the first future root
release that contains this branch. Published `v0.3.0` does not contain these
paths. There is no separate compatibility module or compatibility tag.

That introducing root release contains these import paths:

- `github.com/josephjohncox/effectus/compat/v03/embedded`
- `github.com/josephjohncox/effectus/compat/v03/executorhttp`
- `github.com/josephjohncox/effectus/compat/v03/invocation`

## Frozen surface

The v0.3 paths expose only the embedded checked-runtime, HTTP executor, and
invocation contracts introduced for v0.3. They do not provide adapters, list,
flow, loader, unified, or fake forwarding packages. New integrations should
use the current root packages unless they require this frozen source surface.

## Consumer check

Before the introducing root release merges, CI compiles an external-package
test against the local root module. Only after that root release exists, the
publish workflow creates a temporary external module and resolves these imports
through `https://proxy.golang.org`:

```bash
just smoke-compat "$ROOT_VERSION"
```

Set `ROOT_VERSION` to the version of the first root release containing this
branch. The smoke command accepts either `vMAJOR.MINOR.PATCH` or
`MAJOR.MINOR.PATCH`. It does not use a checkout replacement or a direct VCS
fallback.
