package flow

import (
	"context"
	"fmt"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/common"
	"github.com/effectus/effectus-go/eval"
	"github.com/effectus/effectus-go/schema/verb"
)

// Spec implements the effectus.Spec interface for flow rules
type Spec struct {
	Name       string
	Flows      []*CompiledFlow
	FactPaths  []string
	VerbSystem *verb.UnifiedVerbSystem // Use unified verb system
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
	// Sort flows by priority using common utility
	flows := common.SortByPriorityFunc(s.Flows, func(flow *CompiledFlow) int {
		return flow.Priority
	})

	// Create an executor wrapper that uses unified verb system when available
	executor := &unifiedExecutor{
		ctx:        ctx,
		executor:   ex,
		verbSystem: s.VerbSystem,
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

// GetPriority implements the common.Prioritized interface
func (cf *CompiledFlow) GetPriority() int {
	return cf.Priority
}

// sortFlowsByPriority is now replaced by common.SortByPriorityFunc
// This eliminates code duplication across different spec types

// unifiedExecutor wraps an Executor to use the unified verb system when available
type unifiedExecutor struct {
	ctx        context.Context
	executor   effectus.Executor
	verbSystem *verb.UnifiedVerbSystem
}

// Do executes an effect, using unified verb system if available
func (e *unifiedExecutor) Do(effect effectus.Effect) (interface{}, error) {
	// Check if context is cancelled
	if e.ctx.Err() != nil {
		return nil, e.ctx.Err()
	}

	// Use unified verb system if available for better validation and capability checking
	if e.verbSystem != nil {
		// Validate that this verb can be executed
		if args, ok := effect.Payload.(map[string]interface{}); ok {
			if err := e.verbSystem.CanExecute(effect.Verb, args); err != nil {
				return nil, fmt.Errorf("verb validation failed: %w", err)
			}

			// Create properly validated effect
			validatedEffect, err := e.verbSystem.CreateEffect(effect.Verb, args)
			if err != nil {
				return nil, fmt.Errorf("error creating validated effect: %w", err)
			}

			// Execute using unified system
			return e.verbSystem.Execute(e.ctx, validatedEffect)
		}
	}

	// Fallback to legacy executor
	return e.executor.Do(effect)
}
