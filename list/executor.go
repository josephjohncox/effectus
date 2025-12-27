// list/executor.go
package list

import (
	"context"
	"fmt"
	"sync"
	"time"

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
	mu           sync.Mutex
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

	// Check if rule predicates match using the schema evaluator
	matched := schema.EvaluatePredicatesWithFacts(rule.Predicates, factsAdapter)
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

	// If saga is enabled, wrap in a transaction with compensation
	if le.sagaEnabled && le.sagaStore != nil {
		// Add compensation information if available
		compensations := le.extractCompensations(rule)
		if len(compensations) > 0 {
			program = program.WithCompensation(func(verb string) string {
				return compensations[verb]
			})
		}

		// Wrap in atomic transaction for saga support
		program = program.ToAtomic(fmt.Sprintf("rule-%s", rule.Name))
	}

	// Execute using the unified execution system
	_, err := le.executeProgram(ctx, rule.Name, program, facts)
	if err != nil {
		return nil, err
	}

	// Convert result back to effects (for backward compatibility)
	return effects, nil // Return original effects for now
}

// extractCompensations extracts compensation verbs from rule effects
func (le *Executor) extractCompensations(rule *CompiledRule) map[string]string {
	compensations := make(map[string]string)

	for _, effect := range rule.Effects {
		if le.verbRegistry != nil {
			if spec, exists := le.verbRegistry.GetVerb(effect.Verb); exists {
				if inverse := spec.Inverse; inverse != "" {
					compensations[effect.Verb] = inverse
				}
			}
		}
	}

	return compensations
}

// executeProgram executes a program with saga and capability support
func (le *Executor) executeProgram(ctx context.Context, name string, program *flow.Program, facts common.Facts) (interface{}, error) {
	// Create the appropriate executor based on configuration
	var executor eff.Executor

	if le.sagaEnabled && le.sagaStore != nil && le.capSystem != nil {
		// Check if program is transactional
		if program.IsTransactional() {
			// Use saga-aware execution for transactional programs
			return le.executeSagaProgram(ctx, name, program, facts)
		}

		// For non-transactional programs, use capability awareness
		executor = capability.NewCapabilityAwareExecutor(
			common.NewExecutorAdapter(le.verbRegistry, facts),
			le.capSystem,
			fmt.Sprintf("list-%s", name),
		)
	} else if le.capSystem != nil {
		// Use capability-aware executor
		executor = capability.NewCapabilityAwareExecutor(
			common.NewExecutorAdapter(le.verbRegistry, facts),
			le.capSystem,
			"list-executor",
		)
	} else {
		// Use simple executor
		executor = common.NewExecutorAdapter(le.verbRegistry, facts)
	}

	// Execute the program
	return flow.Run(program, executor)
}

// executeSagaProgram executes a transactional program with full saga support
func (le *Executor) executeSagaProgram(ctx context.Context, name string, program *flow.Program, facts common.Facts) (interface{}, error) {
	// Extract transaction information
	transactions := flow.ExtractTransactions(program)

	if len(transactions) == 0 {
		// No transactions, fall back to regular execution
		return le.executeProgram(ctx, name, program, facts)
	}

	// For now, handle the first transaction
	// TODO: Support nested transactions
	transaction := transactions[0]

	// Generate saga ID if not provided
	sagaID := transaction.SagaID
	if sagaID == "" {
		sagaID = fmt.Sprintf("saga-%s-%d", name, ctx.Value("request_id"))
		if sagaID == fmt.Sprintf("saga-%s-<nil>", name) {
			sagaID = fmt.Sprintf("saga-%s-%d", name, time.Now().UnixNano())
		}
	}

	// Create saga executor
	sagaExecutor := schema.NewSagaExecutor(
		common.NewExecutorAdapter(le.verbRegistry, facts),
		le.sagaStore,
		le.capSystem,
		le.verbRegistry,
		fmt.Sprintf("list-%s", name),
	)

	// Convert program to effects for saga execution
	// This is a simplification - for full saga support we'd need to enhance
	// the saga executor to understand Programs directly
	effects := le.programToEffects(transaction.Program)

	// Execute with saga
	results, err := sagaExecutor.ExecuteWithSaga(ctx, sagaID, transaction.Name, effects)
	if err != nil {
		return nil, err
	}

	// Return the last result
	if len(results) > 0 {
		return results[len(results)-1], nil
	}
	return nil, nil
}

// programToEffects converts a program to a list of effects (simplified)
// This is a temporary bridge until we enhance saga executor to work with Programs directly
func (le *Executor) programToEffects(program *flow.Program) []eff.Effect {
	var effects []eff.Effect

	var extract func(*flow.Program)
	extract = func(p *flow.Program) {
		if p == nil {
			return
		}

		if p.Tag == flow.EffectProgramTag {
			effects = append(effects, p.Effect)
			// We can't statically follow the continuation, so we stop here
			// This is a limitation that could be addressed by enhancing the saga executor
		}

		if p.Tag == flow.TransactionProgramTag {
			extract(p.Transaction.Program)
		}
	}

	extract(program)
	return effects
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
