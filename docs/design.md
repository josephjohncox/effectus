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

## 6. Applications in Manufacturing

### 6.1 Factory Execution System

**Example**: Machine Anomaly Detection
```
flow "MachineAnomaly" priority 20 {
  when {
    machine.vibration > machine.baseline * 0.8 &&
    machine.status == "running" &&
    within(5m)
  }

  steps {
    // Log the anomaly event
    anomalyData = recordAnomaly(machineId: machine.id, metric: "vibration", value: machine.vibration)
    
    // Generate alert
    alert = issueAlert(severity: "warning", source: machine.id, message: "High vibration detected")
    
    // Adjust machine parameters
    adjustResult = adjustMachineSettings(machineId: machine.id, 
                                         parameter: "speed", 
                                         value: machine.current_speed * 0.8,
                                         reason: $anomalyData)
    
    // Notify maintenance team
    notifyMaintenance(team: "preventive", 
                      machineId: machine.id, 
                      alertId: $alert,
                      adjustmentResult: $adjustResult)
  }
}
```

### 6.2 Inventory

```
flow "LowStockReplenishment" priority 15 {
  when {
    inventory.quantity < inventory.reorder_point &&
    inventory.on_order == false &&
    inventory.item_type == "raw_material"
  }

  steps {
    // Check supplier availability
    supplierCheck = checkSupplierAvailability(materialId: inventory.material_id, 
                                              quantity: inventory.reorder_quantity)
    
    // Create purchase requisition
    requisition = createPurchaseRequisition(materialId: inventory.material_id,
                                            quantity: inventory.reorder_quantity,
                                            warehouseId: inventory.warehouse_id,
                                            supplierInfo: $supplierCheck)
    
    // Update inventory status
    updateInventoryStatus(inventoryId: inventory.id, 
                          field: "on_order", 
                          value: true)
    
    // Notify purchasing department
    notifyPurchasing(type: "reorder", 
                     urgency: inventory.quantity < inventory.safety_stock ? "high" : "normal",
                     requisitionId: $requisition)
    
    // Log activity
    logActivity(action: "inventory_reorder_initiated", details: $requisition)
  }
}
```

### 6.3 DFM
```
flow "DFMReview" priority 25 {
  when {
    part.status == "design_submitted" &&
    part.complexity_score > 7.5 &&
    !part.dfm_reviewed
  }

  steps {
    // Analyze manufacturability
    dfmAnalysis = analyzeDFM(partId: part.id, 
                             geometry: part.geometry, 
                             material: part.material)
    
    // Identify potential issues
    issues = identifyManufacturingIssues(partId: part.id, 
                                         analysis: $dfmAnalysis, 
                                         machineCapabilities: factory.machine_capabilities)
    
    // Generate recommendations
    recommendations = generateDFMRecommendations(partId: part.id,
                                                issues: $issues,
                                                targetCycleTime: part.target_cycle_time)
    
    // Create engineering change request if needed
    ecr = createEngineeringChangeRequest(partId: part.id,
                                        issues: $issues,
                                        recommendations: $recommendations,
                                        priority: $issues.severity)
    
    // Notify engineering team
    notifyEngineering(type: "dfm_review", 
                    partId: part.id, 
                    ecrId: $ecr)
    
    // Update part status
    updatePartStatus(partId: part.id, 
                     field: "dfm_reviewed", 
                     value: true,
                     reviewResults: $dfmAnalysis)
  }
}
```

### 6.4 Quoting
```
flow "CustomerQuoteGeneration" priority 30 {
  when {
    request.type == "quote_request" &&
    request.status == "received" &&
    customer.account_status == "active"
  }

  steps {
    // Analyze part complexity
    complexity = analyzePartComplexity(geometry: request.part_geometry,
                                      features: request.part_features,
                                      material: request.part_material)
    
    // Check manufacturing capacity
    capacityCheck = checkCapacity(partType: request.part_type,
                                 complexity: $complexity,
                                 quantity: request.quantity,
                                 targetDate: request.delivery_date)
    
    // Calculate cost estimate
    costEstimate = calculateCost(partId: request.part_id,
                               complexity: $complexity,
                               material: request.part_material,
                               quantity: request.quantity,
                               leadTime: $capacityCheck.earliest_delivery_date)
    
    // Apply customer-specific pricing rules
    finalQuote = applyPricingRules(customerId: customer.id,
                                  baseEstimate: $costEstimate,
                                  contractTerms: customer.contract_terms)
    
    // Generate quote document
    quoteDoc = generateQuoteDocument(quoteId: request.id,
                                   customer: customer,
                                   partDetails: request.part_details,
                                   pricing: $finalQuote,
                                   deliveryDate: $capacityCheck.recommended_delivery_date)
    
    // Update request status
    updateRequestStatus(requestId: request.id,
                       status: "quoted",
                       quote: $quoteDoc)
    
    // Notify sales team and customer
    notifySales(type: "quote_generated",
               requestId: request.id,
               quoteId: $quoteDoc.id)
    
    notifyCustomer(customerId: customer.id,
                  notificationType: "quote_ready",
                  quoteDocument: $quoteDoc)
  }
}
```

### 6.5 Quality
```
flow "QualityInspectionTrigger" priority 40 {
  when {
    part.status == "machining_complete" &&
    part.customer_tier == "aerospace" &&
    part.has_critical_dimensions == true &&
    part.requires_cmm == true  // Pre-filter condition instead of conditional branching
  }

  steps {
    // Determine inspection requirements
    inspectionPlan = determineInspectionRequirements(partId: part.id,
                                                    partType: part.type,
                                                    customerTier: part.customer_tier,
                                                    criticalFeatures: part.critical_features)
    
    // Assign to inspection station
    inspectionAssignment = assignInspectionStation(partId: part.id,
                                                 plan: $inspectionPlan,
                                                 availableStations: factory.inspection_stations)
    
    // Create inspection task
    inspectionTask = createInspectionTask(partId: part.id,
                                        stationId: $inspectionAssignment.station_id,
                                        plan: $inspectionPlan,
                                        priority: part.due_date)
    
    // Update part status
    updatePartStatus(partId: part.id,
                    status: "awaiting_inspection",
                    inspectionDetails: $inspectionTask)
    
    // Generate and upload CMM program
    cmmProgram = generateCMMProgram(partId: part.id,
                                   geometry: part.geometry,
                                   criticalDimensions: part.critical_dimensions)
    
    uploadCMMProgram(stationId: $inspectionAssignment.station_id,
                    programId: $cmmProgram.id,
                    taskId: $inspectionTask.id)
    
    // Notify quality team
    notifyQualityTeam(type: "inspection_required",
                     partId: part.id,
                     taskId: $inspectionTask.id,
                     stationId: $inspectionAssignment.station_id)
  }
}

// Create a separate flow for non-CMM inspections
flow "StandardQualityInspection" priority 40 {
  when {
    part.status == "machining_complete" &&
    part.customer_tier == "aerospace" &&
    part.has_critical_dimensions == true &&
    part.requires_cmm == false  // Pre-filter condition
  }

  steps {
    // Determine inspection requirements
    inspectionPlan = determineInspectionRequirements(partId: part.id,
                                                    partType: part.type,
                                                    customerTier: part.customer_tier,
                                                    criticalFeatures: part.critical_features)
    
    // Assign to inspection station
    inspectionAssignment = assignInspectionStation(partId: part.id,
                                                 plan: $inspectionPlan,
                                                 availableStations: factory.inspection_stations)
    
    // Create inspection task
    inspectionTask = createInspectionTask(partId: part.id,
                                        stationId: $inspectionAssignment.station_id,
                                        plan: $inspectionPlan,
                                        priority: part.due_date)
    
    // Update part status
    updatePartStatus(partId: part.id,
                    status: "awaiting_inspection",
                    inspectionDetails: $inspectionTask)
    
    // Notify quality team
    notifyQualityTeam(type: "inspection_required",
                     partId: part.id,
                     taskId: $inspectionTask.id,
                     stationId: $inspectionAssignment.station_id)
  }
}
```

### 6.3 Scheduling and Capacity Management

**Example**: Dynamic Capacity Adjustment
```
flow "DynamicCapacityAdjustment" priority 35 {
  when {
    machine.utilization < 0.7 &&
    machine.state == "running" &&
    since(2h) &&
    factory.shift_status == "active" &&
    machine.reallocation_candidate == true  // Pre-filter condition
  }

  steps {
    // Analyze current production schedule
    scheduleAnalysis = analyzeSchedule(machineId: machine.id,
                                      timeWindow: 24h,
                                      currentUtilization: machine.utilization)
    
    // Find alternative jobs
    alternativeJobs = findSuitableJobs(machineCapabilities: machine.capabilities,
                                      timeAvailable: $scheduleAnalysis.available_time,
                                      priorityThreshold: "high")
    
    // Simulate impact of reallocation
    simulationResults = simulateReallocation(currentSchedule: $scheduleAnalysis.schedule,
                                           proposedJobs: $alternativeJobs,
                                           machineId: machine.id)
    
    // Reallocate capacity 
    reallocation = reallocateCapacity(machineId: machine.id,
                                     jobs: $simulationResults.recommended_jobs,
                                     reason: "underutilization")
    
    // Update affected promise dates
    updatePromiseDates(orderIds: $reallocation.affected_order_ids,
                      newDates: $simulationResults.new_completion_dates)
    
    // Notify production control
    notifyProductionControl(type: "capacity_reallocation",
                           machineId: machine.id,
                           reallocationDetails: $reallocation)
    
    // Update scheduling constraints
    updateSchedulingConstraints(machineId: machine.id,
                               minUtilization: 0.8,
                               timeWindow: 8h)
  }
}

// Separate flow for logging capacity analysis without reallocation
flow "CapacityAnalysisLogging" priority 34 {
  when {
    machine.utilization < 0.7 &&
    machine.state == "running" &&
    since(2h) &&
    factory.shift_status == "active" &&
    machine.reallocation_candidate == false  // Pre-filter condition
  }

  steps {
    // Analyze current production schedule
    scheduleAnalysis = analyzeSchedule(machineId: machine.id,
                                      timeWindow: 24h,
                                      currentUtilization: machine.utilization)
    
    // Find alternative jobs
    alternativeJobs = findSuitableJobs(machineCapabilities: machine.capabilities,
                                      timeAvailable: $scheduleAnalysis.available_time,
                                      priorityThreshold: "high")
    
    // Simulate impact of reallocation
    simulationResults = simulateReallocation(currentSchedule: $scheduleAnalysis.schedule,
                                           proposedJobs: $alternativeJobs,
                                           machineId: machine.id)
    
    // Log the analysis
    logCapacityAnalysis(machineId: machine.id,
                       analysis: $simulationResults,
                       action: "no_change",
                       reason: "machine_not_candidate_for_reallocation")
  }
}
```

## 7. Applications in Other Domains

Effectus's flexibility and domain-agnostic design make it suitable for a wide range of industries beyond manufacturing. This section demonstrates how the same patterns and capabilities apply to other business domains.

### 7.1 Financial Services

**Example**: Fraud Detection
```
flow "SuspiciousTransactionAlert" priority 20 {
  when {
    transaction.amount > customer.average_transaction * 3 &&
    transaction.country != customer.home_country &&
    within(30m)
  }

  steps {
    // Log the suspicious activity
    fraudData = recordSuspiciousActivity(customerId: customer.id, 
                                       transactionId: transaction.id, 
                                       reason: "unusual_amount_and_location")
    
    // Generate alert
    alert = issueAlert(severity: "high", 
                      source: transaction.id, 
                      message: "Potentially fraudulent transaction detected")
    
    // Hold transaction for review
    holdResult = holdTransaction(transactionId: transaction.id, 
                               reason: $fraudData.id,
                               timeout: 4h)
    
    // Notify fraud team
    notifyFraudTeam(priority: "high", 
                   customerId: customer.id, 
                   alertId: $alert.id,
                   holdDetails: $holdResult)
  }
}
```

### 7.2 Supply Chain Management

```
flow "InventoryReplenishment" priority 15 {
  when {
    inventory.quantity < inventory.reorder_point &&
    inventory.on_order == false &&
    inventory.item_type == "active"
  }

  steps {
    // Check supplier availability
    supplierCheck = checkSupplierAvailability(itemId: inventory.item_id, 
                                            quantity: inventory.reorder_quantity)
    
    // Create purchase requisition
    requisition = createPurchaseRequisition(itemId: inventory.item_id,
                                          quantity: inventory.reorder_quantity,
                                          warehouseId: inventory.warehouse_id,
                                          supplierInfo: $supplierCheck)
    
    // Update inventory status
    updateInventoryStatus(inventoryId: inventory.id, 
                        field: "on_order", 
                        value: true)
    
    // Notify purchasing department
    notifyPurchasing(type: "reorder", 
                   urgency: inventory.quantity < inventory.safety_stock ? "high" : "normal",
                   requisitionId: $requisition.id)
    
    // Log activity
    logActivity(action: "inventory_reorder_initiated", details: $requisition)
  }
}
```

### 7.3 Healthcare

```
flow "PatientRiskAssessment" priority 25 {
  when {
    patient.vitals_updated == true &&
    patient.heart_rate > 100 &&
    patient.blood_pressure.systolic > 160 &&
    patient.age > 50
  }

  steps {
    // Calculate risk score
    riskAnalysis = calculateCardiacRisk(patientId: patient.id, 
                                      vitals: patient.vitals, 
                                      history: patient.medical_history)
    
    // Identify potential issues
    issues = identifyRiskFactors(patientId: patient.id, 
                               analysis: $riskAnalysis, 
                               currentMedications: patient.medications)
    
    // Generate recommendations
    recommendations = generateCareRecommendations(patientId: patient.id,
                                                issues: $issues,
                                                carePathways: system.care_pathways)
    
    // Create care plan update if needed
    careUpdate = createCarePlanUpdate(patientId: patient.id,
                                     issues: $issues,
                                     recommendations: $recommendations,
                                     priority: $issues.severity)
      
    // Notify healthcare team
    notifyProviders(type: "risk_assessment", 
                    patientId: patient.id, 
                    careUpdateId: $careUpdate.id)
    
    // Update patient record
    updatePatientRecord(patientId: patient.id, 
                       field: "last_risk_assessment", 
                       value: $riskAnalysis)
  }
}
```

### 7.4 E-Commerce
```
flow "CustomerOrderProcessing" priority 30 {
  when {
    order.status == "received" &&
    order.payment_status == "approved" &&
    customer.account_status == "active"
  }

  steps {
    // Check inventory availability
    inventoryCheck = checkInventoryAvailability(orderItems: order.items,
                                             warehouseId: order.nearest_warehouse)
    
    // Allocate inventory
    allocation = allocateInventory(orderItems: order.items,
                                 warehouseId: $inventoryCheck.recommended_warehouse,
                                 orderPriority: customer.tier)
    
    // Generate shipping label
    shippingLabel = generateShippingLabel(orderId: order.id,
                                       customer: customer.shipping_address,
                                       warehouseId: $allocation.warehouse_id,
                                       carrier: order.shipping_method)
    
    // Update order status
    updateOrderStatus(orderId: order.id,
                    status: "processing",
                    allocationDetails: $allocation)
    
    // Notify warehouse
    notifyWarehouse(type: "new_order",
                   warehouseId: $allocation.warehouse_id,
                   orderId: order.id,
                   shippingLabelId: $shippingLabel.id)
    
    // Send customer notification
    notifyCustomer(customerId: customer.id,
                 notificationType: "order_processing",
                 estimatedDelivery: $allocation.estimated_ship_date)
  }
}
```

### 7.5 Customer Service
```
flow "CustomerSupportEscalation" priority 40 {
  when {
    ticket.status == "open" &&
    ticket.priority == "high" &&
    ticket.response_time > SLA.response_time &&
    ticket.assigned == true
  }

  steps {
    // Determine escalation path
    escalationPlan = determineEscalationPath(ticketId: ticket.id,
                                           ticketType: ticket.type,
                                           customerTier: customer.tier,
                                           currentSLA: ticket.SLA)
    
    // Assign to escalation agent
    escalationAssignment = assignEscalationAgent(ticketId: ticket.id,
                                               plan: $escalationPlan,
                                               availableAgents: department.available_agents)
    
    // Create escalation record
    escalationRecord = createEscalationRecord(ticketId: ticket.id,
                                           agentId: $escalationAssignment.agent_id,
                                           plan: $escalationPlan,
                                           reason: "SLA_breach")
    
    // Update ticket status
    updateTicketStatus(ticketId: ticket.id,
                     status: "escalated",
                     escalationDetails: $escalationRecord)
    
    // Notify support management
    notifySupportManagement(type: "escalation",
                          ticketId: ticket.id,
                          escalationId: $escalationRecord.id,
                          agentId: $escalationAssignment.agent_id)
  }
}
```

### 7.6 Resource Management

**Example**: Dynamic Resource Allocation
```
flow "DynamicResourceScaling" priority 35 {
  when {
    service.utilization > 0.8 &&
    service.state == "online" &&
    since(5m) &&
    system.auto_scale_enabled == true
  }

  steps {
    // Analyze current workload
    workloadAnalysis = analyzeWorkload(serviceId: service.id,
                                     timeWindow: 15m,
                                     currentUtilization: service.utilization)
    
    // Calculate required resources
    resourceRequired = calculateRequiredResources(currentCapacity: service.capacity,
                                               targetUtilization: 0.6,
                                               projectedDemand: $workloadAnalysis.projected_demand)
    
    // Simulate impact of scaling
    simulationResults = simulateScaling(currentConfig: service.config,
                                      proposedResources: $resourceRequired,
                                      serviceId: service.id)
    
    // Scale resources
    scaleOperation = scaleServiceResources(serviceId: service.id,
                                         newResources: $simulationResults.recommended_resources,
                                         reason: "high_utilization")
    
    // Update service configuration
    updateServiceConfig(serviceId: service.id,
                      configChange: $simulationResults.new_config)
    
    // Notify operations team
    notifyOperations(type: "auto_scaling",
                   serviceId: service.id,
                   scaleDetails: $scaleOperation)
    
    // Update monitoring thresholds
    updateMonitoringThresholds(serviceId: service.id,
                             newThresholds: $simulationResults.recommended_thresholds)
  }
}
```

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

## 9. Integration with Hadrian Systems

### 9.1 Factory Network
Effectus powers the "Factory Network as a Single Machine" by:
- Coordinating distributed CNC cells through typed, deterministic rules
- Enabling global resource allocation with consistent capability-based locking
- Providing real-time event propagation across the factory network
- Ensuring transactional integrity with compensation-based recovery

### 9.2 Capacity Management & Scheduling
Effectus enhances scheduling capabilities by:
- Implementing strongly-typed queuing models with explicit resource capabilities
- Executing ATP/CTP logic with predictable, verifiable outcomes
- Balancing WIP, changeover time, and due dates through priority-based rules
- Supporting safe concurrent scheduling through capability protection

### 9.3 Factory Execution Systems
Effectus drives production processes by:
- Managing state transitions for digital Kanban cards
- Orchestrating machine operations via capability-controlled effects
- Coordinating human and robotic tasks through unified rule evaluation
- Ensuring compensating actions for failed operations

### 9.4 Inventory
Effectus optimizes inventory management by:
- Triggering automated replenishment based on predefined thresholds
- Maintaining material traceability through fact-based state tracking
- Coordinating just-in-time delivery through rule constraints
- Protecting critical inventory operations with appropriate capabilities

### 9.5 DFM
Effectus improves design for manufacturing by:
- Enforcing manufacturability rules during design submission
- Automating design reviews with consistent evaluation criteria
- Tracking design changes with full causality and attribution
- Ensuring safe concurrent modifications through capability controls

### 9.6 Quoting
Effectus streamlines quoting processes by:
- Calculating accurate costs based on up-to-date capability models
- Automating capacity checks across the factory network
- Generating consistent quotes with traceable decision logic
- Ensuring transactional integrity through compensation

### 9.7 Quality
Effectus ensures quality control by:
- Orchestrating inspection workflows based on product specifications
- Managing quality gates with capability-controlled access
- Correlating quality data with process parameters for continuous improvement
- Providing fail-safe compensation strategies for interrupted inspections

## 10. Implementation Roadmap

1. **Phase 1**: Core engine implementation
   - Predicate evaluation system
   - List engine for sequential execution
   - Basic capability system
   - Schema registry

2. **Phase 2**: Advanced features
   - Flow engine implementation
   - Distributed locking with fencing
   - Saga pattern for transactions and compensation
   - Advanced capability enforcement

3. **Phase 3**: Tooling and integration
   - VS Code extension
   - CLI tools
   - Integration with Hadrian systems
   - Python and TypeScript client SDKs

4. **Phase 4**: Extended language support
   - Rust implementation of core engine
   - WebAssembly compilation targets
   - Language-specific optimizations
   - Cross-language test suite

## 11. Conclusion

Effectus provides Hadrian with a strongly typed rule engine for systems that ensures reliability through compile-time verification, capability-based protection, and compensation-driven recovery. It delivers the correctness guarantees needed for mission-critical systems while maintaining the flexibility to adapt to changing business requirements.
