// Package schema provides saga transaction management with capability-based locking
package schema

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/schema/capability"
	"github.com/effectus/effectus-go/schema/types"
	"github.com/effectus/effectus-go/schema/verb"
)

// SagaStore defines the interface for persisting saga transactions
type SagaStore interface {
	StartTransaction(sagaID, ruleName string) error
	RecordEffect(sagaID, verb string, args map[string]interface{}) error
	MarkSuccess(sagaID, verb string) error
	MarkFailed(sagaID, verb string, reason error) error
	MarkCompensated(sagaID, verb string) error
	GetTransactionEffects(sagaID string) ([]*SagaEffect, error)
	GetActiveSagas() ([]string, error)
	CompleteSaga(sagaID string) error
}

// SagaEffect represents an effect recorded in a saga transaction
type SagaEffect struct {
	Verb      string                 `json:"verb"`
	Args      map[string]interface{} `json:"args"`
	Status    string                 `json:"status"` // "pending", "success", "failed", "compensated"
	Timestamp time.Time              `json:"timestamp"`
	Error     string                 `json:"error,omitempty"`
}

// SagaExecutor wraps an executor with saga and capability management
type SagaExecutor struct {
	executor     effectus.Executor
	sagaStore    SagaStore
	capSystem    *capability.CapabilitySystem
	verbRegistry SagaVerbRegistry
	holderID     string
}

// SagaVerbRegistry defines the interface for accessing verb specifications in sagas
type SagaVerbRegistry interface {
	GetVerb(name string) (SagaVerbSpec, bool)
}

// SagaVerbSpec defines the interface for verb specifications with saga support
type SagaVerbSpec interface {
	GetCapability() verb.Capability
	GetResourceSet() verb.ResourceSet
	GetInverse() string // For compensation
	IsIdempotent() bool
	IsCommutative() bool
	IsExclusive() bool
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
	var executedEffects []effectus.Effect

	// Plan the execution with capability analysis
	plan, err := se.createExecutionPlan(effects)
	if err != nil {
		return nil, fmt.Errorf("creating execution plan: %w", err)
	}

	// Execute effects according to the plan
	for _, step := range plan.Steps {
		select {
		case <-ctx.Done():
			// Context cancelled, compensate
			se.compensate(ctx, sagaID, executedEffects)
			return nil, ctx.Err()
		default:
		}

		// Acquire capability-based locks for this step
		locks, err := se.acquireLocksForStep(step)
		if err != nil {
			// Failed to acquire locks, compensate
			se.compensate(ctx, sagaID, executedEffects)
			return nil, fmt.Errorf("acquiring locks for step %d: %w", len(results), err)
		}

		// Execute the effects in this step
		stepResults, stepEffects, err := se.executeStep(ctx, sagaID, step, locks)

		// Release locks
		for _, lock := range locks {
			lock.Unlock()
		}

		if err != nil {
			// Execution failed, compensate
			se.compensate(ctx, sagaID, executedEffects)
			return nil, fmt.Errorf("executing step %d: %w", len(results), err)
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
	Effects            []effectus.Effect
	CanRunConcurrently bool
}

// createExecutionPlan analyzes effects and creates an execution plan that respects capabilities
func (se *SagaExecutor) createExecutionPlan(effects []effectus.Effect) (*ExecutionPlan, error) {
	plan := &ExecutionPlan{
		Steps: make([]*ExecutionStep, 0),
	}

	remainingEffects := make([]effectus.Effect, len(effects))
	copy(remainingEffects, effects)

	// Group effects into steps based on capability conflicts
	for len(remainingEffects) > 0 {
		step := &ExecutionStep{
			Effects:            make([]effectus.Effect, 0),
			CanRunConcurrently: true,
		}

		var nextRemaining []effectus.Effect

		for _, effect := range remainingEffects {
			canAddToStep := true

			// Check if this effect conflicts with any effect already in the step
			for _, stepEffect := range step.Effects {
				if se.effectsConflict(effect, stepEffect) {
					canAddToStep = false
					break
				}
			}

			if canAddToStep {
				step.Effects = append(step.Effects, effect)
			} else {
				nextRemaining = append(nextRemaining, effect)
			}
		}

		if len(step.Effects) == 0 {
			// No progress made, there might be a deadlock situation
			return nil, fmt.Errorf("unable to create execution plan: possible capability deadlock")
		}

		plan.Steps = append(plan.Steps, step)
		remainingEffects = nextRemaining
	}

	return plan, nil
}

// effectsConflict checks if two effects would conflict based on their capabilities and resources
func (se *SagaExecutor) effectsConflict(effect1, effect2 effectus.Effect) bool {
	// Get verb specifications
	spec1, exists := se.verbRegistry.GetVerb(effect1.Verb)
	if !exists {
		return true // Conservative: assume conflict if verb not found
	}

	spec2, exists := se.verbRegistry.GetVerb(effect2.Verb)
	if !exists {
		return true // Conservative: assume conflict if verb not found
	}

	// Check resource conflicts
	resources1 := spec1.GetResourceSet()
	resources2 := spec2.GetResourceSet()

	return resources1.ConflictsWith(resources2)
}

// acquireLocksForStep acquires all necessary capability-based locks for a step
func (se *SagaExecutor) acquireLocksForStep(step *ExecutionStep) ([]*capability.LockResult, error) {
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
		resources := spec.GetResourceSet()
		for _, resource := range resources {
			// Convert verb capability to types capability
			typesCapability := convertVerbCapabilityToTypes(resource.Cap)

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
func (se *SagaExecutor) executeStep(ctx context.Context, sagaID string, step *ExecutionStep, locks []*capability.LockResult) ([]interface{}, []effectus.Effect, error) {
	var results []interface{}
	var executedEffects []effectus.Effect

	for _, effect := range step.Effects {
		// Record effect in saga
		if args, ok := effect.Payload.(map[string]interface{}); ok {
			if err := se.sagaStore.RecordEffect(sagaID, effect.Verb, args); err != nil {
				return nil, executedEffects, fmt.Errorf("recording effect in saga: %w", err)
			}
		}

		// Execute the effect
		result, err := se.executor.Do(effect)
		if err != nil {
			// Mark as failed in saga
			se.sagaStore.MarkFailed(sagaID, effect.Verb, err)
			return nil, executedEffects, fmt.Errorf("executing effect %s: %w", effect.Verb, err)
		}

		// Mark as successful in saga
		if err := se.sagaStore.MarkSuccess(sagaID, effect.Verb); err != nil {
			return nil, executedEffects, fmt.Errorf("marking effect success in saga: %w", err)
		}

		results = append(results, result)
		executedEffects = append(executedEffects, effect)
	}

	return results, executedEffects, nil
}

// compensate performs compensation for failed effects
func (se *SagaExecutor) compensate(ctx context.Context, sagaID string, executedEffects []effectus.Effect) error {
	// Get saga effects from store
	sagaEffects, err := se.sagaStore.GetTransactionEffects(sagaID)
	if err != nil {
		return fmt.Errorf("getting saga effects for compensation: %w", err)
	}

	// Execute compensation in reverse order
	for i := len(sagaEffects) - 1; i >= 0; i-- {
		sagaEffect := sagaEffects[i]

		// Skip effects that weren't successful
		if sagaEffect.Status != "success" {
			continue
		}

		// Get verb specification
		spec, exists := se.verbRegistry.GetVerb(sagaEffect.Verb)
		if !exists {
			continue // Skip if verb not found
		}

		// Check if verb has an inverse
		inverseVerb := spec.GetInverse()
		if inverseVerb == "" {
			continue // Skip if no inverse defined
		}

		// Get inverse verb specification
		inverseSpec, exists := se.verbRegistry.GetVerb(inverseVerb)
		if !exists {
			continue // Skip if inverse verb not found
		}

		// Acquire locks for compensation
		inverseResources := inverseSpec.GetResourceSet()
		var locks []*capability.LockResult

		for _, resource := range inverseResources {
			typesCapability := convertVerbCapabilityToTypes(resource.Cap)
			lock, err := se.capSystem.AcquireLock(typesCapability, resource.Resource, se.holderID)
			if err != nil {
				// Log error but continue with compensation
				fmt.Printf("Warning: failed to acquire lock for compensation %s:%s: %v\n",
					resource.Resource, resource.Cap.String(), err)
				continue
			}
			locks = append(locks, lock)
		}

		// Execute compensation
		compensationEffect := effectus.Effect{
			Verb:    inverseVerb,
			Payload: sagaEffect.Args,
		}

		_, err = se.executor.Do(compensationEffect)

		// Release locks
		for _, lock := range locks {
			lock.Unlock()
		}

		if err != nil {
			fmt.Printf("Warning: compensation failed for %s: %v\n", sagaEffect.Verb, err)
		} else {
			// Mark as compensated
			se.sagaStore.MarkCompensated(sagaID, sagaEffect.Verb)
		}
	}

	return nil
}

// convertVerbCapabilityToTypes converts verb.Capability to types.Capability
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

// InMemorySagaStore provides an in-memory implementation of SagaStore for testing
type InMemorySagaStore struct {
	transactions map[string][]*SagaEffect
	mu           sync.RWMutex
}

// NewInMemorySagaStore creates a new in-memory saga store
func NewInMemorySagaStore() *InMemorySagaStore {
	return &InMemorySagaStore{
		transactions: make(map[string][]*SagaEffect),
	}
}

// StartTransaction implements SagaStore
func (ims *InMemorySagaStore) StartTransaction(sagaID, ruleName string) error {
	ims.mu.Lock()
	defer ims.mu.Unlock()

	ims.transactions[sagaID] = make([]*SagaEffect, 0)
	return nil
}

// RecordEffect implements SagaStore
func (ims *InMemorySagaStore) RecordEffect(sagaID, verb string, args map[string]interface{}) error {
	ims.mu.Lock()
	defer ims.mu.Unlock()

	if effects, exists := ims.transactions[sagaID]; exists {
		effect := &SagaEffect{
			Verb:      verb,
			Args:      args,
			Status:    "pending",
			Timestamp: time.Now(),
		}
		ims.transactions[sagaID] = append(effects, effect)
	}
	return nil
}

// MarkSuccess implements SagaStore
func (ims *InMemorySagaStore) MarkSuccess(sagaID, verb string) error {
	ims.mu.Lock()
	defer ims.mu.Unlock()

	if effects, exists := ims.transactions[sagaID]; exists {
		for i := len(effects) - 1; i >= 0; i-- {
			if effects[i].Verb == verb && effects[i].Status == "pending" {
				effects[i].Status = "success"
				break
			}
		}
	}
	return nil
}

// MarkFailed implements SagaStore
func (ims *InMemorySagaStore) MarkFailed(sagaID, verb string, reason error) error {
	ims.mu.Lock()
	defer ims.mu.Unlock()

	if effects, exists := ims.transactions[sagaID]; exists {
		for i := len(effects) - 1; i >= 0; i-- {
			if effects[i].Verb == verb && effects[i].Status == "pending" {
				effects[i].Status = "failed"
				effects[i].Error = reason.Error()
				break
			}
		}
	}
	return nil
}

// MarkCompensated implements SagaStore
func (ims *InMemorySagaStore) MarkCompensated(sagaID, verb string) error {
	ims.mu.Lock()
	defer ims.mu.Unlock()

	if effects, exists := ims.transactions[sagaID]; exists {
		for _, effect := range effects {
			if effect.Verb == verb {
				effect.Status = "compensated"
				break
			}
		}
	}
	return nil
}

// GetTransactionEffects implements SagaStore
func (ims *InMemorySagaStore) GetTransactionEffects(sagaID string) ([]*SagaEffect, error) {
	ims.mu.RLock()
	defer ims.mu.RUnlock()

	if effects, exists := ims.transactions[sagaID]; exists {
		// Return a copy
		result := make([]*SagaEffect, len(effects))
		copy(result, effects)
		return result, nil
	}
	return nil, fmt.Errorf("saga transaction not found: %s", sagaID)
}

// GetActiveSagas implements SagaStore
func (ims *InMemorySagaStore) GetActiveSagas() ([]string, error) {
	ims.mu.RLock()
	defer ims.mu.RUnlock()

	sagas := make([]string, 0, len(ims.transactions))
	for sagaID := range ims.transactions {
		sagas = append(sagas, sagaID)
	}
	return sagas, nil
}

// CompleteSaga implements SagaStore
func (ims *InMemorySagaStore) CompleteSaga(sagaID string) error {
	ims.mu.Lock()
	defer ims.mu.Unlock()

	delete(ims.transactions, sagaID)
	return nil
}
