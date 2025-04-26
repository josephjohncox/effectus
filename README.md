# Effectus Rules Engine

Effectus is a strongly-typed, mathematically-sound rules engine with two dialects:

- **List Rules** (.eff) - Simple, parallelizable rule sets for most use cases
- **Flow Rules** (.effx) - More powerful, sequential rules with variable binding

## Core Principles

1. **One syntax, one AST** — shared front-end code for both dialects
2. **Two back-ends** — `list` (ordered slice of `Effect`) and `flow` (free-monad `Program[A]`)
3. **Hard wall** — rule files compile to exactly one IR; no runtime branching
4. **Opt-in mix-ins** — common snippets can be included in either dialect

## Theoretical Foundation

Effectus is built on solid mathematical foundations:
- List rules are based on **free monoids**
- Flow rules are based on **free monads**
- See the [docs/theory](docs/theory) directory for details

## Project Structure

```
effectus-go/
├── ast/            ← combined grammar (rule | flow)
├── list/           ← list rule compiler
├── flow/           ← flow rule compiler
├── schema/         ← protobuf descriptors
└── cmd/            ← CLI tools
    ├── effectusc/  ← compiler
    └── effectusd/  ← daemon
```

## Example Rules

### List Rule (.eff)

```
rule "BASIC_QUALITY_CHECK" priority 10 {
    when {
        product.quality < 95
        product.type == "critical"
    }
    then {
        Reject("Quality below threshold for critical part")
        LogWarning("Quality check failed", { "quality": product.quality })
    }
}
```

### Flow Rule (.effx)

```
flow "MATERIAL_RESERVATION" priority 5 {
    when {
        order.status == "new"
        order.type == "standard"
    }
    steps {
        reserveResult = ReserveMaterial(order.items) -> result
        
        // Use the result from previous step
        if (result.success) {
            UpdateOrderStatus(order.id, "materials_reserved")
        } else {
            Reject("Cannot reserve materials", { "reason": result.reason })
        }
    }
}
```

## CLI Usage

```bash
# Compile a list rule
effectusc list path/to/rule.eff

# Compile a flow rule
effectusc flow path/to/rule.effx

# Lint rules
effectusc lint path/to/rules/

# Run rules against facts
effectusc run --mode=list path/to/rule.eff < facts.json
effectusc run --mode=flow path/to/rule.effx < facts.json
```

## Getting Started

1. Clone the repository
2. Build the CLI tool: `go build -o effectusc ./cmd/effectusc`
3. Create your first rule file
4. Compile it: `./effectusc list your_rule.eff`

## Documentation

- [Basic Theory](docs/theory/basic.md) - Core concepts and type-theoretic foundation
- [Appendix](docs/theory/appendix.md) - Formal mathematical proofs
- [Simplified Appendix](docs/theory/appendix_simple.md) - More accessible explanation

# Effectus — Typed, Deterministic Rule Engine  

*Production-grade effects without production-size headaches*

---

## ✨ What is it?  

Effectus lets you write **small, readable rule files** that turn live data (Facts) into **safe, idempotent side-effects** (Effects).

| ✅ | Feature |
|----|---------|
| | Strongly-typed Facts & Verbs (Protobuf) |
| | Two DSLs – `*.eff` (simple lists) • `*.effx` (data-flow) |
| | Temporal guards – `within 5m`, `since "2025-05-01"` |
| | Capability × Property grid → auto-locking & idempotency |
| | Plug-in verbs (Go / Rust / WASM) |
| | Hot-reload bundles from **OCI**, **ConfigMap**, **Postgres** or **FS** |
| | Saga rollback with inverse verbs |
| | Multi-tenant isolation & PII redaction |
| | CLI, VS-Code extension, WASM linter, Prometheus metrics |

---

## 🗺️ Repo layout

```
effectus/              ← core engine + CLI
factory-rules/         ← your domain repo
├─ proto/              ← Facts & extra Verb rows
├─ rules/              ← .eff / .effx files
├─ tests/              ← golden fixtures
├─ effectus.yaml       ← feature toggles (temporal, saga, …)
└─ Makefile            ← buf · lint · test · bundle
```

---

## 🚀 Quick start

```bash
# 1. add a fact schema
protoc --buf_out=. proto/facts/task/v1/task.proto

# 2. write a rule
cat > rules/task/late.eff <<'EOF'
rule "Late Task" {
  when { task.slack_min < 0 within 10m }
  then { escalate_late_job task_id:$task.id }
}
EOF

# 3. lint & compile
effectusc list rules/**/*.eff -o build/task.elist.json

# 4. run locally
cat examples/fact.json | effectus run --spec build/task.elist.json
```

---

## 🔌 Extending

I want to… | Do this
--- | ---
Add fact field | edit `proto/facts/*.proto` → `buf generate` → `effectusc lint`
Add new verb | add row in `proto/verbs/*.proto` → implement handler (Go / Rust / WASM)
Write rule | create `.eff` / `.effx` file, PR; CI lints & tests
Ship bundle | `effectus-bundle push ghcr.io/acme/task:v1.2`
Pull bundle | `set loader yaml kind=oci & ref=ghcr.io/acme`


---

## 📈 Project timeline

| Milestone | Target tag | Deliverables |
|-----------|------------|--------------|
| M-0 Core lists | v0.1 | .eff parser, List engine, Go CLI |
| M-1 Flows | v0.2 | .effx with -> binds |
| M-2 Temporal mix-in | v0.3 | within / since / not before |
| M-3 Runtime operator | v0.5 | Redis locks, idempotency, saga |
| M-4 Adapters | v0.6 | OCI + ConfigMap + Postgres + FS |
| M-5 Observability | v0.7 | Prometheus metrics, struct logs |
| M-6 Multi-tenant + PII | v0.8 | tenant_id, redaction masks |
| M-7 WASM & UI | v1.0 | WASM linter, VS-Code ext, React UI |

(Every minor bump is backwards-compatible for old rules.)

---

## 📨 Need help?
	•	Slack #effectus-help
	•	Docs https://docs.effectus.io
	•	Issues https://github.com/effectus/effectus

## New Named Parameter Syntax

Effectus now supports a cleaner step syntax with named parameters and variable references:

```effx
flow "STANDARD MILL" priority 5 {
    when {
        customer.code   == "ABC"
        part.tolerance  <= 0.0005
    }

    steps {
        reserve_material qty:1 lot:"A1"                -> mat
        allocate_machine group:"5AXIS" input:$mat      -> m
        set_cut_params   machine:$m sfm:7000           -> p
        generate_setup_sheet machine:$m params:$p      -> sheet
        require_cert     type:"FAI"  doc:$sheet   
        release_job      sheet:$sheet               -> job
        schedule_inspection job:$job stage:"IP"
    }
}
```

## Overview

Effectus is a declarative rule engine...

