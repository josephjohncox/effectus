// flow/executor.go
package flow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
	eff "github.com/josephjohncox/effectus"
	"github.com/josephjohncox/effectus/common"
	"github.com/josephjohncox/effectus/schema"
	"github.com/josephjohncox/effectus/schema/capability"
	"github.com/josephjohncox/effectus/schema/verb"
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
	if fe.sagaEnabled {
		if fe.sagaStore == nil {
			return nil, schema.ErrSagaStoreRequired
		}
		if !program.IsTransactional() {
			program = program.ToAtomic(fmt.Sprintf("flow-%s", flowName))
		}
		return fe.executeSagaProgram(ctx, flowName, program, facts)
	}

	var executor eff.Executor
	if fe.capSystem != nil {
		executor = capability.NewCapabilityAwareExecutor(
			common.NewExecutorAdapter(fe.verbRegistry, facts),
			fe.capSystem,
			"flow-executor",
		)
	} else {
		executor = common.NewExecutorAdapter(fe.verbRegistry, facts)
	}

	return RunContext(ctx, program, executor)
}

// executeSagaProgram executes a transactional program with full saga support
func (fe *Executor) executeSagaProgram(ctx context.Context, flowName string, program *Program, facts common.Facts) (interface{}, error) {
	// Extract transaction information
	transactions := ExtractTransactions(program)

	if len(transactions) == 0 {
		// No transactions, fall back to regular execution
		return fe.ExecuteProgram(ctx, flowName, program, facts)
	}

	if len(transactions) > 1 {
		return nil, fmt.Errorf("nested saga transactions are not supported")
	}
	transaction := transactions[0]

	// Generate saga ID if not provided.
	sagaID := transaction.SagaID
	if sagaID == "" {
		sagaID = newSagaID(ctx, "saga-flow-"+flowName)
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

	result, err := RunContext(ctx, transaction.Program, sagaExecutor)
	if err != nil {
		return nil, err
	}

	if err := fe.sagaStore.CompleteSaga(sagaID); err != nil {
		return nil, fmt.Errorf("completing saga: %w", err)
	}

	return result, nil
}

func newSagaID(ctx context.Context, prefix string) string {
	if ctx != nil {
		requestID := strings.TrimSpace(fmt.Sprint(ctx.Value("request_id")))
		if requestID != "" && requestID != "<nil>" {
			return prefix + "-" + requestID
		}
	}
	return prefix + "-" + uuid.NewString()
}

type sagaProgramExecutor struct {
	executor         eff.Executor
	sagaStore        schema.SagaStore
	capSystem        *capability.CapabilitySystem
	verbRegistry     common.VerbRegistry
	holderID         string
	sagaID           string
	ctx              context.Context
	executionMu      sync.Mutex
	nextSequence     int
	executed         []sagaExecutedEffect
	compensationOnce sync.Once
	compensationErr  error
}

type sagaExecutedEffect struct {
	id     string
	effect eff.Effect
}

func (se *sagaProgramExecutor) Do(effect eff.Effect) (interface{}, error) {
	return se.DoContext(se.ctx, effect)
}

func (se *sagaProgramExecutor) DoContext(ctx context.Context, effect eff.Effect) (interface{}, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, errors.Join(ctx.Err(), se.compensate())
		default:
		}
	}

	var args map[string]interface{}
	switch payload := effect.Payload.(type) {
	case nil:
		args = make(map[string]interface{})
	case map[string]interface{}:
		args = payload
	default:
		return nil, fmt.Errorf("recording effect %s: saga payload must be an argument map", effect.Verb)
	}
	se.executionMu.Lock()
	se.nextSequence++
	sequence := se.nextSequence
	se.executionMu.Unlock()
	effectID := schema.SagaEffectID(sequence)
	if err := se.sagaStore.RecordEffect(se.sagaID, effectID, sequence, effect.Verb, args); err != nil {
		return nil, errors.Join(
			fmt.Errorf("recording effect in saga: %w", err),
			se.compensate(),
		)
	}
	recorded, err := schema.GetSagaEffect(se.sagaStore, se.sagaID, effectID)
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("loading recorded effect %s: %w", effectID, err),
			se.compensate(),
		)
	}
	switch recorded.Status {
	case schema.SagaEffectSuccess:
		se.executionMu.Lock()
		se.executed = append(se.executed, sagaExecutedEffect{id: effectID, effect: effect})
		se.executionMu.Unlock()
		return recorded.Result, nil
	case schema.SagaEffectFailed:
		return nil, errors.Join(
			fmt.Errorf("effect %s previously failed: %s", effectID, recorded.Error),
			se.compensate(),
		)
	case schema.SagaEffectCompensated:
		return nil, errors.Join(
			fmt.Errorf("effect %s was already compensated", effectID),
			se.compensate(),
		)
	case schema.SagaEffectPending:
		// Pending effects are retried. The external-effect/MarkSuccess crash
		// window therefore remains at-least-once unless the verb is idempotent.
	default:
		return nil, errors.Join(
			fmt.Errorf("effect %s has unknown saga status %q", effectID, recorded.Status),
			se.compensate(),
		)
	}

	locks, err := se.acquireLocks(effect)
	if err != nil {
		markErr := se.sagaStore.MarkFailed(se.sagaID, effectID, err)
		return nil, errors.Join(
			fmt.Errorf("acquiring locks for %s: %w", effect.Verb, err),
			wrapSagaStoreError("marking effect failed", markErr),
			se.compensate(),
		)
	}
	for _, lock := range locks {
		defer lock.Unlock()
	}

	result, err := eff.Invoke(ctx, se.executor, effect)
	if err != nil {
		markErr := se.sagaStore.MarkFailed(se.sagaID, effectID, err)
		return nil, errors.Join(
			fmt.Errorf("executing effect %s: %w", effect.Verb, err),
			wrapSagaStoreError("marking effect failed", markErr),
			se.compensate(),
		)
	}

	se.executionMu.Lock()
	se.executed = append(se.executed, sagaExecutedEffect{id: effectID, effect: effect})
	se.executionMu.Unlock()

	if err := se.sagaStore.MarkSuccess(se.sagaID, effectID, result); err != nil {
		return nil, errors.Join(
			fmt.Errorf("marking effect success in saga: %w", err),
			se.compensate(),
		)
	}

	return result, nil
}

func wrapSagaStoreError(action string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", action, err)
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
			resource.Cap.RuntimeCapability(),
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

func (se *sagaProgramExecutor) compensate() error {
	se.compensationOnce.Do(func() {
		se.compensationErr = se.runCompensation()
	})
	return se.compensationErr
}

func (se *sagaProgramExecutor) runCompensation() error {
	se.executionMu.Lock()
	executed := append([]sagaExecutedEffect(nil), se.executed...)
	se.executionMu.Unlock()

	var failures []error
	for i := len(executed) - 1; i >= 0; i-- {
		executedEffect := executed[i]
		effect := executedEffect.effect
		spec, exists := se.verbRegistry.GetVerb(effect.Verb)
		if !exists {
			failures = append(failures, fmt.Errorf("compensating %s: verb is not registered", effect.Verb))
			continue
		}
		inverseVerb := spec.Inverse
		if inverseVerb == "" {
			failures = append(failures, fmt.Errorf("compensating %s: inverse verb is not configured", effect.Verb))
			continue
		}

		inverseSpec, exists := se.verbRegistry.GetVerb(inverseVerb)
		if !exists {
			failures = append(failures, fmt.Errorf("compensating %s: inverse verb %s is not registered", effect.Verb, inverseVerb))
			continue
		}

		locks, err := se.acquireCompensationLocks(inverseSpec)
		if err != nil {
			failures = append(failures, fmt.Errorf("compensating %s: %w", effect.Verb, err))
			continue
		}

		_, executeErr := se.executor.Do(eff.Effect{
			Verb:    inverseVerb,
			Payload: effect.Payload,
		})
		for _, lock := range locks {
			lock.Unlock()
		}
		if executeErr != nil {
			failures = append(failures, fmt.Errorf("compensating %s with %s: %w", effect.Verb, inverseVerb, executeErr))
			continue
		}
		if err := se.sagaStore.MarkCompensated(se.sagaID, executedEffect.id); err != nil {
			failures = append(failures, fmt.Errorf("marking %s compensated: %w", effect.Verb, err))
		}
	}
	return errors.Join(failures...)
}

func (se *sagaProgramExecutor) acquireCompensationLocks(spec *verb.Spec) ([]*capability.LockResult, error) {
	if se.capSystem == nil {
		return nil, nil
	}

	var locks []*capability.LockResult
	for _, resource := range spec.Resources {
		lock, err := se.capSystem.AcquireLock(
			resource.Cap.RuntimeCapability(),
			resource.Resource,
			se.holderID,
		)
		if err != nil {
			for _, held := range locks {
				held.Unlock()
			}
			return nil, fmt.Errorf("acquiring lock for %s:%s: %w", resource.Resource, resource.Cap.String(), err)
		}
		locks = append(locks, lock)
	}
	return locks, nil
}
