package flow

import (
	"context"
	"fmt"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/eval"
)

// Spec implements the effectus.Spec interface for flow rules
type Spec struct {
	Name      string
	Flows     []*CompiledFlow
	FactPaths []string
}

// GetName returns the name of this spec
func (s *Spec) GetName() string {
	return s.Name
}

// RequiredFacts returns the list of fact paths required by this spec
func (s *Spec) RequiredFacts() []string {
	return s.FactPaths
}

// Execute runs all flows in the spec
func (s *Spec) Execute(ctx context.Context, facts effectus.Facts, ex effectus.Executor) error {
	// Sort flows by priority
	flows := sortFlowsByPriority(s.Flows)

	// Create an executor wrapper to handle context cancellation
	executor := &contextExecutor{
		ctx:      ctx,
		executor: ex,
	}

	// Run each flow
	for _, flow := range flows {
		// Check if the context is cancelled
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Evaluate flow predicates
		if !eval.EvaluatePredicates(flow.Predicates, facts) {
			continue
		}

		// Execute the program
		_, err := Run(flow.Program, executor)
		if err != nil {
			return fmt.Errorf("error executing flow %s: %w", flow.Name, err)
		}
	}

	return nil
}

// CompiledFlow represents a flow after compilation
type CompiledFlow struct {
	Name       string
	Priority   int
	Predicates []*eval.Predicate
	Program    *Program
	FactPaths  []string
	SourceFile string
}

// sortFlowsByPriority sorts flows by priority (highest first)
func sortFlowsByPriority(flows []*CompiledFlow) []*CompiledFlow {
	// Copy flows to avoid modifying the original
	sortedFlows := make([]*CompiledFlow, len(flows))
	copy(sortedFlows, flows)

	// Sort by priority (highest first)
	for i := 0; i < len(sortedFlows); i++ {
		for j := i + 1; j < len(sortedFlows); j++ {
			if sortedFlows[i].Priority < sortedFlows[j].Priority {
				sortedFlows[i], sortedFlows[j] = sortedFlows[j], sortedFlows[i]
			}
		}
	}

	return sortedFlows
}

// contextExecutor wraps an Executor to check for context cancellation
type contextExecutor struct {
	ctx      context.Context
	executor effectus.Executor
}

// Do executes an effect, checking for context cancellation first
func (e *contextExecutor) Do(effect effectus.Effect) (interface{}, error) {
	// Check if context is cancelled
	if e.ctx.Err() != nil {
		return nil, e.ctx.Err()
	}

	// Execute the effect
	return e.executor.Do(effect)
}
