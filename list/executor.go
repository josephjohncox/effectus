// list/executor.go
package list

import (
	"context"
	"fmt"

	eff "github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/common"
	"github.com/effectus/effectus-go/flow"
	"github.com/effectus/effectus-go/schema"
	"github.com/effectus/effectus-go/schema/capability"
)

// ExecutorOption defines an option for configuring the executor
type ExecutorOption func(*Executor)

// WithSaga enables saga-style compensation for failed executions
func WithSaga(store schema.SagaStore) ExecutorOption {
	return func(e *Executor) {
		e.sagaStore = store
		e.sagaEnabled = true
	}
}

// WithCapabilitySystem enables capability-based locking
func WithCapabilitySystem(capSystem *capability.CapabilitySystem) ExecutorOption {
	return func(e *Executor) {
		e.capSystem = capSystem
	}
}

// Executor is the main executor for list rules with saga and capability support
type Executor struct {
	verbRegistry common.VerbRegistry
	capSystem    *capability.CapabilitySystem
	sagaEnabled  bool
	sagaStore    schema.SagaStore
}

// NewExecutor creates a new executor for list rules
func NewExecutor(verbRegistry common.VerbRegistry, options ...ExecutorOption) *Executor {
	executor := &Executor{
		verbRegistry: verbRegistry,
	}

	// Apply options
	for _, option := range options {
		option(executor)
	}

	return executor
}

// ExecuteRule executes a single rule against facts with saga and capability support
func (le *Executor) ExecuteRule(ctx context.Context, rule *CompiledRule, facts common.Facts) ([]eff.Effect, error) {
	// Create adapter to use with schema evaluation system
	factsAdapter := &effectusFactsAdapter{facts: facts}

	// Check if rule predicates match using the schema evaluator.
	matched, err := schema.EvaluatePredicatesWithFactsE(rule.Predicates, factsAdapter)
	if err != nil {
		return nil, fmt.Errorf("evaluating predicates for rule %s: %w", rule.Name, err)
	}
	if !matched {
		return nil, nil
	}

	// Convert effects to Program for unified execution
	effects := make([]eff.Effect, 0, len(rule.Effects))
	for _, effect := range rule.Effects {
		effects = append(effects, eff.Effect{
			Verb:    effect.Verb,
			Payload: effect.Args,
		})
	}

	// Create a Program from the effects list with transaction support
	program := flow.FromList(effects)

	if le.sagaEnabled {
		if le.sagaStore == nil {
			return nil, schema.ErrSagaStoreRequired
		}
		program = program.ToAtomic(fmt.Sprintf("rule-%s", rule.Name))
	}

	// Execute using the unified execution system
	_, err = le.executeProgram(ctx, rule.Name, program, facts)
	if err != nil {
		return nil, err
	}

	// Convert result back to effects (for backward compatibility)
	return effects, nil // Return original effects for now
}

// executeProgram executes a program with saga and capability support
func (le *Executor) executeProgram(ctx context.Context, name string, program *flow.Program, facts common.Facts) (interface{}, error) {
	if le.sagaEnabled {
		options := []flow.ExecutorOption{flow.WithSaga(le.sagaStore)}
		if le.capSystem != nil {
			options = append(options, flow.WithCapabilitySystem(le.capSystem))
		}
		return flow.NewExecutor(le.verbRegistry, options...).ExecuteProgram(ctx, name, program, facts)
	}

	var executor eff.Executor
	if le.capSystem != nil {
		executor = capability.NewCapabilityAwareExecutor(
			common.NewExecutorAdapter(le.verbRegistry, facts),
			le.capSystem,
			"list-executor",
		)
	} else {
		executor = common.NewExecutorAdapter(le.verbRegistry, facts)
	}
	return flow.RunContext(ctx, program, executor)
}

// effectusFactsAdapter adapts common.Facts to eff.Facts
type effectusFactsAdapter struct {
	facts common.Facts
}

func (f *effectusFactsAdapter) Get(path string) (interface{}, bool) {
	return f.facts.Get(path)
}

func (f *effectusFactsAdapter) Schema() eff.SchemaInfo {
	return &effectusSchemaAdapter{f.facts.Schema()}
}

// effectusSchemaAdapter adapts common.SchemaInfo to eff.SchemaInfo
type effectusSchemaAdapter struct {
	schema common.SchemaInfo
}

func (s *effectusSchemaAdapter) ValidatePath(path string) bool {
	return s.schema.ValidatePath(path)
}
