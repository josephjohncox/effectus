| Area                |  **Effectus** naming               | Purpose (one-liner)                                    |
| ------------------- |  --------------------------------- | ------------------------------------------------------ |
| Compiler CLI        |  **`effectusc`**                   | Parse + type-check rules, emit IR/JSON, fail on errors |
| Linter              |  **`effectus lint`**               | Stylistic checks, dead predicates, unreachable rules   |
| Executor daemon     |  **`effectusd`**                   | gRPC/HTTP service that ingests Facts ⟶ Effects         |
| Local runner        |  **`effectus run`**                | CLI harness for dev / CI, pipes JSON Facts ⟶ stdout    |
| Hot-reload side-car |  **`effectus-watch`**              | Watches rule directory, sends SIGHUP to `effectusd`    |
| VS Code ext         |  **`effectus-vscode`**             | Syntax highlight + autocompletion from schema registry |
| Go SDK module       |  `github.com/yourorg/effectus-go`  | Pure library: parser, compiler, executor stubs         |
| Docker image        |  `ghcr.io/yourorg/effectusd:<ver>` | Container for prod deploys                             |
| Env var prefix      |  **`EFFECTUS_…`**                  | e.g. `EFFECTUS_RULE_PATH=/etc/effectus`                |

### Quick examples

```bash
# 1. Compile & lint
effectusc compile rules/base.eff          # exit 0 or 1
effectus lint    rules/base.eff --fix     # auto-format

# 2. Run locally
cat facts/batch123.json | effectus run rules/  \
    --executor=log | jq .

# 3. Prod daemon (hot reload)
docker run -d \
  -v /etc/effectus:/rules \
  -e EFFECTUS_RULE_PATH=/rules \
  ghcr.io/yourorg/effectusd:v0.8.2
effectus-watch /rules --signal --pid=$(pgrep effectusd)
```