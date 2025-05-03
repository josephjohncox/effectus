# Effectus Architecture

Effectus is a sophisticated rule engine that combines the power of category theory and functional programming with the practicality of business rules. This document outlines the overall architecture of Effectus.

## Core Concepts

Effectus is built on three fundamental mathematical structures:

1. **Initial Algebras (Lists)**: Sequential rule execution as a free monoid
2. **Free Monads (Flows)**: Composable, effectful computations with branching
3. **Category Theory**: Providing a unified foundation for both

## System Components

The Effectus system consists of these main components:

```
┌─────────────────────────────────────────────────────────────────┐
│                         Effectus System                          │
├───────────┬───────────┬────────────┬───────────┬────────────────┤
│           │           │            │           │                │
│  Schema   │   Verb    │   Rules    │   Flow    │    Unified     │
│  System   │  System   │  (Lists)   │  System   │    Bundle      │
│           │           │            │           │    System      │
└───────────┴───────────┴────────────┴───────────┴────────────────┘
       │           │           │           │            │
       └───────────┴───────────┴───────────┴────────────┘
                              │
                     ┌────────┴─────────┐
                     │                  │
           ┌─────────┴──────┐   ┌───────┴───────┐
           │                │   │               │
           │ CLI Tools      │   │ Runtime       │
           │ (effectusc)    │   │ (effectusd)   │
           │                │   │               │
           └────────────────┘   └───────────────┘
```

### Schema System

The Schema System manages type definitions and validation for facts. It includes:

- **TypeSystem**: Core type definitions and type checking
- **SchemaRegistry**: Loads and manages schemas from multiple sources
- **PathResolvers**: Handles resolution of paths within fact structures
- **Validators**: Validates facts against schemas

### Verb System

The Verb System defines operations that can be performed by rules:

- **VerbRegistry**: Manages verb specifications and implementations
- **VerbSpecs**: Defines verb signatures (arguments, return types, capabilities)
- **VerbExecutors**: Implements verb functionality
- **PluginSystem**: Loads verbs from dynamic plugins

### Rule System (Lists)

The List Rule System implements rules as a free monoid (initial algebra):

- **Parser**: Parses rule definitions into AST
- **TypeChecker**: Validates rule semantics and types
- **Compiler**: Compiles rules into executable form
- **Interpreter/Executor**: Executes rules against facts

### Flow System

The Flow System implements composable flows as a free monad:

- **Parser**: Parses flow definitions into AST
- **TypeChecker**: Validates flow semantics and types
- **Compiler**: Compiles flows into executable form
- **Interpreter/Executor**: Executes flows with effects and control flow

### Unified Bundle System

The Bundle System packages all components for distribution:

- **BundleBuilder**: Creates bundles from schemas, verbs, and rules
- **OCIIntegration**: Pushes/pulls bundles to/from OCI registries
- **Bundle**: Self-contained package of schemas, verbs, and rules

## Data Flow

```
┌──────────┐    ┌──────────┐    ┌──────────┐    ┌─────────┐    ┌──────────┐
│          │    │          │    │          │    │         │    │          │
│  Schema  │───▶│  Parser  │───▶│   AST    │───▶│  Type   │───▶│ Compiler │
│  Files   │    │          │    │          │    │ Checker │    │          │
│          │    │          │    │          │    │         │    │          │
└──────────┘    └──────────┘    └──────────┘    └─────────┘    └────┬─────┘
                                                                     │
                                                                     ▼
┌──────────┐                                                    ┌──────────┐
│          │                                                    │          │
│  Facts   │───────────────────────────────────────────────────▶│ Executor │
│          │                                                    │          │
└──────────┘                                                    └────┬─────┘
                                                                     │
                                                                     ▼
                                                                ┌──────────┐
                                                                │          │
                                                                │ Effects  │
                                                                │          │
                                                                └──────────┘
```

## Mathematical Foundations

### List Rules as Initial Algebras

List rules can be understood as an initial algebra for the functor `F X = 1 + E × X`, where:
- `1` represents termination
- `E` represents effects
- `×` represents sequencing
- `+` represents choice

In this representation, a rule is a sequence of effects that can optionally terminate at any point.

### Flow Rules as Free Monads

Flow rules are implemented as a free monad, which allows for branching, looping, and other control flow constructs. The free monad for our effect functor `F` is:

```
data Free F A = Pure A | Impure (F (Free F A))
```

This structure allows us to compose effectful computations while maintaining purity at the language level.

## Implementation Details

### Compiler

The compiler transforms rule and flow definitions into executable forms. For list rules, it produces a sequence of effects to be executed. For flow rules, it produces a tree of effects and control flow.

### Executor

The executor evaluates compiled rules against facts. For list rules, this is a simple iteration over the effects. For flow rules, it's a more complex evaluation of the free monad structure.

### Type System

The type system validates rules against schemas, ensuring that:
- Fact paths are valid according to the schema
- Operations on facts are type-compatible
- Verb arguments and return types are correct

### Bundle System

The bundle system packages all the components needed to run rules:
- Schema definitions
- Verb specifications and implementations
- Compiled rules and flows

Bundles can be distributed via OCI registries, allowing for versioning and distribution.

## Runtime Components

### effectusc (CLI)

The `effectusc` command-line tool provides utilities for working with Effectus:
- Parsing and type-checking rules and flows
- Compiling rules and flows
- Building bundles
- Testing rules against sample facts
- Linting rules for best practices

### effectusd (Runtime)

The `effectusd` runtime executes bundled rules against facts:
- Loads bundles from local files or OCI registries
- Receives facts from various sources (HTTP, Kafka, etc.)
- Executes rules and produces effects
- Handles saga compensation for failed executions
- Provides observability via metrics and logs

## Conclusion

Effectus combines the rigor of category theory with practical business rule execution. Its architecture separates concerns while maintaining a unified mathematical foundation, providing both formal correctness and operational flexibility. 