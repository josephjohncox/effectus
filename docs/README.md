# Effectus Documentation

Welcome to the Effectus documentation. Effectus is a sophisticated rule engine built on category theory that combines mathematical rigor with practical business rule execution.

## Core Concepts

- [Basic Concepts](BASICS.md) - Introduction to the fundamental concepts of Effectus
- [Architecture](ARCHITECTURE.md) - Overview of the system architecture and components
- [Theory and Mathematical Foundations](theory/basic.md) - Deeper exploration of the category theory underpinnings

## System Components

- [Bundle System](BUNDLE_SYSTEM.md) - Packaging and distribution of rules, schemas, and verbs
- [Verb System](VERB_SYSTEM.md) - Implementing and managing effects in Effectus
- [Schema System](fact_path_improvements.md) - Fact path resolution and type checking

## Reference

- [Commands](COMMANDS.md) - CLI commands for effectusc and effectusd
- [File Reference](FILES.md) - File formats and organization

## Practical Applications

- [Use Cases](USE_CASES.md) - Industry-specific applications and technical domains
- [Advanced Examples](theory/appendix.md) - Appendix with detailed examples
- [Simple Examples](theory/appendix_simple.md) - Simplified examples for beginners

## Development Workflow

The typical workflow for developing and deploying rules with Effectus:

1. Define schemas for your fact types
2. Create verb specifications for available operations
3. Write rules in .eff (list) or .effx (flow) files
4. Type check and validate rules with effectusc
5. Bundle and distribute your rules
6. Deploy and execute with effectusd

## Quick Start

```bash
# Install Effectus
go install github.com/effectus/effectus-go/cmd/effectusc@latest
go install github.com/effectus/effectus-go/cmd/effectusd@latest

# Create a simple rule file
echo 'rule hello {
  when {
    user.name == "world"
  }
  then {
    log(message: "Hello, world!")
  }
}' > hello.eff

# Type check the rule
effectusc typecheck hello.eff

# Create a bundle
effectusc bundle --name hello-bundle --version 1.0.0 --rules-dir ./ --output bundle.json

# Run the bundle
effectusd --bundle bundle.json
```

## Further Reading

- [Category Theory for Programmers](https://bartoszmilewski.com/2014/10/28/category-theory-for-programmers-the-preface/) - Background on category theory
- [Free Monads](https://serokell.io/blog/introduction-to-free-monads) - Understanding the theoretical foundations of flow rules 