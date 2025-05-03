// list/executor.go
package list

import (
	"context"
	"fmt"
	"sync"

	eff "github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/common"
	"github.com/effectus/effectus-go/eval"
	"github.com/effectus/effectus-go/pathutil"
)

// ExecutorOption defines an option for configuring the executor
type ExecutorOption func(*Executor)

// WithSaga enables saga-style compensation for failed executions
func WithSaga(store eval.SagaStore) ExecutorOption {
	return func(e *Executor) {
		e.sagaStore = store
		e.sagaEnabled = true
	}
}

// Executor is the main executor for list rules
type Executor struct {
	verbRegistry VerbRegistry
	locks        *LockManager
	sagaEnabled  bool
	sagaStore    eval.SagaStore
	mu           sync.Mutex
}

// VerbRegistry defines the interface for verb registry access
type VerbRegistry interface {
	GetVerb(name string) (VerbSpec, bool)
}

// VerbSpec defines the interface for verb specifications
type VerbSpec interface {
	GetCapability() uint32
	GetExecutor() VerbExecutor
	GetInverse() string // Method to get inverse verb name
}

// VerbExecutor defines the interface for verb execution
type VerbExecutor interface {
	Execute(ctx context.Context, args map[string]interface{}) (interface{}, error)
}

// NewExecutor creates a new executor for list rules
func NewExecutor(verbRegistry VerbRegistry, options ...ExecutorOption) *Executor {
	executor := &Executor{
		verbRegistry: verbRegistry,
		locks:        NewLockManager(),
	}

	// Apply options
	for _, option := range options {
		option(executor)
	}

	return executor
}

// effectusFactsAdapter adapts common.Facts to eff.Facts
type effectusFactsAdapter struct {
	facts common.Facts
}

func (f *effectusFactsAdapter) Get(path pathutil.Path) (interface{}, bool) {
	return f.facts.Get(path)
}

func (f *effectusFactsAdapter) Schema() eff.SchemaInfo {
	// Simply implement the required interface
	return &effectusSchemaAdapter{f.facts.Schema()}
}

// effectusSchemaAdapter adapts common.SchemaInfo to eff.SchemaInfo
type effectusSchemaAdapter struct {
	schema common.SchemaInfo
}

func (s *effectusSchemaAdapter) ValidatePath(path pathutil.Path) bool {
	return s.schema.ValidatePath(path)
}

// ExecuteRule executes a single rule against facts
func (le *Executor) ExecuteRule(ctx context.Context, rule *CompiledRule, facts common.Facts) ([]eff.Effect, error) {
	// Check if rule predicates match
	matched := true
	for _, pred := range rule.Predicates {
		// Get fact value
		value, exists := facts.Get(pred.Path)
		if !exists {
			matched = false
			break
		}

		// Compare based on operator
		if !eval.CompareFact(value, pred.Op, pred.Lit) {
			matched = false
			break
		}
	}

	if !matched {
		return nil, nil
	}

	// Start a transaction if saga is enabled
	var txID string
	var effects []eff.Effect
	var err error

	if le.sagaEnabled {
		txID, err = le.sagaStore.StartTransaction(rule.Name)
		if err != nil {
			return nil, fmt.Errorf("starting transaction: %w", err)
		}
	}

	// Execute effects
	effects, err = le.executeEffects(ctx, txID, rule.Effects, facts)

	// Handle error with compensation if needed
	if err != nil && le.sagaEnabled {
		compensateErr := le.compensate(ctx, txID)
		if compensateErr != nil {
			// Log compensation error but return original error
			fmt.Printf("Compensation failed: %v\n", compensateErr)
		}
	}

	return effects, err
}

// executeEffects executes a list of effects with proper locking
func (le *Executor) executeEffects(ctx context.Context, txID string, effects []*Effect, facts common.Facts) ([]eff.Effect, error) {
	var result []eff.Effect

	// Collect locks needed
	locks := make(map[string][]string) // capability -> []keys
	for _, effect := range effects {
		// Get verb spec
		verbSpec, exists := le.verbRegistry.GetVerb(effect.Verb)
		if !exists {
			return nil, fmt.Errorf("unknown verb: %s", effect.Verb)
		}

		// TODO: Implement effect capability -> key mapping
		// Use the verb's capability for locking
		cap := fmt.Sprintf("%d", verbSpec.GetCapability())

		// Extract keys from effect
		// This would be based on arguments that represent resource IDs
		keys := []string{effect.Verb} // Default to just the verb name

		locks[cap] = append(locks[cap], keys...)
	}

	// Acquire locks in order
	unlock, err := le.locks.AcquireLocks(locks)
	if err != nil {
		return nil, fmt.Errorf("acquiring locks: %w", err)
	}
	defer unlock()

	// Execute effects in order
	for _, effect := range effects {
		// Get verb spec and executor
		verbSpec, exists := le.verbRegistry.GetVerb(effect.Verb)
		if !exists {
			return nil, fmt.Errorf("unknown verb: %s", effect.Verb)
		}

		executor := verbSpec.GetExecutor()
		if executor == nil {
			return nil, fmt.Errorf("verb %s has no executor", effect.Verb)
		}

		// Prepare arguments
		args := make(map[string]interface{})
		for name, value := range effect.Args {
			resolvedValue, err := le.resolveValue(value, facts)
			if err != nil {
				return nil, fmt.Errorf("resolving argument %s: %w", name, err)
			}
			args[name] = resolvedValue
		}

		// Record in saga store if enabled
		if le.sagaEnabled {
			if err := le.sagaStore.RecordEffect(txID, effect.Verb, args); err != nil {
				return nil, fmt.Errorf("recording effect: %w", err)
			}
		}

		// Execute the verb
		execResult, err := executor.Execute(ctx, args)
		if err != nil {
			return nil, fmt.Errorf("executing verb %s: %w", effect.Verb, err)
		}

		// Mark as successful in saga store
		if le.sagaEnabled {
			if err := le.sagaStore.MarkSuccess(txID, effect.Verb); err != nil {
				return nil, fmt.Errorf("marking success: %w", err)
			}
		}

		// Add to results
		executed := &ExecutedEffect{
			Verb:   effect.Verb,
			Args:   args,
			Result: execResult,
		}
		result = append(result, executed.AsEffect())
	}

	return result, nil
}

// resolveValue resolves a value from facts or literals
func (le *Executor) resolveValue(value interface{}, facts common.Facts) (interface{}, error) {
	// Handle different value types
	switch v := value.(type) {
	case string:
		// Check if it's a fact path reference
		if len(v) > 0 && v[0] == '$' {
			pathStr := v[1:] // Remove $ prefix

			// Convert string path to pathutil.Path
			path, err := pathutil.ParseString(pathStr)
			if err != nil {
				return nil, fmt.Errorf("invalid path: %s: %w", pathStr, err)
			}

			factVal, exists := facts.Get(path)
			if !exists {
				return nil, fmt.Errorf("fact not found: %s", pathStr)
			}
			return factVal, nil
		}
		return v, nil
	default:
		return v, nil
	}
}

// compensate performs compensation for a failed transaction
func (le *Executor) compensate(ctx context.Context, txID string) error {
	// Get executed effects in reverse order
	effects, err := le.sagaStore.GetTransactionEffects(txID)
	if err != nil {
		return fmt.Errorf("getting transaction effects: %w", err)
	}

	// Execute inverse verbs in reverse order
	for i := len(effects) - 1; i >= 0; i-- {
		effect := effects[i]

		// Skip already failed or compensated effects
		if effect.Status != "success" {
			continue
		}

		// Get verb spec
		verbSpec, exists := le.verbRegistry.GetVerb(effect.Verb)
		if !exists {
			return fmt.Errorf("unknown verb: %s", effect.Verb)
		}

		// Check if inverse exists
		inverseName := verbSpec.GetInverse()
		if inverseName == "" {
			return fmt.Errorf("verb %s has no inverse for compensation", effect.Verb)
		}

		// Get inverse verb spec
		inverseSpec, exists := le.verbRegistry.GetVerb(inverseName)
		if !exists {
			return fmt.Errorf("inverse verb %s not found", inverseName)
		}

		inverseExecutor := inverseSpec.GetExecutor()
		if inverseExecutor == nil {
			return fmt.Errorf("inverse verb %s has no executor", inverseName)
		}

		// Execute inverse verb
		_, err := inverseExecutor.Execute(ctx, effect.Args)
		if err != nil {
			return fmt.Errorf("executing inverse verb %s: %w", inverseName, err)
		}

		// Mark as compensated
		if err := le.sagaStore.MarkCompensated(txID, effect.Verb); err != nil {
			return fmt.Errorf("marking compensated: %w", err)
		}
	}

	return nil
}

// ExecutedEffect represents an executed effect that implements the effectus.Effect interface
type ExecutedEffect struct {
	Verb   string
	Args   map[string]interface{}
	Result interface{}
}

// GetVerb returns the verb of the effect
func (e *ExecutedEffect) GetVerb() string {
	return e.Verb
}

// GetArgs returns the arguments of the effect
func (e *ExecutedEffect) GetArgs() map[string]interface{} {
	return e.Args
}

// GetResult returns the result of the effect execution
func (e *ExecutedEffect) GetResult() interface{} {
	return e.Result
}

// Ensure ExecutedEffect implements the effectus.Effect interface by converting it
func (e *ExecutedEffect) AsEffect() eff.Effect {
	return eff.Effect{
		Verb:    e.Verb,
		Payload: e.Args,
	}
}

// LockManager manages capability-based locks
type LockManager struct {
	locks map[string]struct{}
	mu    sync.Mutex
}

// NewLockManager creates a new lock manager
func NewLockManager() *LockManager {
	return &LockManager{
		locks: make(map[string]struct{}),
	}
}

// AcquireLocks acquires all needed locks
func (lm *LockManager) AcquireLocks(locks map[string][]string) (func(), error) {
	// Sort locks by capability and key
	// TODO: Implement proper lock ordering to prevent deadlock

	lm.mu.Lock()

	// Check if any locks are already held
	for cap, keys := range locks {
		for _, key := range keys {
			lockKey := fmt.Sprintf("%s:%s", cap, key)
			if _, exists := lm.locks[lockKey]; exists {
				lm.mu.Unlock()
				return nil, fmt.Errorf("lock %s already held", lockKey)
			}
		}
	}

	// Acquire all locks
	acquired := make([]string, 0)
	for cap, keys := range locks {
		for _, key := range keys {
			lockKey := fmt.Sprintf("%s:%s", cap, key)
			lm.locks[lockKey] = struct{}{}
			acquired = append(acquired, lockKey)
		}
	}

	lm.mu.Unlock()

	// Return unlock function
	return func() {
		lm.mu.Lock()
		defer lm.mu.Unlock()

		for _, lockKey := range acquired {
			delete(lm.locks, lockKey)
		}
	}, nil
}

