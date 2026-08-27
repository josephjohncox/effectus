package compiler

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/effectus/effectus-go/ir"
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
	VerbSpecs         map[string]*CompiledVerbSpec
	Functions         map[string]*CompiledFunction
	TypeSystem        *TypeSystem
	ExecutionPlan     *ExecutionPlan
	CheckedIR         *ir.Checked
	IREnvironment     ir.Environment
	InitialData       map[string]interface{}
	Dependencies      []string // External dependencies required
	Capabilities      []string // Required capabilities
	ExtensionSnapshot *loader.ExtensionSnapshot
}

// CompiledVerbSpec represents a validated verb specification
type CompiledVerbSpec struct {
	Spec            *verb.Spec
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
	URL                 string            `json:"url"`
	Method              string            `json:"method"`
	Headers             map[string]string `json:"headers"`
	Timeout             string            `json:"timeout"`
	AllowPrivateNetwork bool              `json:"allowPrivateNetwork,omitempty"`
	RetryPolicy         *RetryPolicy      `json:"retryPolicy,omitempty"`
}

func (hec *HTTPExecutorConfig) GetType() ExecutorType { return ExecutorHTTP }
func (hec *HTTPExecutorConfig) Validate() error {
	if hec.URL == "" {
		return fmt.Errorf("HTTP executor requires URL")
	}
	if _, err := (loader.OutboundNetworkPolicy{AllowPrivate: hec.AllowPrivateNetwork}).ValidateURL(hec.URL); err != nil {
		return fmt.Errorf("HTTP executor URL: %w", err)
	}
	if hec.Method == "" {
		hec.Method = "POST"
	}
	if strings.TrimSpace(hec.Timeout) != "" {
		timeout, err := time.ParseDuration(hec.Timeout)
		if err != nil || timeout <= 0 {
			return fmt.Errorf("HTTP executor timeout must be a positive duration")
		}
	}
	return nil
}

// GRPCExecutorConfig for gRPC-based execution
type GRPCExecutorConfig struct {
	Address     string            `json:"address"`
	Method      string            `json:"method"` // Fully-qualified method, e.g. /package.Service/Call
	Timeout     string            `json:"timeout"`
	Metadata    map[string]string `json:"metadata"`
	UseTLS      bool              `json:"useTLS"`
	Insecure    bool              `json:"insecure,omitempty"`
	ServerName  string            `json:"serverName,omitempty"`
	RetrySafe   bool              `json:"retrySafe,omitempty"`
	RetryPolicy *RetryPolicy      `json:"retryPolicy,omitempty"`
}

func (gec *GRPCExecutorConfig) GetType() ExecutorType { return ExecutorGRPC }
func (gec *GRPCExecutorConfig) Validate() error {
	if gec.Address == "" {
		return fmt.Errorf("gRPC executor requires address")
	}
	if gec.Method == "" {
		return fmt.Errorf("gRPC executor requires method")
	}
	if strings.TrimSpace(gec.Timeout) != "" {
		timeout, err := time.ParseDuration(gec.Timeout)
		if err != nil || timeout <= 0 {
			return fmt.Errorf("gRPC executor timeout must be a positive duration")
		}
	}
	if !gec.UseTLS && !gec.Insecure {
		return fmt.Errorf("gRPC executor requires TLS unless insecure is explicitly enabled")
	}
	if gec.UseTLS && gec.Insecure {
		return fmt.Errorf("gRPC executor TLS and insecure modes are mutually exclusive")
	}
	return nil
}

// MessageExecutorConfig for message queue execution
type MessageExecutorConfig struct {
	Publisher           string            `json:"publisher,omitempty"` // "kafka" or "http"
	Brokers             []string          `json:"brokers,omitempty"`
	URL                 string            `json:"url,omitempty"`
	Headers             map[string]string `json:"headers,omitempty"`
	Topic               string            `json:"topic"`
	Queue               string            `json:"queue"`
	Exchange            string            `json:"exchange"`
	RoutingKey          string            `json:"routingKey"`
	Timeout             string            `json:"timeout"`
	AllowPrivateNetwork bool              `json:"allowPrivateNetwork,omitempty"`
	RetryPolicy         *RetryPolicy      `json:"retryPolicy,omitempty"`
}

func (mec *MessageExecutorConfig) GetType() ExecutorType { return ExecutorMessage }
func (mec *MessageExecutorConfig) Validate() error {
	if mec.Publisher == "" {
		switch {
		case mec.URL != "":
			mec.Publisher = "http"
		case len(mec.Brokers) > 0:
			mec.Publisher = "kafka"
		default:
			mec.Publisher = "stdout"
		}
	}

	switch mec.Publisher {
	case "http":
		if mec.URL == "" {
			return fmt.Errorf("message executor requires url for http publisher")
		}
		if _, err := (loader.OutboundNetworkPolicy{AllowPrivate: mec.AllowPrivateNetwork}).ValidateURL(mec.URL); err != nil {
			return fmt.Errorf("message HTTP URL: %w", err)
		}
	case "kafka":
		if len(mec.Brokers) == 0 {
			return fmt.Errorf("message executor requires brokers for kafka publisher")
		}
		if mec.Topic == "" {
			return fmt.Errorf("message executor requires topic for kafka publisher")
		}
	case "stdout":
		return nil
	default:
		if mec.Topic == "" && mec.Queue == "" {
			return fmt.Errorf("message executor requires topic or queue")
		}
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
	Name               string
	Implementation     interface{}
	ResolverDescriptor any
	TypeSignature      *TypeSignature
	Dependencies       []string
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
	validators    []Validator
	optimizers    []Optimizer
	errorReporter *ErrorReporter
}

// NewExtensionCompiler creates a new extension compiler instance
func NewExtensionCompiler() *ExtensionCompiler {
	return &ExtensionCompiler{
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

// Compile stages mutable loaders before it compiles. Production callers should
// use Stage and CompileSnapshot as separate bounded phases.
func (c *ExtensionCompiler) Compile(ctx context.Context, em *loader.ExtensionManager) (*CompilationResult, error) {
	if em == nil {
		return nil, fmt.Errorf("extension manager is required")
	}
	snapshot, err := em.Stage(ctx, loader.StageOptions{})
	if err != nil {
		return nil, err
	}
	result, err := c.CompileSnapshot(ctx, snapshot)
	if err != nil || result == nil || !result.Success {
		_ = snapshot.Retire()
		return result, err
	}
	return result, nil
}

// CompileSnapshot compiles only immutable in-memory loader output. It does not
// call mutable filesystem, HTTP, DNS, or OCI loaders.
func (c *ExtensionCompiler) CompileSnapshot(ctx context.Context, snapshot *loader.ExtensionSnapshot) (*CompilationResult, error) {
	result := &CompilationResult{
		Success:  true,
		Errors:   make([]CompilationError, 0),
		Warnings: make([]CompilationWarning, 0),
	}

	if snapshot == nil {
		return nil, fmt.Errorf("extension snapshot is required")
	}
	// Phase 1: Load immutable data into candidate-only registries.
	registry := schema.NewRegistry()
	verbRegistry := verb.NewRegistry(registry)
	candidateTarget := newExtensionCandidateTarget(registry, verbRegistry)

	if err := snapshot.Load(ctx, candidateTarget); err != nil {
		result.Success = false
		result.Errors = append(result.Errors, CompilationError{
			Type:      "load_error",
			Component: "extension",
			Message:   fmt.Sprintf("Failed to load extensions: %v", err),
		})
		return result, nil
	}

	// Phase 2: Build a candidate type system. Failed candidates never mutate a published unit.
	candidateTypeSystem := &TypeSystem{
		types: make(map[string]*TypeDefinition), functions: make(map[string]*FunctionDefinition), registry: registry,
	}
	if err := c.buildTypeSystem(candidateTypeSystem, registry, verbRegistry); err != nil {
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
	compiledFunctions := make(map[string]*CompiledFunction, len(candidateTarget.functions))
	for name, implementation := range candidateTarget.functions {
		var descriptor any
		if provider, ok := implementation.(CheckedFunctionProvider); ok {
			descriptor = provider.CheckedFunctionDescriptor()
			implementation = provider.CheckedFunctionImplementation()
		}
		compiledFunctions[name] = &CompiledFunction{Name: name, Implementation: implementation, ResolverDescriptor: descriptor}
	}

	// Process verbs in a stable order.
	allVerbs := c.getAllVerbs(verbRegistry)
	verbNames := make([]string, 0, len(allVerbs))
	for name := range allVerbs {
		verbNames = append(verbNames, name)
	}
	sort.Strings(verbNames)
	for _, name := range verbNames {
		compiled, errs, warnings := c.compileVerbSpec(name, allVerbs[name])
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
		errs, warnings := validator.Validate(candidateTypeSystem, compiledVerbs)
		if len(errs) > 0 {
			result.Success = false
			result.Errors = append(result.Errors, errs...)
		}
		result.Warnings = append(result.Warnings, warnings...)
	}

	// Phase 5: Invoke the same checked compiler used by standalone sources.
	if result.Success {
		environment, err := buildExtensionEnvironment(candidateTarget, compiledVerbs)
		if err == nil {
			sources := make([]Source, len(candidateTarget.sources))
			for index, source := range candidateTarget.sources {
				sources[index] = Source{Path: source.Path, Data: append([]byte(nil), source.Data...)}
			}
			var checked *ir.Checked
			checked, err = CompileChecked(ctx, sources, environment, CompileOptions{})
			if err == nil {
				executionPlan := compatibilityExecutionPlan(checked)
				for name, compiledVerb := range compiledVerbs {
					executionPlan.Executors[name] = compiledVerb.ExecutorConfig
					executionPlan.Dependencies[name] = append([]string(nil), compiledVerb.Dependencies...)
					executionPlan.Capabilities[name] = capabilityNames(compiledVerb.Spec.Capability)
				}
				for _, optimizer := range c.optimizers {
					executionPlan = optimizer.Optimize(executionPlan)
				}
				result.CompiledUnit = &CompiledUnit{
					VerbSpecs: compiledVerbs, Functions: compiledFunctions, TypeSystem: candidateTypeSystem,
					ExecutionPlan: executionPlan, CheckedIR: checked, IREnvironment: environment,
					InitialData:  cloneInterfaceMap(candidateTarget.initialData),
					Dependencies: c.extractDependencies(compiledVerbs), Capabilities: c.extractCapabilities(compiledVerbs),
					ExtensionSnapshot: snapshot,
				}
			}
		}
		if err != nil {
			result.Success = false
			result.Errors = append(result.Errors, CompilationError{
				Type: "planning_error", Component: "execution_plan", Message: fmt.Sprintf("Failed to create checked execution plan: %v", err),
			})
		}
	}

	return result, nil
}

func compatibilityExecutionPlan(checked *ir.Checked) *ExecutionPlan {
	plan := &ExecutionPlan{
		Dependencies: make(map[string][]string), Capabilities: make(map[string][]string), Executors: make(map[string]ExecutorConfig),
	}
	if checked == nil {
		return plan
	}
	for _, checkedPlan := range checked.CloneArtifact().Plans {
		verbs := make([]string, 0, len(checkedPlan.Steps))
		for _, step := range checkedPlan.Steps {
			verbs = append(verbs, step.Verb)
		}
		plan.Phases = append(plan.Phases, ExecutionPhase{Name: checkedPlan.Id, Verbs: verbs, ErrorPolicy: ErrorPolicyFail})
	}
	return plan
}

// Helper methods

func (c *ExtensionCompiler) buildTypeSystem(typeSystem *TypeSystem, registry *schema.Registry, verbRegistry *verb.Registry) error {
	if typeSystem == nil || registry == nil || verbRegistry == nil {
		return fmt.Errorf("candidate type system and registries are required")
	}
	return nil
}

func (c *ExtensionCompiler) getAllVerbs(verbRegistry *verb.Registry) map[string]*verb.Spec {
	allVerbs := verbRegistry.GetAllVerbs()
	result := make(map[string]*verb.Spec, len(allVerbs))

	for _, spec := range allVerbs {
		result[spec.Name] = spec
	}

	return result
}

func (c *ExtensionCompiler) compileVerbSpec(name string, spec *verb.Spec) (*CompiledVerbSpec, []CompilationError, []CompilationWarning) {
	var errors []CompilationError
	var warnings []CompilationWarning

	// Determine executor type and config
	executorType, config, err := c.determineExecutorConfig(spec)
	if err == nil && config != nil {
		err = config.Validate()
	}
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

func (c *ExtensionCompiler) determineExecutorConfig(spec *verb.Spec) (ExecutorType, ExecutorConfig, error) {
	// Analyze the spec to determine appropriate executor
	if spec.Executor != nil {
		// Has implementation - use local executor
		return ExecutorLocal, &LocalExecutorConfig{
			Implementation: spec.Executor,
		}, nil
	}

	return ExecutorType(""), nil, fmt.Errorf("verb %q has no executable implementation", spec.Name)
}

func (c *ExtensionCompiler) validateTypeSignature(sig *TypeSignature) error {
	if sig == nil {
		return fmt.Errorf("type signature is required")
	}
	if strings.TrimSpace(sig.OutputType) == "" {
		return fmt.Errorf("output type is required")
	}
	for name, typeName := range sig.InputTypes {
		if strings.TrimSpace(name) == "" || name != strings.TrimSpace(name) {
			return fmt.Errorf("invalid argument name %q", name)
		}
		if strings.TrimSpace(typeName) == "" {
			return fmt.Errorf("argument %q type is required", name)
		}
	}
	return nil
}

func (c *ExtensionCompiler) extractVerbDependencies(spec *verb.Spec) []string {
	// Extract dependencies from verb specification
	return []string{}
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
	sort.Strings(result)
	return result
}

func capabilityNames(capability verb.Capability) []string {
	var names []string
	checks := []struct {
		value verb.Capability
		name  string
	}{
		{verb.CapRead, "read"}, {verb.CapWrite, "write"}, {verb.CapCreate, "create"}, {verb.CapDelete, "delete"},
		{verb.CapIdempotent, "idempotent"}, {verb.CapExclusive, "exclusive"}, {verb.CapCommutative, "commutative"},
	}
	for _, check := range checks {
		if capability&check.value != 0 {
			names = append(names, check.name)
		}
	}
	return names
}

func (c *ExtensionCompiler) extractCapabilities(verbs map[string]*CompiledVerbSpec) []string {
	caps := make(map[string]struct{})
	for _, verbSpec := range verbs {
		// Extract capabilities from verb spec
		if verbSpec.Spec.Capability&verb.CapRead != 0 {
			caps["read"] = struct{}{}
		}
		if verbSpec.Spec.Capability&verb.CapWrite != 0 {
			caps["write"] = struct{}{}
		}
		if verbSpec.Spec.Capability&verb.CapCreate != 0 {
			caps["create"] = struct{}{}
		}
		if verbSpec.Spec.Capability&verb.CapDelete != 0 {
			caps["delete"] = struct{}{}
		}
	}

	result := make([]string, 0, len(caps))
	for cap := range caps {
		result = append(result, cap)
	}
	sort.Strings(result)
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
	var failures []CompilationError
	if ts == nil {
		return []CompilationError{{Type: "type_error", Component: "type_system", Message: "candidate type system is required"}}, nil
	}
	for name, compiled := range verbs {
		if compiled == nil || compiled.Spec == nil {
			failures = append(failures, CompilationError{Type: "type_error", Component: "verb", Location: name, Message: "compiled verb specification is required"})
			continue
		}
		seen := map[string]struct{}{}
		for _, required := range compiled.Spec.RequiredArgs {
			if _, duplicate := seen[required]; duplicate {
				failures = append(failures, CompilationError{Type: "type_error", Component: "verb", Location: name, Message: fmt.Sprintf("required argument %q is duplicated", required)})
			}
			seen[required] = struct{}{}
			if _, ok := compiled.Spec.ArgTypes[required]; !ok {
				failures = append(failures, CompilationError{Type: "type_error", Component: "verb", Location: name, Message: fmt.Sprintf("required argument %q is not declared", required)})
			}
		}
	}
	return failures, nil
}

func (dv *DependencyValidator) Validate(_ *TypeSystem, verbs map[string]*CompiledVerbSpec) ([]CompilationError, []CompilationWarning) {
	var failures []CompilationError
	for name, compiled := range verbs {
		if compiled == nil || compiled.Spec == nil || compiled.Spec.Inverse == "" {
			continue
		}
		if _, ok := verbs[compiled.Spec.Inverse]; !ok {
			failures = append(failures, CompilationError{Type: "dependency_error", Component: "verb", Location: name, Message: fmt.Sprintf("inverse verb %q is not registered", compiled.Spec.Inverse)})
		}
	}
	return failures, nil
}

func (cv *CapabilityValidator) Validate(_ *TypeSystem, verbs map[string]*CompiledVerbSpec) ([]CompilationError, []CompilationWarning) {
	var failures []CompilationError
	for name, compiled := range verbs {
		if compiled == nil || compiled.Spec == nil {
			continue
		}
		capability := compiled.Spec.Capability
		if capability&verb.CapExclusive != 0 && capability&verb.CapCommutative != 0 {
			failures = append(failures, CompilationError{Type: "capability_error", Component: "verb", Location: name, Message: "exclusive and commutative capabilities conflict"})
			continue
		}
		for _, resource := range compiled.Spec.Resources {
			if resource.Cap&capability != resource.Cap {
				failures = append(failures, CompilationError{Type: "capability_error", Component: "verb", Location: name, Message: fmt.Sprintf("resource %q exceeds verb capabilities", resource.Resource)})
			}
		}
	}
	return failures, nil
}

func (sv *SecurityValidator) Validate(_ *TypeSystem, verbs map[string]*CompiledVerbSpec) ([]CompilationError, []CompilationWarning) {
	var failures []CompilationError
	for name, compiled := range verbs {
		if compiled == nil || compiled.ExecutorConfig == nil {
			failures = append(failures, CompilationError{Type: "security_error", Component: "verb", Location: name, Message: "executor configuration is required"})
			continue
		}
		if err := compiled.ExecutorConfig.Validate(); err != nil {
			failures = append(failures, CompilationError{Type: "security_error", Component: "verb", Location: name, Message: err.Error()})
		}
	}
	return failures, nil
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
