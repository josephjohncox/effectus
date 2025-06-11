# Dynamic Proto-First Architecture

The buf/protobuf integration fundamentally transforms Effectus into a **dynamic, schema-driven system** where everything flows from typed proto definitions.

## **Core Architecture Principles**

### 1. **Schema-Driven Everything**
```
Proto Schemas → Type Generation → Runtime Validation → Dynamic Compilation
```

- **Facts** are strongly typed proto messages
- **Verbs** are gRPC service interfaces  
- **Rules** compile against typed schemas
- **Compatibility** is enforced automatically

### 2. **Separation of Concerns**

```go
// Pure evaluation logic - no I/O
type CoreEvaluator struct {
    ruleRegistry *atomic.Value // Hot-swappable rules
    metrics      *EvaluationMetrics
}

// I/O and external interactions
type EffectExecutor struct {
    verbExecutors map[string]VerbExecutor
    lockManager   LockManager
    sagaManager   SagaManager
}
```

### 3. **Hot-Reload Architecture**
```
Schema Update → Compatibility Check → Rule Recompilation → Atomic Swap
```

## **Multi-Source Fact Ingestion**

### Fact Source Interface
```go
type FactSource interface {
    // Subscribe to typed facts with schema validation
    Subscribe(ctx context.Context, factTypes []string) (<-chan *TypedFact, error)
    
    // Schema compatibility
    GetSourceSchema() *effectusv1.Schema
    
    // Source metadata
    GetMetadata() SourceMetadata
}
```

### Supported Source Types

#### **Kafka Adapter**
```go
type KafkaFactSource struct {
    reader         *kafka.Reader
    schemaRegistry *schema.BufSchemaRegistry
    converter      *ProtoConverter
}

func (k *KafkaFactSource) Subscribe(ctx context.Context, factTypes []string) (<-chan *TypedFact, error) {
    factChan := make(chan *TypedFact, 100)
    
    go func() {
        for {
            msg, err := k.reader.ReadMessage(ctx)
            if err != nil {
                continue
            }
            
            // Convert Kafka message to typed fact
            fact, err := k.converter.ConvertToTypedFact(msg.Value, msg.Headers)
            if err != nil {
                continue
            }
            
            factChan <- fact
        }
    }()
    
    return factChan, nil
}
```

#### **HTTP API Adapter**  
```go
type HTTPFactSource struct {
    endpoint       string
    client         *http.Client
    schemaRegistry *schema.BufSchemaRegistry
    pollInterval   time.Duration
}

func (h *HTTPFactSource) Subscribe(ctx context.Context, factTypes []string) (<-chan *TypedFact, error) {
    // Poll HTTP endpoint for new facts
    // Convert JSON/XML to proto messages
    // Validate against schemas
}
```

#### **Database Adapter**
```go
type DatabaseFactSource struct {
    db             *sql.DB
    schemaRegistry *schema.BufSchemaRegistry
    changeStream   ChangeStreamReader
}

func (d *DatabaseFactSource) Subscribe(ctx context.Context, factTypes []string) (<-chan *TypedFact, error) {
    // Listen to database change streams
    // Convert SQL rows to proto messages
    // Handle schema evolution
}
```

## **Dynamic Rule Compilation Pipeline**

### 1. **Schema Validation**
```go
type SchemaValidator struct {
    factRegistry *schema.FactSchemaRegistry
    verbRegistry *schema.VerbSchemaRegistry
}

func (sv *SchemaValidator) ValidateRule(rule *RuleDefinition) error {
    // Validate fact types exist
    for _, predicate := range rule.Predicates {
        schema, err := sv.factRegistry.GetSchema(predicate.FactType)
        if err != nil {
            return fmt.Errorf("unknown fact type: %s", predicate.FactType)
        }
        
        // Validate field paths
        if !schema.HasField(predicate.Path) {
            return fmt.Errorf("field %s not found in %s", predicate.Path, predicate.FactType)
        }
    }
    
    // Validate verb interfaces exist
    for _, effect := range rule.Effects {
        _, err := sv.verbRegistry.GetInterface(effect.VerbName)
        if err != nil {
            return fmt.Errorf("unknown verb: %s", effect.VerbName)
        }
    }
    
    return nil
}
```

### 2. **Typed Compilation**
```go
type RuleCompiler struct {
    schemaRegistry *schema.BufSchemaRegistry
    typeGenerator  *TypeGenerator
    cache          *CompilationCache
}

func (rc *RuleCompiler) CompileRule(rule *RuleDefinition) (*CompiledRule, error) {
    // Generate typed predicate validators
    predicates := make([]*TypedPredicate, len(rule.Predicates))
    for i, pred := range rule.Predicates {
        validator, err := rc.generatePredicateValidator(pred)
        if err != nil {
            return nil, err
        }
        predicates[i] = &TypedPredicate{
            FactType:      pred.FactType,
            SchemaVersion: pred.SchemaVersion,
            Path:          pred.Path,
            Operator:      pred.Operator,
            Value:         pred.Value,
            Validator:     validator,
        }
    }
    
    // Generate typed effect validators
    effects := make([]*TypedEffect, len(rule.Effects))
    for i, eff := range rule.Effects {
        validator, err := rc.generateEffectValidator(eff)
        if err != nil {
            return nil, err
        }
        effects[i] = &TypedEffect{
            VerbName:         eff.VerbName,
            InterfaceVersion: eff.InterfaceVersion,
            Args:             eff.Args,
            Validator:        validator,
        }
    }
    
    return &CompiledRule{
        Name:       rule.Name,
        Type:       rule.Type,
        Predicates: predicates,
        Effects:    effects,
        SchemaHash: rc.computeSchemaHash(rule),
    }, nil
}
```

### 3. **Hot-Reload Management**
```go
type HotLoader struct {
    schemaRegistry *schema.BufSchemaRegistry
    ruleCompiler   *RuleCompiler
    ruleRegistry   *atomic.Value
    updateChan     chan *UpdateEvent
}

func (hl *HotLoader) Run(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        case update := <-hl.updateChan:
            if err := hl.handleUpdate(update); err != nil {
                // Log error but continue
                continue
            }
        case <-time.After(30 * time.Second):
            // Periodic schema sync from buf registry
            if err := hl.syncSchemas(); err != nil {
                continue
            }
        }
    }
}

func (hl *HotLoader) handleUpdate(update *UpdateEvent) error {
    switch update.Type {
    case UpdateTypeSchemaChange:
        return hl.handleSchemaUpdate(update)
    case UpdateTypeRuleChange:
        return hl.handleRuleUpdate(update)
    default:
        return fmt.Errorf("unknown update type: %v", update.Type)
    }
}

func (hl *HotLoader) handleSchemaUpdate(update *UpdateEvent) error {
    // 1. Validate breaking changes
    if err := hl.schemaRegistry.ValidateUpdate(update); err != nil {
        return fmt.Errorf("breaking change detected: %w", err)
    }
    
    // 2. Apply schema update
    if err := hl.schemaRegistry.ApplyUpdate(update); err != nil {
        return err
    }
    
    // 3. Recompile affected rules
    affectedRules := hl.findAffectedRules(update.SchemaName)
    newRegistry, err := hl.recompileRules(affectedRules)
    if err != nil {
        return err
    }
    
    // 4. Atomic swap
    hl.ruleRegistry.Store(newRegistry)
    
    return nil
}
```

## **User Workflow Integration**

### 1. **Schema-First Development**
```bash
# 1. Define fact schema
buf generate examples/company/proto/acme/v1/facts/

# 2. Write rules against typed interfaces  
effectusc compile --schema-version=v1.2.0 rules/

# 3. Deploy with compatibility checking
effectusc deploy --check-compatibility rules.effx
```

### 2. **Type-Safe Rule Authoring**
```go
// Generated from proto schema
type UserProfileFact struct {
    UserID    string `protobuf:"bytes,1,opt,name=user_id"`
    Email     string `protobuf:"bytes,2,opt,name=email"`
    Status    UserStatus `protobuf:"varint,11,opt,name=status"`
}

// Rule with compile-time type safety
rule "send_welcome_email" {
    when UserProfileFact.Status == ACCOUNT_STATUS_ACTIVE
    then send_email(
        to: UserProfileFact.Email,
        template: "welcome",
        data: UserProfileFact
    )
}
```

### 3. **Multi-Language Clients**
```python
# Python client auto-generated from same schemas
from acme.v1.facts import user_profile_pb2
from effectus.client import EffectusClient

client = EffectusClient("localhost:8080")

# Type-safe fact submission
fact = user_profile_pb2.UserProfile(
    user_id="user123",
    email="user@example.com", 
    status=user_profile_pb2.ACCOUNT_STATUS_ACTIVE
)

# Execute rules with full type safety
result = client.execute_rules([fact])
```

## **Benefits of This Architecture**

### **Dynamic Capabilities**
✅ **Hot schema updates** without downtime  
✅ **Rule recompilation** with rollback safety  
✅ **Multi-source ingestion** with unified types  
✅ **Breaking change detection** prevents runtime errors  

### **Developer Experience**  
✅ **Type safety** at compile time  
✅ **Auto-generated clients** for all languages  
✅ **Schema evolution** with migration guides  
✅ **Zero-config deployment** with buf registry  

### **Operational Excellence**
✅ **Atomic updates** prevent inconsistent state  
✅ **Rollback capabilities** for failed deployments  
✅ **Observability** with schema-aware metrics  
✅ **Multi-tenant** schema isolation  

## **Migration Path**

### Phase 1: Proto Schemas
- Convert existing JSON schemas to proto
- Set up buf registry
- Generate initial Go types

### Phase 2: Dynamic Compilation  
- Implement schema-driven rule compilation
- Add hot-reload capabilities
- Migrate existing rules

### Phase 3: Multi-Source Integration
- Implement fact source adapters
- Add format converters (JSON → Proto)
- Scale to multiple data sources

### Phase 4: Advanced Features
- Schema evolution automation
- Cross-language rule authoring
- Visual rule builder with type hints

This architecture transforms Effectus from a static system into a **dynamic, schema-driven platform** that can evolve safely at runtime while maintaining mathematical rigor and type safety. 