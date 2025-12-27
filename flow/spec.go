package flow

import (
	"context"
	"fmt"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/common"
	"github.com/effectus/effectus-go/schema"
	"github.com/effectus/effectus-go/schema/capability"
	"github.com/effectus/effectus-go/schema/types"
)

// Spec implements the effectus.Spec interface for flow rules
type Spec struct {
	Name         string
	Flows        []*CompiledFlow
	FactPaths    []string
	SagaEnabled  bool                         // Whether to use saga execution
	SagaStore    schema.SagaStore             // Saga store for transaction management
	CapSystem    *capability.CapabilitySystem // Capability system for locking
	VerbRegistry common.VerbRegistry          // Verb registry for execution
}

// GetName returns the name of this spec
func (s *Spec) GetName() string {
	return s.Name
}

// RequiredFacts returns the list of fact paths required by this spec
func (s *Spec) RequiredFacts() []string {
	return s.FactPaths
}

// Execute runs all flows in the spec with saga and capability support
func (s *Spec) Execute(ctx context.Context, facts effectus.Facts, ex effectus.Executor) error {
	// Sort flows by priority using common utility (make a copy first)
	flows := make([]*CompiledFlow, len(s.Flows))
	copy(flows, s.Flows)
	common.SortByPriority(flows)

	// Create executor with saga and capability support if available
	var executor *Executor
	if s.VerbRegistry != nil {
		var options []ExecutorOption

		if s.SagaEnabled && s.SagaStore != nil {
			options = append(options, WithSaga(s.SagaStore))
		}

		if s.CapSystem != nil {
			options = append(options, WithCapabilitySystem(s.CapSystem))
		}

		executor = NewExecutor(s.VerbRegistry, options...)
	}

	// Run each flow
	for _, flow := range flows {
		// Check if the context is cancelled
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Evaluate flow predicates using facts directly
		if !schema.EvaluatePredicatesWithFacts(flow.Predicates, facts) {
			continue
		}

		fmt.Printf("Flow %s matches, executing program\n", flow.Name)

		// Execute the program based on available execution modes
		if executor != nil {
			// Use the enhanced executor with saga/capability support
			if err := s.executeFlowWithEnhancedExecutor(ctx, flow, facts, executor); err != nil {
				return fmt.Errorf("error executing flow %s: %w", flow.Name, err)
			}
		} else {
			// Fallback to simple execution
			if err := s.executeFlowSimple(ctx, flow, facts, ex); err != nil {
				return fmt.Errorf("error executing flow %s: %w", flow.Name, err)
			}
		}
	}

	return nil
}

// executeFlowWithEnhancedExecutor executes a flow using the enhanced executor with saga/capability support
func (s *Spec) executeFlowWithEnhancedExecutor(ctx context.Context, flow *CompiledFlow, facts effectus.Facts, executor *Executor) error {
	// Convert facts to common.Facts for the executor
	commonFacts := &factsAdapter{facts: facts}

	// Execute the program
	_, err := executor.ExecuteProgram(ctx, flow.Name, flow.Program, commonFacts)
	return err
}

// executeFlowSimple executes a flow using simple execution without saga/capability support
func (s *Spec) executeFlowSimple(ctx context.Context, flow *CompiledFlow, facts effectus.Facts, ex effectus.Executor) error {
	// Execute the program using standard executor
	_, err := Run(flow.Program, ex)
	return err
}

// CompiledFlow represents a flow after compilation
type CompiledFlow struct {
	Name       string
	Priority   int
	Predicates []*schema.Predicate
	Program    *Program
	FactPaths  []string
	SourceFile string
}

// GetPriority implements the common.Prioritized interface
func (cf *CompiledFlow) GetPriority() int {
	return cf.Priority
}

// factsAdapter adapts effectus.Facts to common.Facts for the executor
type factsAdapter struct {
	facts effectus.Facts
}

// Get implements common.Facts
func (fa *factsAdapter) Get(path string) (interface{}, bool) {
	return fa.facts.Get(path)
}

// GetWithContext implements common.Facts (required by the interface)
func (fa *factsAdapter) GetWithContext(path string) (interface{}, *common.ResolutionResult) {
	value, exists := fa.facts.Get(path)
	return value, &common.ResolutionResult{
		Exists: exists,
		Path:   path,
		Value:  value,
	}
}

// HasPath implements common.Facts (required by the interface)
func (fa *factsAdapter) HasPath(path string) bool {
	_, exists := fa.facts.Get(path)
	return exists
}

// Schema implements common.Facts
func (fa *factsAdapter) Schema() common.SchemaInfo {
	return &schemaAdapter{schema: fa.facts.Schema()}
}

// schemaAdapter adapts effectus.SchemaInfo to common.SchemaInfo
type schemaAdapter struct {
	schema effectus.SchemaInfo
}

// ValidatePath implements common.SchemaInfo
func (sa *schemaAdapter) ValidatePath(path string) bool {
	return sa.schema.ValidatePath(path)
}

// GetPathType implements common.SchemaInfo (required by the interface)
func (sa *schemaAdapter) GetPathType(path string) *types.Type {
	// Simple implementation - in practice this would be more sophisticated
	if sa.schema.ValidatePath(path) {
		// Return a generic type since we don't have type information from effectus.SchemaInfo
		return &types.Type{Name: "unknown"}
	}
	return nil
}

// RegisterPathType implements common.SchemaInfo (required by the interface)
func (sa *schemaAdapter) RegisterPathType(path string, typ *types.Type) {
	// No-op for now since effectus.SchemaInfo doesn't support registration
}
