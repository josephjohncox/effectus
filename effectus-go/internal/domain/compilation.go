package domain

import (
	"context"
	"fmt"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/ast"
	"github.com/effectus/effectus-go/flow"
	"github.com/effectus/effectus-go/list"
	"github.com/effectus/effectus-go/schema/verb"
)

// CompilationService handles the business logic of rule compilation
type CompilationService struct {
	verbSystem *verb.UnifiedVerbSystem
}

// NewCompilationService creates a new compilation service
func NewCompilationService(verbSystem *verb.UnifiedVerbSystem) *CompilationService {
	return &CompilationService{
		verbSystem: verbSystem,
	}
}

// CompilationRequest represents a request to compile rules
type CompilationRequest struct {
	ParsedFiles []*ast.File
	Schema      effectus.SchemaInfo
	Name        string
}

// CompilationResult represents the result of compilation
type CompilationResult struct {
	Spec          effectus.Spec
	RequiredFacts []string
	Diagnostics   []CompilationDiagnostic
}

// CompilationDiagnostic represents a warning or error during compilation
type CompilationDiagnostic struct {
	Level   DiagnosticLevel
	Message string
	File    string
	Line    int
}

// DiagnosticLevel represents the severity of a diagnostic
type DiagnosticLevel int

const (
	DiagnosticInfo DiagnosticLevel = iota
	DiagnosticWarning
	DiagnosticError
)

// CompileRules compiles parsed files into a unified specification
func (cs *CompilationService) CompileRules(req CompilationRequest) (*CompilationResult, error) {
	// Separate files by type
	listFiles := []*ast.File{}
	flowFiles := []*ast.File{}

	for _, file := range req.ParsedFiles {
		if len(file.Rules) > 0 {
			listFiles = append(listFiles, file)
		}
		if len(file.Flows) > 0 {
			flowFiles = append(flowFiles, file)
		}
	}

	// Compile list files
	var listSpec *list.Spec
	if len(listFiles) > 0 {
		compiled, err := cs.compileListFiles(listFiles, req.Schema)
		if err != nil {
			return nil, err
		}
		listSpec = compiled
	}

	// Compile flow files
	var flowSpec *flow.Spec
	if len(flowFiles) > 0 {
		compiled, err := cs.compileFlowFiles(flowFiles, req.Schema)
		if err != nil {
			return nil, err
		}
		flowSpec = compiled
	}

	// Create unified spec
	unifiedSpec := NewUnifiedSpec(listSpec, flowSpec, cs.verbSystem, req.Name)

	// Collect required facts
	requiredFacts := unifiedSpec.RequiredFacts()

	result := &CompilationResult{
		Spec:          unifiedSpec,
		RequiredFacts: requiredFacts,
		Diagnostics:   []CompilationDiagnostic{}, // TODO: Collect diagnostics during compilation
	}

	return result, nil
}

// compileListFiles compiles list-style rule files
func (cs *CompilationService) compileListFiles(files []*ast.File, schema effectus.SchemaInfo) (*list.Spec, error) {
	// Domain logic for list compilation
	// This is where business rules for list compilation would go

	// For now, use a simplified approach since we're focusing on architecture
	// TODO: Extract business logic from list.Compiler into this domain service

	merged := &list.Spec{
		Rules:      []*list.CompiledRule{},
		FactPaths:  []string{},
		VerbSystem: cs.verbSystem,
	}

	// This is a placeholder - actual compilation logic would go here
	// For now, return empty spec to avoid dependency on non-existent methods
	return merged, nil
}

// compileFlowFiles compiles flow-style rule files
func (cs *CompilationService) compileFlowFiles(files []*ast.File, schema effectus.SchemaInfo) (*flow.Spec, error) {
	// Domain logic for flow compilation
	// This is where business rules for flow compilation would go

	// For now, use a simplified approach since we're focusing on architecture
	// TODO: Extract business logic from flow.Compiler into this domain service

	merged := &flow.Spec{
		Flows:      []*flow.CompiledFlow{},
		FactPaths:  []string{},
		VerbSystem: cs.verbSystem,
	}

	// This is a placeholder - actual compilation logic would go here
	// For now, return empty spec to avoid dependency on non-existent methods
	return merged, nil
}

// mergeListSpecs merges multiple list specs into a single one
func (cs *CompilationService) mergeListSpecs(specs []effectus.Spec) *list.Spec {
	if len(specs) == 0 {
		return nil
	}

	merged := &list.Spec{
		Rules:      []*list.CompiledRule{},
		FactPaths:  []string{},
		VerbSystem: cs.verbSystem, // Inject verb system
	}

	factPathSet := make(map[string]struct{})

	for _, spec := range specs {
		listSpec, ok := spec.(*list.Spec)
		if !ok {
			continue
		}

		// Add rules
		merged.Rules = append(merged.Rules, listSpec.Rules...)

		// Collect fact paths
		for _, path := range listSpec.FactPaths {
			factPathSet[path] = struct{}{}
		}
	}

	// Extract unique fact paths
	for path := range factPathSet {
		merged.FactPaths = append(merged.FactPaths, path)
	}

	return merged
}

// mergeFlowSpecs merges multiple flow specs into a single one
func (cs *CompilationService) mergeFlowSpecs(specs []effectus.Spec) *flow.Spec {
	if len(specs) == 0 {
		return nil
	}

	merged := &flow.Spec{
		Flows:      []*flow.CompiledFlow{},
		FactPaths:  []string{},
		VerbSystem: cs.verbSystem, // Inject verb system
	}

	factPathSet := make(map[string]struct{})

	for _, spec := range specs {
		flowSpec, ok := spec.(*flow.Spec)
		if !ok {
			continue
		}

		// Add flows
		merged.Flows = append(merged.Flows, flowSpec.Flows...)

		// Collect fact paths
		for _, path := range flowSpec.FactPaths {
			factPathSet[path] = struct{}{}
		}
	}

	// Extract unique fact paths
	for path := range factPathSet {
		merged.FactPaths = append(merged.FactPaths, path)
	}

	return merged
}

// UnifiedSpec represents a unified specification with clean separation
type UnifiedSpec struct {
	listSpec   *list.Spec
	flowSpec   *flow.Spec
	verbSystem *verb.UnifiedVerbSystem
	name       string
}

// NewUnifiedSpec creates a new unified spec with proper dependency injection
func NewUnifiedSpec(listSpec *list.Spec, flowSpec *flow.Spec, verbSystem *verb.UnifiedVerbSystem, name string) *UnifiedSpec {
	return &UnifiedSpec{
		listSpec:   listSpec,
		flowSpec:   flowSpec,
		verbSystem: verbSystem,
		name:       name,
	}
}

// GetName implements effectus.Spec
func (us *UnifiedSpec) GetName() string {
	return us.name
}

// RequiredFacts implements effectus.Spec
func (us *UnifiedSpec) RequiredFacts() []string {
	factPathSet := make(map[string]struct{})

	// Add list spec fact paths
	if us.listSpec != nil {
		for _, path := range us.listSpec.FactPaths {
			factPathSet[path] = struct{}{}
		}
	}

	// Add flow spec fact paths
	if us.flowSpec != nil {
		for _, path := range us.flowSpec.FactPaths {
			factPathSet[path] = struct{}{}
		}
	}

	// Extract unique fact paths
	factPaths := make([]string, 0, len(factPathSet))
	for path := range factPathSet {
		factPaths = append(factPaths, path)
	}

	return factPaths
}

// Execute implements effectus.Spec with clean business logic
func (us *UnifiedSpec) Execute(ctx context.Context, facts effectus.Facts, ex effectus.Executor) error {
	// Business logic: Execute in priority order (list first, then flows)
	// This separation allows us to change execution strategy without touching infrastructure

	// Execute list spec if available
	if us.listSpec != nil {
		if err := us.listSpec.Execute(ctx, facts, ex); err != nil {
			return NewExecutionError("list", err)
		}
	}

	// Execute flow spec if available
	if us.flowSpec != nil {
		if err := us.flowSpec.Execute(ctx, facts, ex); err != nil {
			return NewExecutionError("flow", err)
		}
	}

	return nil
}

// ExecutionError represents an error during execution with context
type ExecutionError struct {
	SpecType string
	Cause    error
}

// NewExecutionError creates a new execution error
func NewExecutionError(specType string, cause error) *ExecutionError {
	return &ExecutionError{
		SpecType: specType,
		Cause:    cause,
	}
}

// Error implements the error interface
func (ee *ExecutionError) Error() string {
	return fmt.Sprintf("%s spec execution error: %v", ee.SpecType, ee.Cause)
}

// Unwrap allows errors.Unwrap to work
func (ee *ExecutionError) Unwrap() error {
	return ee.Cause
}
