// flow/executor.go
package flow

import (
	"context"
	"fmt"
	"sync"
	"time"

	eff "github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/common"
	"github.com/effectus/effectus-go/schema"
	"github.com/effectus/effectus-go/schema/capability"
)

// ExecutorOption defines an option for configuring the flow executor
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

// Executor is the main executor for flow programs with saga and capability support
type Executor struct {
	verbRegistry common.VerbRegistry
	capSystem    *capability.CapabilitySystem
	sagaEnabled  bool
	sagaStore    schema.SagaStore
	mu           sync.Mutex
}

// NewExecutor creates a new executor for flow programs
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

// ExecuteProgram executes a flow program with saga and capability support
func (fe *Executor) ExecuteProgram(ctx context.Context, flowName string, program *Program, facts common.Facts) (interface{}, error) {
	// If saga is enabled and program is not already transactional, add compensation
	if fe.sagaEnabled && fe.sagaStore != nil && !program.IsTransactional() {
		// Add compensation information if available
		compensations := fe.extractCompensations(program)
		if len(compensations) > 0 {
			program = program.WithCompensation(func(verb string) string {
				return compensations[verb]
			})
		}

		// Wrap in atomic transaction for saga support
		program = program.ToAtomic(fmt.Sprintf("flow-%s", flowName))
	}

	// Create the appropriate executor based on configuration
	var executor eff.Executor

	if fe.sagaEnabled && fe.sagaStore != nil && fe.capSystem != nil {
		// Check if program is transactional
		if program.IsTransactional() {
			// Use saga-aware execution for transactional programs
			return fe.executeSagaProgram(ctx, flowName, program, facts)
		}

		// For non-transactional programs, use capability awareness
		executor = capability.NewCapabilityAwareExecutor(
			common.NewExecutorAdapter(fe.verbRegistry, facts),
			fe.capSystem,
			fmt.Sprintf("flow-%s", flowName),
		)
	} else if fe.capSystem != nil {
		// Use capability-aware executor
		executor = capability.NewCapabilityAwareExecutor(
			common.NewExecutorAdapter(fe.verbRegistry, facts),
			fe.capSystem,
			"flow-executor",
		)
	} else {
		// Use simple executor
		executor = common.NewExecutorAdapter(fe.verbRegistry, facts)
	}

	// Execute the program using the existing Run function
	return Run(program, executor)
}

// extractCompensations extracts compensation verbs from program effects
func (fe *Executor) extractCompensations(program *Program) map[string]string {
	compensations := make(map[string]string)

	var extract func(*Program)
	extract = func(p *Program) {
		if p == nil {
			return
		}

		if p.Tag == EffectProgramTag {
			if fe.verbRegistry != nil {
				if spec, exists := fe.verbRegistry.GetVerb(p.Effect.Verb); exists {
					if inverse := spec.Inverse; inverse != "" {
						compensations[p.Effect.Verb] = inverse
					}
				}
			}
			// We can't statically follow continuations, but we've captured this effect
		}

		if p.Tag == TransactionProgramTag {
			extract(p.Transaction.Program)
		}
	}

	extract(program)
	return compensations
}

// executeSagaProgram executes a transactional program with full saga support
func (fe *Executor) executeSagaProgram(ctx context.Context, flowName string, program *Program, facts common.Facts) (interface{}, error) {
	// Extract transaction information
	transactions := ExtractTransactions(program)

	if len(transactions) == 0 {
		// No transactions, fall back to regular execution
		return fe.ExecuteProgram(ctx, flowName, program, facts)
	}

	// For now, handle the first transaction
	// TODO: Support nested transactions
	transaction := transactions[0]

	// Generate saga ID if not provided
	sagaID := transaction.SagaID
	if sagaID == "" {
		sagaID = fmt.Sprintf("saga-flow-%s-%d", flowName, ctx.Value("request_id"))
		if sagaID == fmt.Sprintf("saga-flow-%s-<nil>", flowName) {
			sagaID = fmt.Sprintf("saga-flow-%s-%d", flowName, time.Now().UnixNano())
		}
	}

	// Create saga executor
	sagaExecutor := schema.NewSagaExecutor(
		common.NewExecutorAdapter(fe.verbRegistry, facts),
		fe.sagaStore,
		fe.capSystem,
		fe.verbRegistry,
		fmt.Sprintf("flow-%s", flowName),
	)

	// Convert program to effects for saga execution
	// This is a simplification - for full saga support we'd need to enhance
	// the saga executor to understand Programs directly
	effects := fe.programToEffects(transaction.Program)

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
func (fe *Executor) programToEffects(program *Program) []eff.Effect {
	var effects []eff.Effect

	var extract func(*Program)
	extract = func(p *Program) {
		if p == nil {
			return
		}

		if p.Tag == EffectProgramTag {
			effects = append(effects, p.Effect)
			// We can't statically follow the continuation, so we stop here
			// This is a limitation that could be addressed by enhancing the saga executor
		}

		if p.Tag == TransactionProgramTag {
			extract(p.Transaction.Program)
		}
	}

	extract(program)
	return effects
}
