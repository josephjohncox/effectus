package compiler

import (
	"context"
	"fmt"

	"github.com/effectus/effectus-go/loader"
	"github.com/effectus/effectus-go/schema"
	"github.com/effectus/effectus-go/schema/verb"
)

// CompilationResult represents the outcome of compilation
type CompilationResult struct {
	Success      bool
	Errors       []CompilationError
	Warnings     []CompilationWarning
	CompiledUnit *CompiledUnit
}

// CompilationError represents a compilation error
type CompilationError struct {
	Type        string // "type_error", "dependency_error", "capability_error"
	Component   string // "verb", "function", "expression"
	Location    string // verb name, function name, etc.
	Message     string
	Suggestions []string
}

// CompilationWarning represents a compilation warning
type CompilationWarning struct {
	Type     string
	Location string
	Message  string
}

// CompiledUnit represents a fully validated and ready-to-execute unit
type CompiledUnit struct {
	VerbSpecs     map[string]*CompiledVerbSpec
	Functions     map[string]*CompiledFunction
	TypeSystem    *TypeSystem
	ExecutionPlan *ExecutionPlan
	Dependencies  []string // External dependencies required
	Capabilities  []string // Required capabilities
}

// CompiledVerbSpec represents a validated verb specification
type CompiledVerbSpec struct {
	Spec            *verb.StandardVerbSpec
	ExecutorType    ExecutorType
	ExecutorConfig  ExecutorConfig
	Dependencies    []string // Other verbs this depends on
	TypeSignature   *TypeSignature
	ValidationRules []ValidationRule
}

// ExecutorType defines how a verb should be executed
type ExecutorType string

const (
	ExecutorLocal    ExecutorType = "local"    // Execute in-process
	ExecutorHTTP     ExecutorType = "http"     // Execute via HTTP
	ExecutorGRPC     ExecutorType = "grpc"     // Execute via gRPC
	ExecutorMessage  ExecutorType = "message"  // Execute via message queue
	ExecutorExternal ExecutorType = "external" // Execute in external system
	ExecutorMock     ExecutorType = "mock"     // Mock execution for testing
)

// ExecutorConfig contains configuration for verb execution
type ExecutorConfig interface {
	GetType() ExecutorType
	Validate() error
}

// LocalExecutorConfig for in-process execution
type LocalExecutorConfig struct {
	Implementation loader.VerbExecutor
}

func (lec *LocalExecutorConfig) GetType() ExecutorType { return ExecutorLocal }
func (lec *LocalExecutorConfig) Validate() error {
	if lec.Implementation == nil {
		return fmt.Errorf("local executor requires implementation")
	}
	return nil
}

// HTTPExecutorConfig for HTTP-based execution
type HTTPExecutorConfig struct {
	URL         string            `json:"url"`
	Method      string            `json:"method"`
	Headers     map[string]string `json:"headers"`
	Timeout     string            `json:"timeout"`
	RetryPolicy *RetryPolicy      `json:"retryPolicy,omitempty"`
}

func (hec *HTTPExecutorConfig) GetType() ExecutorType { return ExecutorHTTP }
func (hec *HTTPExecutorConfig) Validate() error {
	if hec.URL == "" {
		return fmt.Errorf("HTTP executor requires URL")
	}
	if hec.Method == "" {
		hec.Method = "POST"
	}
	return nil
}

// MessageExecutorConfig for message queue execution
type MessageExecutorConfig struct {
	Topic       string       `json:"topic"`
	Queue       string       `json:"queue"`
	Exchange    string       `json:"exchange"`
	RoutingKey  string       `json:"routingKey"`
	Timeout     string       `json:"timeout"`
	RetryPolicy *RetryPolicy `json:"retryPolicy,omitempty"`
}

func (mec *MessageExecutorConfig) GetType() ExecutorType { return ExecutorMessage }
func (mec *MessageExecutorConfig) Validate() error {
	if mec.Topic == "" && mec.Queue == "" {
		return fmt.Errorf("message executor requires topic or queue")
	}
	return nil
}

// RetryPolicy defines retry behavior for external executors
type RetryPolicy struct {
	MaxRetries      int      `json:"maxRetries"`
	InitialDelay    string   `json:"initialDelay"`
	MaxDelay        string   `json:"maxDelay"`
	BackoffFactor   float64  `json:"backoffFactor"`
	RetryableErrors []string `json:"retryableErrors"`
}

// CompiledFunction represents a validated function
type CompiledFunction struct {
	Name           string
	Implementation interface{}
	TypeSignature  *TypeSignature
	Dependencies   []string
}

// TypeSignature represents the type information for a verb or function
type TypeSignature struct {
	InputTypes  map[string]string // arg name -> type
	OutputType  string
	Constraints []TypeConstraint
}

// TypeConstraint represents a constraint on types
type TypeConstraint struct {
	Type        string // "range", "enum", "pattern", "dependency"
	Parameter   string
	Values      []interface{}
	Description string
}

// ValidationRule represents a validation rule for a verb
type ValidationRule struct {
	Type         string // "input", "output", "capability", "dependency"
	Expression   string
	ErrorMessage string
}

// ExecutionPlan defines how compiled verbs should be executed
type ExecutionPlan struct {
	Phases       []ExecutionPhase
	Dependencies map[string][]string // verb -> dependencies
	Capabilities map[string][]string // verb -> required capabilities
	Executors    map[string]ExecutorConfig
}

// ExecutionPhase represents a phase in the execution plan
type ExecutionPhase struct {
	Name        string
	Verbs       []string
	Parallel    bool
	Timeout     string
	ErrorPolicy ErrorPolicy
}

// ErrorPolicy defines how to handle errors in execution
type ErrorPolicy string

const (
	ErrorPolicyFail       ErrorPolicy = "fail"       // Fail entire execution
	ErrorPolicyContinue   ErrorPolicy = "continue"   // Continue with other verbs
	ErrorPolicyRetry      ErrorPolicy = "retry"      // Retry failed verbs
	ErrorPolicyCompensate ErrorPolicy = "compensate" // Run compensation verbs
)

// TypeSystem manages type information and validation
type TypeSystem struct {
	types     map[string]*TypeDefinition
	functions map[string]*FunctionDefinition
	registry  *schema.Registry
}

// TypeDefinition represents a type in the system
type TypeDefinition struct {
	Name        string                 `json:"name"`
	Type        string                 `json:"type"` // "primitive", "object", "array", "union"
	Properties  map[string]interface{} `json:"properties,omitempty"`
	ElementType *TypeDefinition        `json:"elementType,omitempty"`
	UnionTypes  []*TypeDefinition      `json:"unionTypes,omitempty"`
	Constraints []TypeConstraint       `json:"constraints,omitempty"`
}

// FunctionDefinition represents a function signature
type FunctionDefinition struct {
	Name       string
	InputTypes []string
	OutputType string
	Pure       bool // Whether function has side effects
}

// ExtensionCompiler orchestrates the compilation process for extensions
type ExtensionCompiler struct {
	typeSystem    *TypeSystem
	validators    []Validator
	optimizers    []Optimizer
	errorReporter *ErrorReporter
}

// NewExtensionCompiler creates a new extension compiler instance
func NewExtensionCompiler() *ExtensionCompiler {
	return &ExtensionCompiler{
		typeSystem: &TypeSystem{
			types:     make(map[string]*TypeDefinition),
			functions: make(map[string]*FunctionDefinition),
		},
		validators: []Validator{
			&TypeValidator{},
			&DependencyValidator{},
			&CapabilityValidator{},
			&SecurityValidator{},
		},
		optimizers: []Optimizer{
			&ExecutionPlanOptimizer{},
			&DependencyOptimizer{},
		},
		errorReporter: NewErrorReporter(),
	}
}

// Compile takes loaded extensions and produces a validated, executable unit
func (c *ExtensionCompiler) Compile(ctx context.Context, em *loader.ExtensionManager) (*CompilationResult, error) {
	result := &CompilationResult{
		Success:  true,
		Errors:   make([]CompilationError, 0),
		Warnings: make([]CompilationWarning, 0),
	}

	// Phase 1: Load and extract information
	registry := schema.NewRegistry()
	verbRegistry := verb.NewVerbRegistry()

	if err := schema.LoadExtensionsIntoRegistries(em, registry, verbRegistry); err != nil {
		result.Success = false
		result.Errors = append(result.Errors, CompilationError{
			Type:      "load_error",
			Component: "extension",
			Message:   fmt.Sprintf("Failed to load extensions: %v", err),
		})
		return result, nil
	}

	// Phase 2: Build type system
	c.typeSystem.registry = registry
	if err := c.buildTypeSystem(registry, verbRegistry); err != nil {
		result.Success = false
		result.Errors = append(result.Errors, CompilationError{
			Type:      "type_error",
			Component: "type_system",
			Message:   fmt.Sprintf("Failed to build type system: %v", err),
		})
		return result, nil
	}

	// Phase 3: Compile verb specifications
	compiledVerbs := make(map[string]*CompiledVerbSpec)
	compiledFunctions := make(map[string]*CompiledFunction)

	// Process verbs
	for name, verbSpec := range c.getAllVerbs(verbRegistry) {
		compiled, errs, warnings := c.compileVerbSpec(name, verbSpec)
		if len(errs) > 0 {
			result.Success = false
			result.Errors = append(result.Errors, errs...)
		}
		result.Warnings = append(result.Warnings, warnings...)

		if compiled != nil {
			compiledVerbs[name] = compiled
		}
	}

	// Phase 4: Run validators
	for _, validator := range c.validators {
		errs, warnings := validator.Validate(c.typeSystem, compiledVerbs)
		if len(errs) > 0 {
			result.Success = false
			result.Errors = append(result.Errors, errs...)
		}
		result.Warnings = append(result.Warnings, warnings...)
	}

	// Phase 5: Create execution plan
	if result.Success {
		executionPlan, err := c.createExecutionPlan(compiledVerbs)
		if err != nil {
			result.Success = false
			result.Errors = append(result.Errors, CompilationError{
				Type:      "planning_error",
				Component: "execution_plan",
				Message:   fmt.Sprintf("Failed to create execution plan: %v", err),
			})
		} else {
			// Phase 6: Optimize
			for _, optimizer := range c.optimizers {
				executionPlan = optimizer.Optimize(executionPlan)
			}

			result.CompiledUnit = &CompiledUnit{
				VerbSpecs:     compiledVerbs,
				Functions:     compiledFunctions,
				TypeSystem:    c.typeSystem,
				ExecutionPlan: executionPlan,
				Dependencies:  c.extractDependencies(compiledVerbs),
				Capabilities:  c.extractCapabilities(compiledVerbs),
			}
		}
	}

	return result, nil
}

// Helper methods

func (c *ExtensionCompiler) buildTypeSystem(registry *schema.Registry, verbRegistry *verb.VerbRegistry) error {
	// Extract type information from registry metadata
	// Implementation would scan for type definitions and build the type system
	return nil
}

func (c *ExtensionCompiler) getAllVerbs(verbRegistry *verb.VerbRegistry) map[string]*verb.StandardVerbSpec {
	// This would need to be added to VerbRegistry - a method to get all verbs
	// For now, return empty map as placeholder
	return make(map[string]*verb.StandardVerbSpec)
}

func (c *ExtensionCompiler) compileVerbSpec(name string, spec *verb.StandardVerbSpec) (*CompiledVerbSpec, []CompilationError, []CompilationWarning) {
	var errors []CompilationError
	var warnings []CompilationWarning

	// Determine executor type and config
	executorType, config, err := c.determineExecutorConfig(spec)
	if err != nil {
		errors = append(errors, CompilationError{
			Type:      "executor_error",
			Component: "verb",
			Location:  name,
			Message:   err.Error(),
		})
		return nil, errors, warnings
	}

	// Build type signature
	typeSignature := &TypeSignature{
		InputTypes: spec.ArgTypes,
		OutputType: spec.ReturnType,
	}

	// Validate type signature
	if err := c.validateTypeSignature(typeSignature); err != nil {
		errors = append(errors, CompilationError{
			Type:      "type_error",
			Component: "verb",
			Location:  name,
			Message:   fmt.Sprintf("Invalid type signature: %v", err),
		})
	}

	if len(errors) > 0 {
		return nil, errors, warnings
	}

	return &CompiledVerbSpec{
		Spec:           spec,
		ExecutorType:   executorType,
		ExecutorConfig: config,
		TypeSignature:  typeSignature,
		Dependencies:   c.extractVerbDependencies(spec),
	}, errors, warnings
}

func (c *ExtensionCompiler) determineExecutorConfig(spec *verb.StandardVerbSpec) (ExecutorType, ExecutorConfig, error) {
	// Analyze the spec to determine appropriate executor
	if spec.ExecutorImpl != nil {
		// Has implementation - use local executor
		return ExecutorLocal, &LocalExecutorConfig{
			Implementation: spec.ExecutorImpl,
		}, nil
	}

	// No implementation - need to determine from metadata or configuration
	// This would be expanded based on actual requirements
	return ExecutorMock, &MockExecutorConfig{}, nil
}

func (c *ExtensionCompiler) validateTypeSignature(sig *TypeSignature) error {
	// Validate that all types exist and are compatible
	return nil
}

func (c *ExtensionCompiler) extractVerbDependencies(spec *verb.StandardVerbSpec) []string {
	// Extract dependencies from verb specification
	return []string{}
}

func (c *ExtensionCompiler) createExecutionPlan(verbs map[string]*CompiledVerbSpec) (*ExecutionPlan, error) {
	// Create optimized execution plan based on dependencies and capabilities
	plan := &ExecutionPlan{
		Phases:       make([]ExecutionPhase, 0),
		Dependencies: make(map[string][]string),
		Capabilities: make(map[string][]string),
		Executors:    make(map[string]ExecutorConfig),
	}

	// Build dependency graph and create execution phases
	// Implementation would do topological sort of dependencies

	return plan, nil
}

func (c *ExtensionCompiler) extractDependencies(verbs map[string]*CompiledVerbSpec) []string {
	deps := make(map[string]struct{})
	for _, verb := range verbs {
		for _, dep := range verb.Dependencies {
			deps[dep] = struct{}{}
		}
	}

	result := make([]string, 0, len(deps))
	for dep := range deps {
		result = append(result, dep)
	}
	return result
}

func (c *ExtensionCompiler) extractCapabilities(verbs map[string]*CompiledVerbSpec) []string {
	caps := make(map[string]struct{})
	for _, verbSpec := range verbs {
		// Extract capabilities from verb spec
		if verbSpec.Spec.Cap&verb.CapRead != 0 {
			caps["read"] = struct{}{}
		}
		if verbSpec.Spec.Cap&verb.CapWrite != 0 {
			caps["write"] = struct{}{}
		}
		if verbSpec.Spec.Cap&verb.CapCreate != 0 {
			caps["create"] = struct{}{}
		}
		if verbSpec.Spec.Cap&verb.CapDelete != 0 {
			caps["delete"] = struct{}{}
		}
	}

	result := make([]string, 0, len(caps))
	for cap := range caps {
		result = append(result, cap)
	}
	return result
}

// Validator interface for compilation validation
type Validator interface {
	Validate(typeSystem *TypeSystem, verbs map[string]*CompiledVerbSpec) ([]CompilationError, []CompilationWarning)
}

// Optimizer interface for execution plan optimization
type Optimizer interface {
	Optimize(plan *ExecutionPlan) *ExecutionPlan
}

// Placeholder implementations
type TypeValidator struct{}
type DependencyValidator struct{}
type CapabilityValidator struct{}
type SecurityValidator struct{}
type ExecutionPlanOptimizer struct{}
type DependencyOptimizer struct{}
type MockExecutorConfig struct{}

func (tv *TypeValidator) Validate(ts *TypeSystem, verbs map[string]*CompiledVerbSpec) ([]CompilationError, []CompilationWarning) {
	return nil, nil
}

func (dv *DependencyValidator) Validate(ts *TypeSystem, verbs map[string]*CompiledVerbSpec) ([]CompilationError, []CompilationWarning) {
	return nil, nil
}

func (cv *CapabilityValidator) Validate(ts *TypeSystem, verbs map[string]*CompiledVerbSpec) ([]CompilationError, []CompilationWarning) {
	return nil, nil
}

func (sv *SecurityValidator) Validate(ts *TypeSystem, verbs map[string]*CompiledVerbSpec) ([]CompilationError, []CompilationWarning) {
	return nil, nil
}

func (epo *ExecutionPlanOptimizer) Optimize(plan *ExecutionPlan) *ExecutionPlan {
	return plan
}

func (do *DependencyOptimizer) Optimize(plan *ExecutionPlan) *ExecutionPlan {
	return plan
}

func (mec *MockExecutorConfig) GetType() ExecutorType { return ExecutorMock }
func (mec *MockExecutorConfig) Validate() error       { return nil }

// ErrorReporter handles compilation error reporting
type ErrorReporter struct{}

func NewErrorReporter() *ErrorReporter {
	return &ErrorReporter{}
}
