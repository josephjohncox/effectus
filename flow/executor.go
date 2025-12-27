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
	"github.com/effectus/effectus-go/schema/types"
	"github.com/effectus/effectus-go/schema/verb"
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

	executor := common.NewExecutorAdapter(fe.verbRegistry, facts)
	holderID := fmt.Sprintf("flow-%s", flowName)

	if err := fe.sagaStore.StartTransaction(sagaID, transaction.Name); err != nil {
		return nil, fmt.Errorf("starting saga transaction: %w", err)
	}

	sagaExecutor := &sagaProgramExecutor{
		executor:     executor,
		sagaStore:    fe.sagaStore,
		capSystem:    fe.capSystem,
		verbRegistry: fe.verbRegistry,
		holderID:     holderID,
		sagaID:       sagaID,
		ctx:          ctx,
	}

	result, err := Run(transaction.Program, sagaExecutor)
	if err != nil {
		return nil, err
	}

	if err := fe.sagaStore.CompleteSaga(sagaID); err != nil {
		return nil, fmt.Errorf("completing saga: %w", err)
	}

	return result, nil
}

type sagaProgramExecutor struct {
	executor     eff.Executor
	sagaStore    schema.SagaStore
	capSystem    *capability.CapabilitySystem
	verbRegistry common.VerbRegistry
	holderID     string
	sagaID       string
	ctx          context.Context
	mu           sync.Mutex
	compensated  bool
}

func (se *sagaProgramExecutor) Do(effect eff.Effect) (interface{}, error) {
	if se.ctx != nil {
		select {
		case <-se.ctx.Done():
			se.compensate()
			return nil, se.ctx.Err()
		default:
		}
	}

	if args, ok := effect.Payload.(map[string]interface{}); ok {
		if err := se.sagaStore.RecordEffect(se.sagaID, effect.Verb, args); err != nil {
			return nil, fmt.Errorf("recording effect in saga: %w", err)
		}
	}

	locks, err := se.acquireLocks(effect)
	if err != nil {
		se.compensate()
		return nil, fmt.Errorf("acquiring locks for %s: %w", effect.Verb, err)
	}
	for _, lock := range locks {
		defer lock.Unlock()
	}

	result, err := se.executor.Do(effect)
	if err != nil {
		se.sagaStore.MarkFailed(se.sagaID, effect.Verb, err)
		se.compensate()
		return nil, fmt.Errorf("executing effect %s: %w", effect.Verb, err)
	}

	if err := se.sagaStore.MarkSuccess(se.sagaID, effect.Verb); err != nil {
		se.compensate()
		return nil, fmt.Errorf("marking effect success in saga: %w", err)
	}

	return result, nil
}

func (se *sagaProgramExecutor) acquireLocks(effect eff.Effect) ([]*capability.LockResult, error) {
	if se.capSystem == nil || se.verbRegistry == nil {
		return nil, nil
	}

	spec, exists := se.verbRegistry.GetVerb(effect.Verb)
	if !exists {
		return nil, nil
	}

	var locks []*capability.LockResult
	for _, resource := range spec.Resources {
		lock, err := se.capSystem.AcquireLock(
			convertVerbCapabilityToTypes(resource.Cap),
			resource.Resource,
			se.holderID,
		)
		if err != nil {
			for _, held := range locks {
				held.Unlock()
			}
			return nil, err
		}
		locks = append(locks, lock)
	}

	return locks, nil
}

func (se *sagaProgramExecutor) compensate() {
	se.mu.Lock()
	if se.compensated {
		se.mu.Unlock()
		return
	}
	se.compensated = true
	se.mu.Unlock()

	sagaEffects, err := se.sagaStore.GetTransactionEffects(se.sagaID)
	if err != nil {
		return
	}

	for i := len(sagaEffects) - 1; i >= 0; i-- {
		sagaEffect := sagaEffects[i]
		if sagaEffect.Status != "success" {
			continue
		}

		spec, exists := se.verbRegistry.GetVerb(sagaEffect.Verb)
		if !exists {
			continue
		}
		inverseVerb := spec.Inverse
		if inverseVerb == "" {
			continue
		}

		inverseSpec, exists := se.verbRegistry.GetVerb(inverseVerb)
		if !exists {
			continue
		}

		var locks []*capability.LockResult
		if se.capSystem != nil {
			for _, resource := range inverseSpec.Resources {
				lock, err := se.capSystem.AcquireLock(
					convertVerbCapabilityToTypes(resource.Cap),
					resource.Resource,
					se.holderID,
				)
				if err != nil {
					fmt.Printf("Warning: failed to acquire lock for compensation %s:%s: %v\n",
						resource.Resource, resource.Cap.String(), err)
					continue
				}
				locks = append(locks, lock)
			}
		}

		_, err = se.executor.Do(eff.Effect{
			Verb:    inverseVerb,
			Payload: sagaEffect.Args,
		})

		for _, lock := range locks {
			lock.Unlock()
		}

		if err != nil {
			fmt.Printf("Warning: compensation failed for %s: %v\n", sagaEffect.Verb, err)
		} else {
			se.sagaStore.MarkCompensated(se.sagaID, sagaEffect.Verb)
		}
	}
}

func convertVerbCapabilityToTypes(verbCap verb.Capability) types.Capability {
	switch {
	case verbCap&verb.CapRead != 0:
		return types.CapabilityRead
	case verbCap&verb.CapCreate != 0:
		return types.CapabilityCreate
	case verbCap&verb.CapDelete != 0:
		return types.CapabilityDelete
	case verbCap&verb.CapWrite != 0:
		return types.CapabilityModify
	default:
		return types.CapabilityModify
	}
}
