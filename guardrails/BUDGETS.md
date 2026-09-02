# Intentional Surface Budgets

These are release boundaries, not aspirational targets. The inventories in this
folder are generated and reviewed with `go run ./internal/guardrails/cmd snapshot`.

| Surface | Budget | Current | Definition |
| --- | ---: | ---: | --- |
| Product package domains | 18 | 18 | Importable top-level root packages plus the three explicit `compat` packages. The order-review helper is internal; `examples/order_review` contains only shared data and rules. Generated protocol packages and implementation subpackages are not product domains. |
| Immediate examples | 3 | 3 | `embedded_orders`, `standalone_executor`, and `grpc_execution`. Shared `order_review` data and rules support those examples but are not an importable product package. |
| Visible Just recipes | 18 | 18 | Recipes shown by `just --list`; maintenance recipes remain private. |
| Go modules | 1 | 1 | The single root module (`.`), including v0.3 compatibility paths. The VS Code extension is an npm package, not a Go module. |

A change to a budget requires an explicit product-surface review. Test fixtures
belong under `tests/fixtures`, never under `examples`.
