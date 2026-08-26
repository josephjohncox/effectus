// Package schema provides saga transaction management with capability-based locking
package schema

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/schema/capability"
	"github.com/effectus/effectus-go/schema/verb"
)

// ErrSagaStoreRequired reports an invalid saga execution configuration.
var ErrSagaStoreRequired = errors.New("saga execution requires a saga store")

// SagaStore defines the interface for persisting saga transactions
type SagaStore interface {
	StartTransaction(sagaID, ruleName string) error
	RecordEffect(sagaID, effectID string, sequence int, verb string, args map[string]interface{}) error
	MarkSuccess(sagaID, effectID string, result interface{}) error
	MarkFailed(sagaID, effectID string, reason error) error
	MarkCompensated(sagaID, effectID string) error
	GetTransactionEffects(sagaID string) ([]*SagaEffect, error)
	GetActiveSagas() ([]string, error)
	CompleteSaga(sagaID string) error
}

const (
	SagaEffectPending     = "pending"
	SagaEffectSuccess     = "success"
	SagaEffectFailed      = "failed"
	SagaEffectCompensated = "compensated"
)

// SagaEffectID returns the stable identity for an effect's source-order position.
func SagaEffectID(sequence int) string {
	return fmt.Sprintf("step-%06d", sequence)
}

// GetSagaEffect returns one effect occurrence by its stable identity.
func GetSagaEffect(store SagaStore, sagaID, effectID string) (*SagaEffect, error) {
	effects, err := store.GetTransactionEffects(sagaID)
	if err != nil {
		return nil, err
	}
	for _, effect := range effects {
		if effect.ID == effectID {
			return effect, nil
		}
	}
	return nil, fmt.Errorf("effect not found for saga %s: %s", sagaID, effectID)
}

// SagaEffect represents an effect recorded in a saga transaction.
type SagaEffect struct {
	ID        string                 `json:"id"`
	Sequence  int                    `json:"sequence"`
	Verb      string                 `json:"verb"`
	Args      map[string]interface{} `json:"args"`
	Result    interface{}            `json:"result,omitempty"`
	Status    string                 `json:"status"`
	Timestamp time.Time              `json:"timestamp"`
	Error     string                 `json:"error,omitempty"`
}

type executedSagaEffect struct {
	ID     string
	Effect effectus.Effect
}

// SagaExecutor wraps an executor with saga and capability management
type SagaExecutor struct {
	executor     effectus.Executor
	sagaStore    SagaStore
	capSystem    *capability.CapabilitySystem
	verbRegistry SagaVerbRegistry
	holderID     string
}

// SagaVerbRegistry defines the interface for accessing verb specifications in sagas.
type SagaVerbRegistry interface {
	GetVerb(name string) (*verb.Spec, bool)
}

// NewSagaExecutor creates a new saga-aware executor
func NewSagaExecutor(executor effectus.Executor, sagaStore SagaStore, capSystem *capability.CapabilitySystem, verbRegistry SagaVerbRegistry, holderID string) *SagaExecutor {
	return &SagaExecutor{
		executor:     executor,
		sagaStore:    sagaStore,
		capSystem:    capSystem,
		verbRegistry: verbRegistry,
		holderID:     holderID,
	}
}

// ExecuteWithSaga executes a series of effects within a saga transaction
func (se *SagaExecutor) ExecuteWithSaga(ctx context.Context, sagaID string, ruleName string, effects []effectus.Effect) ([]interface{}, error) {
	// Start saga transaction
	if err := se.sagaStore.StartTransaction(sagaID, ruleName); err != nil {
		return nil, fmt.Errorf("starting saga transaction: %w", err)
	}

	var results []interface{}
	var executedEffects []executedSagaEffect

	// Plan the execution with capability analysis
	plan, err := se.createExecutionPlan(effects)
	if err != nil {
		return nil, fmt.Errorf("creating execution plan: %w", err)
	}

	// Execute effects according to the plan
	for _, step := range plan.Steps {
		select {
		case <-ctx.Done():
			return nil, errors.Join(ctx.Err(), se.compensate(sagaID, executedEffects))
		default:
		}

		// Acquire capability-based locks for this step
		locks, err := se.acquireLocksForStep(step)
		if err != nil {
			return nil, errors.Join(
				fmt.Errorf("acquiring locks for step %d: %w", len(results), err),
				se.compensate(sagaID, executedEffects),
			)
		}

		// Execute the effects in this step
		stepResults, stepEffects, err := se.executeStep(ctx, sagaID, step)

		// Release locks
		for _, lock := range locks {
			lock.Unlock()
		}

		if err != nil {
			executedBeforeFailure := append(executedEffects, stepEffects...)
			return nil, errors.Join(
				fmt.Errorf("executing step %d: %w", len(results), err),
				se.compensate(sagaID, executedBeforeFailure),
			)
		}

		results = append(results, stepResults...)
		executedEffects = append(executedEffects, stepEffects...)
	}

	// Mark saga as completed
	if err := se.sagaStore.CompleteSaga(sagaID); err != nil {
		return nil, fmt.Errorf("completing saga: %w", err)
	}

	return results, nil
}

// ExecutionPlan represents a plan for executing effects with proper ordering
type ExecutionPlan struct {
	Steps []*ExecutionStep
}

// ExecutionStep represents a step in the execution plan
type ExecutionStep struct {
	Sequence           int
	Effects            []effectus.Effect
	CanRunConcurrently bool
}

// createExecutionPlan preserves source order. Parallel execution must be
// represented explicitly; capability non-conflict alone does not prove that
// two observable effects commute.
func (se *SagaExecutor) createExecutionPlan(effects []effectus.Effect) (*ExecutionPlan, error) {
	plan := &ExecutionPlan{Steps: make([]*ExecutionStep, 0, len(effects))}
	for index, effect := range effects {
		plan.Steps = append(plan.Steps, &ExecutionStep{
			Sequence:           index + 1,
			Effects:            []effectus.Effect{effect},
			CanRunConcurrently: false,
		})
	}
	return plan, nil
}

// acquireLocksForStep acquires all necessary capability-based locks for a step
func (se *SagaExecutor) acquireLocksForStep(step *ExecutionStep) ([]*capability.LockResult, error) {
	if se.capSystem == nil {
		return nil, nil
	}
	var locks []*capability.LockResult

	for _, effect := range step.Effects {
		// Get verb specification
		spec, exists := se.verbRegistry.GetVerb(effect.Verb)
		if !exists {
			// Release any locks we've already acquired
			for _, lock := range locks {
				lock.Unlock()
			}
			return nil, fmt.Errorf("unknown verb: %s", effect.Verb)
		}

		// Acquire locks for each resource the verb affects
		resources := spec.Resources
		for _, resource := range resources {
			// Convert verb capability to types capability
			typesCapability := resource.Cap.RuntimeCapability()

			lock, err := se.capSystem.AcquireLock(typesCapability, resource.Resource, se.holderID)
			if err != nil {
				// Release any locks we've already acquired
				for _, prevLock := range locks {
					prevLock.Unlock()
				}
				return nil, fmt.Errorf("acquiring lock for %s:%s: %w", resource.Resource, resource.Cap.String(), err)
			}
			locks = append(locks, lock)
		}
	}

	return locks, nil
}

// executeStep executes all effects in a step
func (se *SagaExecutor) executeStep(ctx context.Context, sagaID string, step *ExecutionStep) ([]interface{}, []executedSagaEffect, error) {
	var results []interface{}
	var executedEffects []executedSagaEffect

	for offset, effect := range step.Effects {
		sequence := step.Sequence + offset
		effectID := SagaEffectID(sequence)
		var args map[string]interface{}
		switch payload := effect.Payload.(type) {
		case nil:
			args = make(map[string]interface{})
		case map[string]interface{}:
			args = payload
		default:
			return nil, executedEffects, fmt.Errorf("recording effect %s: saga payload must be an argument map", effect.Verb)
		}
		if err := se.sagaStore.RecordEffect(sagaID, effectID, sequence, effect.Verb, args); err != nil {
			return nil, executedEffects, fmt.Errorf("recording effect in saga: %w", err)
		}
		recorded, err := GetSagaEffect(se.sagaStore, sagaID, effectID)
		if err != nil {
			return nil, executedEffects, fmt.Errorf("loading recorded effect %s: %w", effectID, err)
		}
		switch recorded.Status {
		case SagaEffectSuccess:
			results = append(results, recorded.Result)
			executedEffects = append(executedEffects, executedSagaEffect{ID: effectID, Effect: effect})
			continue
		case SagaEffectFailed:
			return nil, executedEffects, fmt.Errorf("effect %s previously failed: %s", effectID, recorded.Error)
		case SagaEffectCompensated:
			return nil, executedEffects, fmt.Errorf("effect %s was already compensated", effectID)
		case SagaEffectPending:
			// A pending effect is executed. Recovery therefore remains at-least-once
			// across the crash window between the external effect and MarkSuccess.
		default:
			return nil, executedEffects, fmt.Errorf("effect %s has unknown saga status %q", effectID, recorded.Status)
		}

		result, err := effectus.Invoke(ctx, se.executor, effect)
		if err != nil {
			markErr := se.sagaStore.MarkFailed(sagaID, effectID, err)
			return nil, executedEffects, errors.Join(
				fmt.Errorf("executing effect %s: %w", effect.Verb, err),
				wrapSagaStoreError("marking effect failed", markErr),
			)
		}

		results = append(results, result)
		executedEffects = append(executedEffects, executedSagaEffect{ID: effectID, Effect: effect})
		if err := se.sagaStore.MarkSuccess(sagaID, effectID, result); err != nil {
			return results, executedEffects, fmt.Errorf("marking effect success in saga: %w", err)
		}
	}

	return results, executedEffects, nil
}

func wrapSagaStoreError(action string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", action, err)
}

// compensate reverses exactly the effect occurrences completed in this run.
func (se *SagaExecutor) compensate(sagaID string, executedEffects []executedSagaEffect) error {
	var failures []error
	for i := len(executedEffects) - 1; i >= 0; i-- {
		executed := executedEffects[i]
		effect := executed.Effect
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

		var locks []*capability.LockResult
		if se.capSystem != nil {
			lockFailed := false
			for _, resource := range inverseSpec.Resources {
				lock, err := se.capSystem.AcquireLock(
					resource.Cap.RuntimeCapability(),
					resource.Resource,
					se.holderID,
				)
				if err != nil {
					failures = append(failures, fmt.Errorf(
						"compensating %s: acquiring lock for %s:%s: %w",
						effect.Verb,
						resource.Resource,
						resource.Cap.String(),
						err,
					))
					lockFailed = true
					break
				}
				locks = append(locks, lock)
			}
			if lockFailed {
				for _, lock := range locks {
					lock.Unlock()
				}
				continue
			}
		}

		_, executeErr := se.executor.Do(effectus.Effect{Verb: inverseVerb, Payload: effect.Payload})
		for _, lock := range locks {
			lock.Unlock()
		}
		if executeErr != nil {
			failures = append(failures, fmt.Errorf("compensating %s with %s: %w", effect.Verb, inverseVerb, executeErr))
			continue
		}
		if err := se.sagaStore.MarkCompensated(sagaID, executed.ID); err != nil {
			failures = append(failures, fmt.Errorf("marking %s compensated: %w", effect.Verb, err))
		}
	}
	return errors.Join(failures...)
}

// InMemorySagaStore provides an in-memory implementation of SagaStore for testing.
type InMemorySagaStore struct {
	transactions map[string][]*SagaEffect
	completed    map[string]bool
	mu           sync.RWMutex
}

// NewInMemorySagaStore creates a new in-memory saga store.
func NewInMemorySagaStore() *InMemorySagaStore {
	return &InMemorySagaStore{
		transactions: make(map[string][]*SagaEffect),
		completed:    make(map[string]bool),
	}
}

// StartTransaction implements SagaStore. Reopening a saga preserves its effect log.
func (ims *InMemorySagaStore) StartTransaction(sagaID, ruleName string) error {
	ims.mu.Lock()
	defer ims.mu.Unlock()
	if _, exists := ims.transactions[sagaID]; !exists {
		ims.transactions[sagaID] = make([]*SagaEffect, 0)
	}
	ims.completed[sagaID] = false
	return nil
}

// RecordEffect implements SagaStore.
func (ims *InMemorySagaStore) RecordEffect(sagaID, effectID string, sequence int, verb string, args map[string]interface{}) error {
	ims.mu.Lock()
	defer ims.mu.Unlock()
	effects, exists := ims.transactions[sagaID]
	if !exists {
		return fmt.Errorf("saga transaction not found: %s", sagaID)
	}
	for _, effect := range effects {
		if effect.ID != effectID {
			continue
		}
		if effect.Sequence == sequence && effect.Verb == verb && reflect.DeepEqual(effect.Args, args) {
			return nil
		}
		return fmt.Errorf("effect identity conflict for saga %s effect %s", sagaID, effectID)
	}
	ims.transactions[sagaID] = append(effects, &SagaEffect{
		ID:        effectID,
		Sequence:  sequence,
		Verb:      verb,
		Args:      args,
		Status:    SagaEffectPending,
		Timestamp: time.Now().UTC(),
	})
	return nil
}

// MarkSuccess implements SagaStore.
func (ims *InMemorySagaStore) MarkSuccess(sagaID, effectID string, result interface{}) error {
	ims.mu.Lock()
	defer ims.mu.Unlock()
	effect, err := ims.effectLocked(sagaID, effectID)
	if err != nil {
		return err
	}
	if effect.Status != SagaEffectPending && effect.Status != SagaEffectSuccess {
		return fmt.Errorf("cannot mark saga %s effect %s successful from status %s", sagaID, effectID, effect.Status)
	}
	effect.Status = SagaEffectSuccess
	effect.Result = result
	effect.Error = ""
	return nil
}

// MarkFailed implements SagaStore.
func (ims *InMemorySagaStore) MarkFailed(sagaID, effectID string, reason error) error {
	ims.mu.Lock()
	defer ims.mu.Unlock()
	effect, err := ims.effectLocked(sagaID, effectID)
	if err != nil {
		return err
	}
	if effect.Status != SagaEffectPending && effect.Status != SagaEffectFailed {
		return fmt.Errorf("cannot mark saga %s effect %s failed from status %s", sagaID, effectID, effect.Status)
	}
	effect.Status = SagaEffectFailed
	if reason != nil {
		effect.Error = reason.Error()
	}
	return nil
}

// MarkCompensated implements SagaStore.
func (ims *InMemorySagaStore) MarkCompensated(sagaID, effectID string) error {
	ims.mu.Lock()
	defer ims.mu.Unlock()
	effect, err := ims.effectLocked(sagaID, effectID)
	if err != nil {
		return err
	}
	if effect.Status != SagaEffectSuccess && effect.Status != SagaEffectCompensated {
		return fmt.Errorf("cannot compensate saga %s effect %s from status %s", sagaID, effectID, effect.Status)
	}
	effect.Status = SagaEffectCompensated
	return nil
}

func (ims *InMemorySagaStore) effectLocked(sagaID, effectID string) (*SagaEffect, error) {
	effects, exists := ims.transactions[sagaID]
	if !exists {
		return nil, fmt.Errorf("saga transaction not found: %s", sagaID)
	}
	for _, effect := range effects {
		if effect.ID == effectID {
			return effect, nil
		}
	}
	return nil, fmt.Errorf("effect not found for saga %s: %s", sagaID, effectID)
}

// GetTransactionEffects implements SagaStore.
func (ims *InMemorySagaStore) GetTransactionEffects(sagaID string) ([]*SagaEffect, error) {
	ims.mu.RLock()
	defer ims.mu.RUnlock()
	effects, exists := ims.transactions[sagaID]
	if !exists {
		return nil, fmt.Errorf("saga transaction not found: %s", sagaID)
	}
	result := make([]*SagaEffect, len(effects))
	for i, effect := range effects {
		copyEffect := *effect
		result[i] = &copyEffect
	}
	return result, nil
}

// GetActiveSagas implements SagaStore.
func (ims *InMemorySagaStore) GetActiveSagas() ([]string, error) {
	ims.mu.RLock()
	defer ims.mu.RUnlock()
	sagas := make([]string, 0, len(ims.transactions))
	for sagaID := range ims.transactions {
		if !ims.completed[sagaID] {
			sagas = append(sagas, sagaID)
		}
	}
	return sagas, nil
}

// CompleteSaga implements SagaStore.
func (ims *InMemorySagaStore) CompleteSaga(sagaID string) error {
	ims.mu.Lock()
	defer ims.mu.Unlock()
	if _, exists := ims.transactions[sagaID]; !exists {
		return fmt.Errorf("saga transaction not found: %s", sagaID)
	}
	ims.completed[sagaID] = true
	return nil
}
