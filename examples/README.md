# Effectus Examples

This directory contains examples showing how to extend Effectus with your own business logic.

## Philosophy

Effectus core is designed to be a **clean slate** - it provides the mathematical foundations and runtime, but doesn't include predefined business logic. This ensures the system remains flexible and doesn't impose domain assumptions.

## Examples

### 🔧 [Extension System](./extension_system/) **⭐ START HERE**
Comprehensive demonstration of the unified extension loading system.

```bash
cd extension_system
go run main.go
```

**What it demonstrates:**
- Static (compile-time) extension registration
- Dynamic (runtime) extension loading from JSON
- Protocol Buffer and OCI bundle support (placeholders)
- Unified interface for all extension types
- Directory scanning for automatic discovery

### 🏪 [Business Verbs](./business_verbs/)
Shows how to register domain-specific verbs for your business operations.

```bash
cd business_verbs
go run main.go
```

**What it demonstrates:**
- Creating custom verb executors
- Registering verbs with capabilities and resource requirements
- Setting up inverse verbs for compensation
- Handling idempotent, commutative, and exclusive operations

### 📊 [Business Facts](./business_facts/)  
Shows how to load business data and register domain-specific functions.

```bash
cd business_facts
go run main.go
```

**What it demonstrates:**
- Loading structured business data into the registry
- Registering custom functions for expression evaluation
- Using pathutil for fact access
- Type introspection and validation

### 🧭 [Fraud E2E](./fraud_e2e/)
End-to-end fraud workflow: list rules, flow execution, and saga compensation.

```bash
go run ./fraud_e2e
```

Convenience scripts:
```bash
./fraud_e2e/scripts/run-local.sh
./fraud_e2e/scripts/run-compose.sh
./fraud_e2e/scripts/run-compose-failure.sh
```

**What it demonstrates:**
- Typed fact schemas and fact loading
- Rule evaluation with list specs
- Flow execution with saga compensation
- Verb registry wiring with inverse verbs

### 📦 [Multi-Bundle Runtime](./multi_bundle_runtime/)
Manifest-driven bundle resolution, merged rule execution, and hot reload.

```bash
go run ./multi_bundle_runtime
./multi_bundle_runtime/scripts/hot-reload.sh
```

**What it demonstrates:**
- Resolving multiple bundles from a manifest
- Loading schemas/verbs/rules across bundles
- Hot reload by swapping bundle versions

### 🧊 [Warehouse Sources](./warehouse_sources/)
Concrete configs for Snowflake (SQL adapter) and Iceberg via Trino (Iceberg adapter).

```bash
ls ./warehouse_sources
```

**What it demonstrates:**
- Production-style config shapes for warehouse ingestion
- Batch Snowflake snapshots and streaming Iceberg tables
- Local Trino + Iceberg + MinIO devstack in `warehouse_sources/devstack`

## Integration Patterns

### 1. **Unified Extension Pattern** ⭐ **RECOMMENDED**
```go
// Create extension manager
em := loader.NewExtensionManager()

// Add static extensions (compile-time)
em.AddLoader(loader.NewStaticVerbLoader("business", myVerbs))
em.AddLoader(loader.NewStaticSchemaLoader("business").AddFunction("tax", calculateTax))

// Add dynamic extensions (runtime)
em.AddLoader(loader.NewJSONVerbLoader("external", "verbs.json"))
em.AddLoader(loader.NewJSONSchemaLoader("config", "schema.json"))

// Load all extensions
registry := schema.NewRegistry()
verbRegistry := verb.NewRegistry(nil)
schema.LoadExtensionsIntoRegistries(em, registry, verbRegistry)
```

### 2. **Directory Scanning Pattern**
```go
// Automatically discover extensions in directories
em, err := schema.CreateExtensionManagerWithDefaults(
    "./config/extensions",
    "./business/extensions",
)
if err != nil {
    log.Fatal(err)
}

// Load everything
schema.LoadExtensionsIntoRegistries(em, registry, verbRegistry)
```

### 3. **Registry-First Pattern**
```go
// Direct registry manipulation (less flexible)
registry := schema.NewRegistry()
registry.RegisterFunction("validateCustomer", myValidationFunc)
registry.LoadFromMap(myBusinessData)

verbRegistry := verb.NewRegistry(nil)
verbRegistry.RegisterVerb(&verb.Spec{...})
```

### 4. **Fact Provider Pattern**
```go
// Use pathutil for structured fact access
factProvider := pathutil.NewRegistryFactProvider()
factProvider.GetRegistry().LoadFromMap(data)

// Access facts with dot notation
if value, exists := factProvider.Get("customer.vip"); exists {
    // Use the value
}
```

## Building Your Application

1. **Start with core Effectus** - no business logic included
2. **Register your domain verbs** - define what operations your system can perform
3. **Register your domain functions** - define expression functions for your business rules
4. **Load your data** - facts from databases, APIs, etc.
5. **Define your rules** - using `.eff` files with your domain language

## Key Benefits

- ✅ **No vendor lock-in** - your business logic is separate from the framework
- ✅ **Clean testing** - test your verbs and functions independently  
- ✅ **Domain-driven** - use your own business terminology
- ✅ **Flexible** - add new verbs and functions as your business evolves
- ✅ **Composable** - mix and match capabilities as needed

## See Also

- [Extension System Documentation](../loader/README.md) ⭐
- [Core Architecture](../docs/ARCHITECTURE.md)
- [Design Principles](../docs/design.md)  
- [Verb System Documentation](../schema/verb/)
- [Expression Registry](../schema/) 
