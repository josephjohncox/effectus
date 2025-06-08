package runtime

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/effectus/effectus-go/compiler"
	"github.com/effectus/effectus-go/loader"
)

// ExecutionRuntime orchestrates the complete flow from extension loading to execution
type ExecutionRuntime struct {
	extensionManager *loader.ExtensionManager
	compiler         *compiler.ExtensionCompiler
	compiledUnit     *compiler.CompiledUnit
	executors        map[compiler.ExecutorType]ExecutorFactory
	mu               sync.RWMutex
	state            RuntimeState
}

// RuntimeState represents the current state of the runtime
type RuntimeState string

const (
	StateInitializing RuntimeState = "initializing"
	StateLoading      RuntimeState = "loading"
	StateCompiling    RuntimeState = "compiling"
	StateReady        RuntimeState = "ready"
	StateExecuting    RuntimeState = "executing"
	StateFailed       RuntimeState = "failed"
)

// NewExecutionRuntime creates a new execution runtime
func NewExecutionRuntime() *ExecutionRuntime {
	runtime := &ExecutionRuntime{
		extensionManager: loader.NewExtensionManager(),
		compiler:         compiler.NewExtensionCompiler(),
		executors:        make(map[compiler.ExecutorType]ExecutorFactory),
		state:            StateInitializing,
	}

	// Register default executor factories
	runtime.RegisterExecutorFactory(compiler.ExecutorLocal, &LocalExecutorFactory{})
	runtime.RegisterExecutorFactory(compiler.ExecutorHTTP, &HTTPExecutorFactory{})
	runtime.RegisterExecutorFactory(compiler.ExecutorMessage, &MessageExecutorFactory{})
	runtime.RegisterExecutorFactory(compiler.ExecutorMock, &MockExecutorFactory{})

	return runtime
}

// RegisterExtensionLoader adds an extension loader to the runtime
func (er *ExecutionRuntime) RegisterExtensionLoader(loader loader.Loader) {
	er.extensionManager.AddLoader(loader)
}

// RegisterExecutorFactory registers a factory for creating executors
func (er *ExecutionRuntime) RegisterExecutorFactory(executorType compiler.ExecutorType, factory ExecutorFactory) {
	er.executors[executorType] = factory
}

// CompileAndValidate loads extensions, compiles them, and validates everything
func (er *ExecutionRuntime) CompileAndValidate(ctx context.Context) error {
	er.mu.Lock()
	defer er.mu.Unlock()

	er.state = StateLoading

	// Compile extensions
	er.state = StateCompiling
	result, err := er.compiler.Compile(ctx, er.extensionManager)
	if err != nil {
		er.state = StateFailed
		return fmt.Errorf("compilation failed: %w", err)
	}

	if !result.Success {
		er.state = StateFailed
		return fmt.Errorf("compilation errors: %v", result.Errors)
	}

	// Store compiled unit
	er.compiledUnit = result.CompiledUnit
	er.state = StateReady

	// Log warnings if any
	if len(result.Warnings) > 0 {
		for _, warning := range result.Warnings {
			log.Printf("Warning: %s in %s: %s", warning.Type, warning.Location, warning.Message)
		}
	}

	log.Printf("Runtime compiled successfully with %d verbs, %d functions",
		len(er.compiledUnit.VerbSpecs),
		len(er.compiledUnit.Functions))

	return nil
}

// ExecuteVerb executes a specific verb with the given arguments
func (er *ExecutionRuntime) ExecuteVerb(ctx context.Context, verbName string, args map[string]interface{}) (interface{}, error) {
	er.mu.RLock()
	defer er.mu.RUnlock()

	if er.state != StateReady {
		return nil, fmt.Errorf("runtime not ready (state: %s)", er.state)
	}

	verbSpec, exists := er.compiledUnit.VerbSpecs[verbName]
	if !exists {
		return nil, fmt.Errorf("verb not found: %s", verbName)
	}

	// Validate arguments
	if err := er.validateVerbArgs(verbSpec, args); err != nil {
		return nil, fmt.Errorf("argument validation failed: %w", err)
	}

	// Create executor
	executorFactory, exists := er.executors[verbSpec.ExecutorType]
	if !exists {
		return nil, fmt.Errorf("no executor factory for type: %s", verbSpec.ExecutorType)
	}

	executor, err := executorFactory.CreateExecutor(verbSpec.ExecutorConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create executor: %w", err)
	}

	// Execute verb
	result, err := executor.Execute(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("verb execution failed: %w", err)
	}

	return result, nil
}

// ExecuteWorkflow executes a complete workflow using the execution plan
func (er *ExecutionRuntime) ExecuteWorkflow(ctx context.Context, facts map[string]interface{}) error {
	er.mu.RLock()
	defer er.mu.RUnlock()

	if er.state != StateReady {
		return fmt.Errorf("runtime not ready (state: %s)", er.state)
	}

	if er.compiledUnit.ExecutionPlan == nil {
		return fmt.Errorf("no execution plan available")
	}

	er.state = StateExecuting
	defer func() { er.state = StateReady }()

	// Execute each phase in the execution plan
	for i, phase := range er.compiledUnit.ExecutionPlan.Phases {
		log.Printf("Executing phase %d: %s", i+1, phase.Name)

		if err := er.executePhase(ctx, phase, facts); err != nil {
			return fmt.Errorf("phase %s failed: %w", phase.Name, err)
		}
	}

	return nil
}

// GetRuntimeInfo returns information about the current runtime state
func (er *ExecutionRuntime) GetRuntimeInfo() *RuntimeInfo {
	er.mu.RLock()
	defer er.mu.RUnlock()

	info := &RuntimeInfo{
		State:       er.state,
		LoaderCount: len(er.extensionManager.GetLoaders()),
	}

	if er.compiledUnit != nil {
		info.VerbCount = len(er.compiledUnit.VerbSpecs)
		info.FunctionCount = len(er.compiledUnit.Functions)
		info.Dependencies = er.compiledUnit.Dependencies
		info.Capabilities = er.compiledUnit.Capabilities
	}

	return info
}

// HotReload reloads and recompiles all extensions
func (er *ExecutionRuntime) HotReload(ctx context.Context) error {
	log.Println("Starting hot reload...")

	// Reset state
	er.mu.Lock()
	er.compiledUnit = nil
	er.state = StateInitializing
	er.mu.Unlock()

	// Recompile
	if err := er.CompileAndValidate(ctx); err != nil {
		return fmt.Errorf("hot reload failed: %w", err)
	}

	log.Println("Hot reload completed successfully")
	return nil
}

// Helper methods

func (er *ExecutionRuntime) validateVerbArgs(verbSpec *compiler.CompiledVerbSpec, args map[string]interface{}) error {
	// Check required arguments
	for _, required := range verbSpec.Spec.RequiredArgs {
		if _, exists := args[required]; !exists {
			return fmt.Errorf("missing required argument: %s", required)
		}
	}

	// Validate argument types
	for argName, argValue := range args {
		expectedType, exists := verbSpec.TypeSignature.InputTypes[argName]
		if !exists {
			return fmt.Errorf("unexpected argument: %s", argName)
		}

		if err := er.validateArgumentType(argName, argValue, expectedType); err != nil {
			return err
		}
	}

	return nil
}

func (er *ExecutionRuntime) validateArgumentType(name string, value interface{}, expectedType string) error {
	// Basic type validation (could be expanded)
	switch expectedType {
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("argument %s expected string, got %T", name, value)
		}
	case "int", "integer":
		if _, ok := value.(int); !ok {
			return fmt.Errorf("argument %s expected integer, got %T", name, value)
		}
	case "float", "number":
		if _, ok := value.(float64); !ok {
			return fmt.Errorf("argument %s expected float, got %T", name, value)
		}
	case "bool", "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("argument %s expected boolean, got %T", name, value)
		}
	}
	return nil
}

func (er *ExecutionRuntime) executePhase(ctx context.Context, phase compiler.ExecutionPhase, facts map[string]interface{}) error {
	// Set timeout if specified
	if phase.Timeout != "" {
		timeout, err := time.ParseDuration(phase.Timeout)
		if err != nil {
			return fmt.Errorf("invalid timeout: %w", err)
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	if phase.Parallel {
		return er.executeVerbsParallel(ctx, phase.Verbs, facts, phase.ErrorPolicy)
	} else {
		return er.executeVerbsSequential(ctx, phase.Verbs, facts, phase.ErrorPolicy)
	}
}

func (er *ExecutionRuntime) executeVerbsSequential(ctx context.Context, verbs []string, facts map[string]interface{}, policy compiler.ErrorPolicy) error {
	for _, verbName := range verbs {
		if err := er.executeVerbWithFacts(ctx, verbName, facts); err != nil {
			switch policy {
			case compiler.ErrorPolicyFail:
				return err
			case compiler.ErrorPolicyContinue:
				log.Printf("Verb %s failed, continuing: %v", verbName, err)
			case compiler.ErrorPolicyRetry:
				// Simple retry logic
				if retryErr := er.executeVerbWithFacts(ctx, verbName, facts); retryErr != nil {
					return fmt.Errorf("verb %s failed after retry: %w", verbName, retryErr)
				}
			}
		}
	}
	return nil
}

func (er *ExecutionRuntime) executeVerbsParallel(ctx context.Context, verbs []string, facts map[string]interface{}, policy compiler.ErrorPolicy) error {
	var wg sync.WaitGroup
	errChan := make(chan error, len(verbs))

	for _, verbName := range verbs {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			if err := er.executeVerbWithFacts(ctx, name, facts); err != nil {
				errChan <- fmt.Errorf("verb %s: %w", name, err)
			}
		}(verbName)
	}

	wg.Wait()
	close(errChan)

	// Collect errors
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 && policy == compiler.ErrorPolicyFail {
		return fmt.Errorf("parallel execution failed: %v", errors)
	}

	return nil
}

func (er *ExecutionRuntime) executeVerbWithFacts(ctx context.Context, verbName string, facts map[string]interface{}) error {
	// Execute verb using facts as arguments
	// In a real implementation, this would extract the appropriate arguments from facts
	_, err := er.ExecuteVerb(ctx, verbName, facts)
	return err
}

// Supporting types

// RuntimeInfo provides information about the runtime state
type RuntimeInfo struct {
	State         RuntimeState `json:"state"`
	LoaderCount   int          `json:"loaderCount"`
	VerbCount     int          `json:"verbCount"`
	FunctionCount int          `json:"functionCount"`
	Dependencies  []string     `json:"dependencies"`
	Capabilities  []string     `json:"capabilities"`
}

// ExecutorFactory creates executors for different types
type ExecutorFactory interface {
	CreateExecutor(config compiler.ExecutorConfig) (VerbExecutor, error)
}

// VerbExecutor defines the interface for executing verbs
type VerbExecutor interface {
	Execute(ctx context.Context, args map[string]interface{}) (interface{}, error)
}

// Executor factory implementations
type LocalExecutorFactory struct{}
type HTTPExecutorFactory struct{}
type MessageExecutorFactory struct{}
type MockExecutorFactory struct{}

func (lef *LocalExecutorFactory) CreateExecutor(config compiler.ExecutorConfig) (VerbExecutor, error) {
	localConfig, ok := config.(*compiler.LocalExecutorConfig)
	if !ok {
		return nil, fmt.Errorf("invalid config type for local executor")
	}
	return &LocalExecutorAdapter{impl: localConfig.Implementation}, nil
}

func (hef *HTTPExecutorFactory) CreateExecutor(config compiler.ExecutorConfig) (VerbExecutor, error) {
	httpConfig, ok := config.(*compiler.HTTPExecutorConfig)
	if !ok {
		return nil, fmt.Errorf("invalid config type for HTTP executor")
	}
	return &HTTPExecutor{config: httpConfig}, nil
}

func (mef *MessageExecutorFactory) CreateExecutor(config compiler.ExecutorConfig) (VerbExecutor, error) {
	return &MessageExecutor{}, nil
}

func (mef *MockExecutorFactory) CreateExecutor(config compiler.ExecutorConfig) (VerbExecutor, error) {
	return &MockExecutor{}, nil
}

// Executor implementations
type LocalExecutorAdapter struct {
	impl loader.VerbExecutor
}

func (lea *LocalExecutorAdapter) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return lea.impl.Execute(ctx, args)
}

type HTTPExecutor struct {
	config *compiler.HTTPExecutorConfig
}

func (he *HTTPExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	// TODO: Implement actual HTTP execution
	return map[string]interface{}{
		"status": "http_success",
		"url":    he.config.URL,
		"args":   args,
	}, nil
}

type MessageExecutor struct{}

func (me *MessageExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	// TODO: Implement message queue execution
	return map[string]interface{}{
		"status": "message_success",
		"args":   args,
	}, nil
}

type MockExecutor struct{}

func (me *MockExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return map[string]interface{}{
		"status": "mock_success",
		"args":   args,
	}, nil
}
