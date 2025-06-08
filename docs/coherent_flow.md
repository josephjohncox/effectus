# Coherent Flow Architecture

## Overview

The Effectus system now implements a **coherent flow** from extension loading through compilation to execution. This ensures static validation before runtime and provides clear separation of concerns across the entire system.

## Architecture Phases

### 1. Extension Loading
**Purpose**: Load verbs and schemas from multiple sources  
**Interface**: `loader.ExtensionManager`

```go
// Static registration (compile-time)
runtime.RegisterExtensionLoader(loader.NewStaticVerbLoader("business", verbs))

// Dynamic registration (runtime)
runtime.RegisterExtensionLoader(loader.NewJSONVerbLoader("external", "verbs.json"))

// OCI bundle support (future)
runtime.RegisterExtensionLoader(loader.NewOCIBundleLoader("registry", "bundle"))
```

**Key Features**:
- Multiple loader types: Static, JSON, Protocol Buffer, OCI
- Unified interfaces for all extension types
- Directory scanning and auto-discovery

### 2. Compilation & Validation
**Purpose**: Validate all extensions before execution  
**Interface**: `compiler.ExtensionCompiler`

```go
result, err := compiler.Compile(ctx, extensionManager)
if !result.Success {
    // Handle compilation errors before starting daemon
    return fmt.Errorf("compilation failed: %v", result.Errors)
}
```

**Validation Steps**:
1. **Type System Building**: Extract and validate type definitions
2. **Verb Compilation**: Validate verb specifications and signatures
3. **Dependency Resolution**: Check verb dependencies and capabilities
4. **Execution Planning**: Create optimized execution phases
5. **Security Validation**: Ensure capability constraints

**Error Types**:
- `type_error`: Type mismatches or invalid signatures
- `dependency_error`: Missing or circular dependencies  
- `capability_error`: Insufficient or conflicting capabilities
- `security_error`: Policy violations

### 3. Execution Planning
**Purpose**: Create optimized execution strategies  
**Interface**: `compiler.ExecutionPlan`

```go
type ExecutionPlan struct {
    Phases        []ExecutionPhase     // Sequential execution phases
    Dependencies  map[string][]string  // Dependency graph
    Capabilities  map[string][]string  // Required capabilities
    Executors     map[string]ExecutorConfig // Execution configuration
}
```

**Phase Types**:
- **Sequential**: Verbs execute in order
- **Parallel**: Verbs execute concurrently
- **Conditional**: Verbs execute based on predicates

**Error Policies**:
- `fail`: Stop on first error
- `continue`: Continue despite errors
- `retry`: Retry failed operations
- `compensate`: Run inverse operations

### 4. Execution Runtime
**Purpose**: Execute compiled verbs with appropriate executors  
**Interface**: `runtime.ExecutionRuntime`

```go
// Individual verb execution
result, err := runtime.ExecuteVerb(ctx, "ProcessPayment", args)

// Workflow execution
err := runtime.ExecuteWorkflow(ctx, facts)
```

**Executor Types**:
- **Local**: In-process execution (`ExecutorLocal`)
- **HTTP**: Remote HTTP APIs (`ExecutorHTTP`)
- **gRPC**: Remote gRPC services (`ExecutorGRPC`)
- **Message**: Queue-based execution (`ExecutorMessage`)
- **Mock**: Testing execution (`ExecutorMock`)

## Coherent Interface Design

### Separation of Concerns

**Verb Specification** (What):
```go
type VerbSpec interface {
    GetName() string
    GetCapabilities() []string
    GetArgTypes() map[string]string
    GetReturnType() string
}
```

**Verb Executor** (How):
```go
type VerbExecutor interface {
    Execute(ctx context.Context, args map[string]interface{}) (interface{}, error)
}
```

**Execution Configuration**:
```go
type ExecutorConfig interface {
    GetType() ExecutorType
    Validate() error
}
```

### Type Safety & Validation

**Compile-time Safety** (Static):
- Type checking for static verbs
- Dependency validation
- Capability verification

**Runtime Safety** (Dynamic):
- Argument validation
- Type coercion
- Error handling

## Flow Example

```go
// 1. Load Extensions
runtime := runtime.NewExecutionRuntime()
runtime.RegisterExtensionLoader(staticLoader)
runtime.RegisterExtensionLoader(dynamicLoader)

// 2. Compile & Validate (BEFORE daemon starts)
if err := runtime.CompileAndValidate(ctx); err != nil {
    log.Fatal("Compilation failed - fix before starting")
}

// 3. Execute (daemon running)
result, err := runtime.ExecuteVerb(ctx, "ProcessPayment", args)
```

## Key Benefits

### 1. **Fail-Fast Validation**
- All errors caught before daemon starts
- No runtime surprises from invalid configurations
- Clear error messages with suggestions

### 2. **Clear Separation**
- Specification separate from implementation
- Multiple execution strategies for same verb
- Easy testing with mock executors

### 3. **Extensibility**
- Plugin-like architecture for new loaders
- Support for different executor types
- Hot-reload capability

### 4. **Type Safety**
- Static type checking where possible
- Runtime validation for dynamic extensions
- Comprehensive error reporting

### 5. **Performance**
- Optimized execution plans
- Parallel execution support
- Efficient dependency resolution

## Integration Points

### Daemon Integration
```go
func main() {
    runtime := runtime.NewExecutionRuntime()
    
    // Load extensions from config/directory/OCI
    loadExtensions(runtime)
    
    // Compile and validate BEFORE starting server
    if err := runtime.CompileAndValidate(ctx); err != nil {
        log.Fatal("Startup validation failed: %v", err)
    }
    
    // Start HTTP/gRPC server
    server := startServer(runtime)
    server.Run()
}
```

### Testing Integration
```go
func TestBusinessLogic(t *testing.T) {
    runtime := runtime.NewExecutionRuntime()
    runtime.RegisterExtensionLoader(testLoader)
    
    // Use mock executors for testing
    runtime.RegisterExecutorFactory(compiler.ExecutorMock, &MockFactory{})
    
    assert.NoError(t, runtime.CompileAndValidate(ctx))
    
    result, err := runtime.ExecuteVerb(ctx, "ValidatePayment", args)
    assert.NoError(t, err)
    assert.True(t, result["valid"].(bool))
}
```

## Future Enhancements

1. **Distributed Execution**: Support for distributed verb execution
2. **Caching**: Cache compiled units for faster startup
3. **Metrics**: Detailed execution metrics and tracing
4. **Security**: Enhanced capability-based security model
5. **Optimization**: Advanced execution plan optimization

This coherent flow ensures that Effectus maintains consistency across all operational phases while providing the flexibility needed for diverse deployment scenarios. 