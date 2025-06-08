# Technical Design Document: Effectus

## 1. Overview

Effectus is a strongly-typed rule engine built in Go, designed to support mission-critical business systems that require high reliability and deterministic behavior. By modeling rules with formal semantics and strong typing, it provides deterministic execution, compile-time verification, and extensibility for a wide range of domains including workflow orchestration, business process automation, and event-driven systems.

Unlike traditional programming approaches that intermix business logic with implementation details, Effectus separates rule definition from execution, enabling business experts and engineers to collaborate effectively while maintaining guarantees of correctness through strong typing and capability-based execution.

Effectus is designed with language interoperability in mind, using Protocol Buffers for schema definitions and cross-language compatibility. While the core engine is implemented in Go, the architecture supports multi-language clients and will include a native Rust implementation in the future. This language-agnostic approach makes Effectus ideal for heterogeneous environments with diverse technology stacks.

## 2. Background & Motivation

Modern business systems require reliable control logic that can:
- Process complex event streams from multiple sources
- Apply business logic consistently across distributed systems
- Guarantee deterministic outcomes with transactional integrity
- Adapt to changing requirements while maintaining type safety

Traditional rule engines lack the formal rigor, type safety, and performance characteristics needed for critical operational environments. Similarly, conventional coding approaches suffer from:
- Logic Dispersion: Critical business rules scattered across services and codebases
- Domain Translation Loss: Key business concepts get lost in implementation details
- Verification Gaps: Difficult to prove rule correctness without formal models
- Maintenance Complexity: Business changes require developer intervention, creating bottlenecks

Effectus addresses these limitations through strong typing and capability-based execution, providing a domain-specific language that business experts can understand while maintaining the rigor software engineers require.

## 2.1 Why Effectus Over Traditional Code

### 2.1.1 Business-Domain Alignment
Traditional code: Requires translation of business concepts into programming constructs, leading to semantic gaps and misinterpretation.
Effectus: Provides a domain-specific language that directly models business concepts, allowing domain experts to verify rule correctness without understanding implementation details.

### 2.1.2 Static Verification
Traditional code: Testing can only verify specific input-output pairs, leaving edge cases undiscovered.
Effectus: Rules checked at compile-time with strong type constraints, ensuring correctness properties like type safety, capability checks, and resource access controls before deployment.

### 2.1.3 Adaptability to Change
Traditional code: Business logic changes require developer time, code changes, testing, and deployment cycles.
Effectus: Rules are data, not code, enabling hot-reloading, versioning, and modification without recompilation or redeployment.

### 2.1.4 Auditability and Traceability
Traditional code: Reasoning about why a decision was made requires debugging through code execution.
Effectus: Every rule execution is traceable, recording which facts triggered which rules and effects, enabling perfect audit trails and replay capabilities.

### 2.1.5 Separation of Concerns
Traditional code: Business logic often becomes entangled with infrastructure concerns.
Effectus: Clean separation between rule definition (what should happen), fact ingestion (when it should happen), and effect execution (how it should happen).

## 2.2 Comparison with Other Rule Engines

When evaluating rule engines for business environments, several alternatives to Effectus exist, each with different strengths and limitations. This section compares Effectus with three prominent competitors: Microsoft Rules Engine, GoRules, and Drools.

### 2.2.1 Microsoft Rules Engine (JSON-based)

Microsoft's JSON-based Rules Engine is designed for general business applications with an emphasis on accessibility and integration with Microsoft ecosystems.

**Key Differences:**
- **Formal Foundations:** Microsoft's engine uses a JSON declarative approach without the mathematical rigor of Effectus's algebraic foundations.
- **Type Safety:** Limited compile-time type checking compared to Effectus's strong typing throughout the pipeline.
- **Performance:** Not optimized for the high-throughput, real-time requirements of critical operational systems.

While Microsoft's engine offers a low barrier to entry with its JSON-based approach, it lacks the formal verification capabilities and performance characteristics necessary for mission-critical systems.

### 2.2.2 GoRules

GoRules is a lightweight, Go-based rule engine focused on simplicity and integration within Go applications.

**Key Differences:**
- **Expressiveness:** GoRules offers basic conditional logic, while Effectus provides comprehensive support for complex event patterns, advanced rule composition, and type-safe operations.
- **Type Safety & Correctness:** GoRules uses simple conditional evaluation with limited type checking, whereas Effectus enforces strong typing throughout the rule definition and execution pipeline, preventing entire classes of runtime errors.
- **Scalability:** GoRules is designed for smaller-scale applications, while Effectus's architecture supports distributed execution and high-volume processing.

GoRules is suitable for simple rule evaluation in Go applications but doesn't address the complex relationships, strict type safety, and formal verification requirements of sophisticated business systems.

### 2.2.3 Drools

Drools is a mature, Java-based business rules management system with broad adoption in enterprise environments.

**Key Differences:**
- **Implementation Language:** Java-based vs. Effectus's Go implementation, which offers better performance characteristics for modern applications.
- **Learning Curve:** Drools has a steep learning curve and requires significant Java expertise, whereas Effectus is designed for clarity and accessibility.
- **Lightweight vs. Heavyweight:** Drools is a full-featured but heavyweight solution, while Effectus provides comparable power with a more focused, efficient implementation.
- **Rule Expression:** Effectus offers a more intuitive and direct expression of business rules than Drools, with stronger type checking at compile time.

While Drools offers a comprehensive feature set, its complexity, Java dependency, and performance characteristics make it less suitable for organizations seeking a nimble, efficient rule engine.

### 2.2.4 Why Effectus for Critical Business Systems

Effectus stands out as the superior choice for critical business systems for several reasons:

1. **Domain-Agnostic Design:** Effectus was built to support a wide range of business domains, from financial services to supply chain management to healthcare.

2. **Strong Type Safety with Practical Usability:** Effectus provides compile-time verification of rule correctness through strong typing, maintaining an approachable interface for both business and technical users while preventing runtime errors.

3. **Performance Characteristics:** Go-based implementation and efficient execution models make Effectus particularly well-suited for real-time decision making in critical operational environments.

4. **Integration Capabilities:** Native support for distributed fact sources and effect sinks aligns perfectly with modern distributed system architectures.

5. **Extensible Architecture:** Designed to grow with changing business needs, with a roadmap that includes advanced features for complex business process management.

6. **Audit and Compliance:** Superior traceability and explainability compared to alternatives, essential for regulated industries with strict compliance requirements.

Effectus combines the technical rigor needed for mission-critical systems with the usability, performance, and domain-agnostic features that make it uniquely suited to power business operations as a unified decision-making platform.

## 3. Design Principles

### 3.1 Type-Driven Development

All components are explicitly typed, enabling:
- Compile-time verification of rule correctness
- Static analysis for conflict detection
- Prevention of runtime type errors

### 3.2 Capability-Based Protection

Rules operate under a capability model that governs:
- What resources they can access (Read, Modify, Create, Delete)
- How resources are protected during concurrent execution
- Which operations can be composed safely together
- Static verification of permission boundaries at compile time

### 3.3 Deterministic Execution

All rule executions are:
- Deterministic with clear operational semantics
- Traceable with execution logs
- Replayable for debugging and verification
- Transactional with saga patterns for failure recovery and compensation

## 4. Architecture

### 4.1 System Components

```
┌────────────────┐    ┌─────────────────┐    ┌────────────────┐
│  Fact Sources  │───►│   Rule Engine   │───►│  Effect Sinks  │
└────────────────┘    └─────────────────┘    └────────────────┘
       ▲                      │                      ▲
       │                      ▼                      │
       │               ┌─────────────┐               │
       └───────────────┤ Schema Reg. ├───────────────┘
                       └─────────────┘
```

### 4.2 Data Flow

1. **Fact Ingestion**: Typed facts enter through adapters
2. **Rule Evaluation**: Predicates evaluate against facts
3. **Effect Execution**: Triggered effects execute with transactional guarantees
4. **Observability**: Metrics, traces, and logs capture execution details

### 4.3 Core Components

- **Fact System**: Schema registry, adapters, versioning
- **Predicate Engine**: Boolean expression evaluation
- **Effect System**: Typed verbs with capability protection
- **Execution Engines**: List and Flow interpreters
- **Compensation System**: Saga-based error recovery

### 4.4 Deployment Models

Effectus is designed for flexible deployment options:

- **Library Mode**: Embed directly into Go applications for in-process rule execution
- **Sidecar Mode**: Run as a separate process alongside applications, communicating via gRPC
- **Standalone Service**: Deploy as a central rule execution service for multiple applications
- **Multi-Language Integration**: Connect to non-Go services via Protocol Buffer interfaces

This flexibility allows Effectus to integrate with existing systems regardless of their implementation language or architecture, making it ideal for both greenfield projects and incremental adoption in brownfield environments.

## 5. Components in Detail

### 5.1 Fact System

Facts are strongly-typed messages defined via Protocol Buffers:

```protobuf
message MachineStatus {
  string machine_id = 1;
  double utilization = 2;
  string state = 3;
}
```

Facts are:
- Schema-versioned with compatibility verification
- Immutable after creation
- Queryable through a unified API

### 5.2 Predicate System

Predicates are boolean expressions evaluating facts:

```go
type Predicate struct {
    Op        PredicateOp
    FactType  string
    Path      string
    Value     interface{}
    Left      *Predicate
    Right     *Predicate
}
```

### 5.3 Effect System

Effects (verbs) are typed actions protected by capabilities:

```go
type Effect struct {
    VerbID     uint32
    Capability string
    Key        string
    Payload    proto.Message
}
```

Effects are:
- Capability-protected for access control and concurrency
- Versioned via schema registry
- Locked by capability and key to prevent conflicts
- Idempotent where possible for reliability
- Compensatable for error recovery

Importantly, Effectus doesn't directly execute the business logic of effects. Instead, it:
1. Determines which effects should be executed (rule evaluation)
2. Manages the execution order and transactional boundaries
3. Dispatches effects to external interpreters for actual execution
4. Handles compensation in case of failures

This separation of concerns allows Effectus to focus on rule evaluation and execution flow, while domain-specific implementation details remain in specialized modules.

### 5.4 Capability System

Effectus implements a rigorous capability model for effects:

```go
const (
    CapabilityNone Capability = iota
    CapabilityRead
    CapabilityModify
    CapabilityCreate
    CapabilityDelete
)
```

The capability model provides:
- **Resource Protection**: Critical resources are protected by requiring specific capabilities
- **Concurrency Control**: Capabilities determine lock acquisition strategy
- **Authorization Modeling**: Business permissions map to capability requirements
- **Static Verification**: Capabilities are checked at compile time before execution

Capabilities form a lattice where higher capabilities include lower ones:
```
Read ≤ Modify ≤ Create ≤ Delete
```

### 5.5 Execution Engines

#### 5.5.1 List Engine

Executes effects sequentially:
- Acquires locks on (capability, key) pairs
- Executes effects in order
- Releases locks deterministically
- Maintains execution logs
- Performs compensation on failure

#### 5.5.2 Flow Engine

Implements complex control flow:

```go
type Prog[A any] struct {
    Pure  A
    Bind  func(A) Prog[A]
    Effect Effect
}
```

Enables:
- Branching and conditional execution
- Composition of sub-programs

### 5.6 Compensation System

Handles failures through the saga pattern:
- Logs each effect before execution
- On failure, executes compensating actions in reverse order
- Ensures exactly-once semantics through idempotent operations
- Maintains transactional integrity across distributed systems

### 5.7 Verb Extensibility

A key design principle of Effectus is that verbs (effects) are extensible through a simple, pluggable architecture:

```go
// VerbExecutor is the interface for executing verbs
type VerbExecutor interface {
    Execute(ctx context.Context, effect Effect) (proto.Message, error)
    Compensate(ctx context.Context, effect Effect, result proto.Message) error
}

// VerbRegistry manages available verb executors
type VerbRegistry struct {
    executors map[uint32]VerbExecutor
}
```

This architecture provides several benefits:

1. **Simple Go Modules**: Each verb interpreter is a straightforward Go module implementing the `VerbExecutor` interface
2. **Decoupled Execution**: The actual execution logic lives outside Effectus in domain-specific modules
3. **Easy Extension**: Adding new verbs requires only implementing a small interface
4. **Flexible Integration**: Verbs can connect to any external system (databases, APIs, messaging systems)
5. **Testing Simplicity**: Mock executors can be easily created for testing rules without real backends

For example, a verb executor for sending an alert might be implemented as:

```go
type AlertExecutor struct {
    client alertapi.Client
}

func (e *AlertExecutor) Execute(ctx context.Context, effect Effect) (proto.Message, error) {
    payload := effect.Payload.(*AlertPayload)
    alertID, err := e.client.SendAlert(ctx, payload.Severity, payload.Message)
    return &AlertResult{AlertID: alertID}, err
}

func (e *AlertExecutor) Compensate(ctx context.Context, effect Effect, result proto.Message) error {
    alertResult := result.(*AlertResult)
    return e.client.CancelAlert(ctx, alertResult.AlertID)
}
```

This clean separation between rule definition and execution allows domain experts to focus on business logic while system integrators handle the implementation details of connecting to external systems.

## 6. Mathematical Foundations and Formal Semantics

### 6.1 Denotational Semantics

Effects are interpreted in the mathematical **Set** category, giving them precise meaning:

| Term | Meaning in Set theory |
|------|------------|
| `Facts` | Product set $\prod_{i} \llbracket\tau_i\rrbracket$ |
| `Effect` | Coproduct $\sum_{i}(\text{Verb}_i \times \llbracket\text{Payload}_i\rrbracket)$ |
| `Program A` | Free monad $T^E(A) \cong \mu X. \, A + (\text{Effect} \times X)$ |
| `SpecList` | Function $\llbracket\text{Facts}\rrbracket \rightarrow \text{List}(\text{Effect})$ |
| `SpecFlow` | Function $\llbracket\text{Facts}\rrbracket \rightarrow T^E(\text{Unit})$ |

### 6.2 Categorical Properties

- `List(Effect)` is the **free monoid** on `Effect`
- `Program` is the **free monad** on the endofunctor $F(X) = \text{Effect} \times X$
- List rules can be embedded into flow rules via the canonical monoid-to-monad conversion

### 6.3 Type Soundness

The type system ensures:
- **Progress**: Well-typed terms either complete or can take another step
- **Preservation**: Types are maintained throughout execution
- **Termination**: All executions are guaranteed to terminate

## 7. Domain Examples

Effectus is designed to be domain-agnostic. Detailed examples for various industries and use cases are available in the `examples/` directory, including:

- Manufacturing automation
- Financial services
- Supply chain management
- Healthcare workflows
- E-commerce processing
- Customer service automation

These examples demonstrate the practical application of Effectus's mathematical foundations across different business domains.

## 8. Implementation Considerations

### 8.1 Distributed Execution

- Distributed locking with fencing tokens
- Lamport clock synchronization
- Saga pattern for distributed transactions and compensation

### 8.2 Compensation Strategies

- **Logging**: Each effect is logged before execution
- **Inverse Operations**: Each verb defines its compensating action
- **Reverse Execution**: On failure, compensations are executed in reverse order
- **Idempotency**: Compensation actions are designed to be idempotent
- **Monitoring**: Failed transactions and compensations are monitored for manual intervention

### 8.3 Capability Enforcement

- **Static Checking**: Capability requirements verified at compile time
- **Runtime Verification**: Double-checked during execution
- **Resource Locking**: Capability-aware locking prevents conflicts
- **Auditability**: Capability usage is logged for compliance
- **Permission Mapping**: Business roles map to capability sets

### 8.4 Observability

- Prometheus metrics for rule execution
- OpenTelemetry tracing
- PII redaction for sensitive data
- Structured logging
- Compensation event monitoring

### 8.5 Development Tools

- CLI for rule authoring and testing
- VS Code integration with WASM-based linter
- TinyGo compilation for browser-based editing

### 8.6 Language Interoperability

Effectus achieves multi-language compatibility through:

- **Protocol Buffers**: All schemas, facts, and effects are defined in language-neutral .proto files
- **gRPC Interfaces**: Core services expose gRPC APIs consumable from any language
- **Cross-Language SDKs**: Client libraries for Go, Python, TypeScript, and (future) Rust
- **Schema Registry**: Central repository ensures consistent types across language boundaries
- **Language-Specific Adapters**: Optimized integrations for common frameworks and languages

This approach enables teams to use Effectus regardless of their preferred development language, while maintaining the strong typing guarantees across language boundaries.

## 9. Future Directions

### 9.1 Advanced Features

- **Multi-tenant Isolation**: Namespace separation for enterprise deployments
- **A/B Testing**: Gradual rule deployment and testing capabilities  
- **Machine Learning Integration**: Predictive rule optimization and dynamic tuning
- **Formal Verification**: Mathematical correctness proofs for critical business logic
- **Advanced Analytics**: Deep insights into rule performance and execution patterns

### 9.2 Language and Platform Support

- **Rust Implementation**: High-performance runtime for resource-constrained environments
- **Python Bindings**: Data science and machine learning integration
- **JavaScript/WASM**: Browser-based rule editing and execution
- **Kubernetes Operators**: Native cloud-native deployment and management
- **Edge Computing**: Lightweight runtime for IoT and edge devices

### 9.3 Enterprise Features  

- **Distributed Execution**: Cross-region rule execution with consistency guarantees
- **Advanced Security**: Enhanced RBAC, audit trails, and compliance reporting
- **Performance Optimization**: Auto-scaling, caching, and intelligent resource allocation
- **Integration Ecosystem**: Native connectors for major enterprise systems and databases

### 9.4 Developer Experience

- **IDE Integration**: Enhanced VS Code extension with IntelliSense and debugging
- **Testing Framework**: Comprehensive unit and integration testing for rules
- **CI/CD Pipeline**: Automated rule validation, testing, and deployment
- **Observability Suite**: Advanced monitoring, alerting, and performance analytics

## 10. Implementation Roadmap

### Phase 1: Core Foundation
- ✅ Core execution engine and type system
- ✅ Extension loading and compilation system  
- ✅ Basic CLI tools and runtime daemon
- ✅ Coherent flow architecture

### Phase 2: Production Readiness
- 🚧 OCI bundle distribution system
- 🚧 Advanced execution policies and saga compensation
- 📋 Production monitoring and observability
- 📋 Performance optimization and caching

### Phase 3: Enterprise Features
- 📋 Multi-tenant isolation and security
- 📋 Advanced IDE integration and tooling
- 📋 Comprehensive testing framework
- 📋 Cloud-native deployment options

### Phase 4: Advanced Capabilities
- 📋 Machine learning integration
- 📋 Formal verification tools
- 📋 Multi-language runtimes
- 📋 Edge computing support

## 11. Conclusion

Effectus provides a mathematically sound, strongly-typed rule engine that ensures reliability through compile-time verification, capability-based protection, and compensation-driven recovery. It delivers the correctness guarantees needed for mission-critical systems while maintaining the flexibility to adapt to changing business requirements across diverse domains.

The system's coherent flow architecture, from extension loading through compilation to execution, ensures that all errors are caught before runtime, providing the deterministic behavior and safety guarantees essential for production systems.
