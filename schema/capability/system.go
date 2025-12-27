package capability

import (
	"fmt"
	"sync"
	"time"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/schema/types"
)

// CapabilitySystem provides comprehensive capability-based protection
type CapabilitySystem struct {
	// locks tracks active locks by (capability, key) pairs
	locks map[lockKey]*activeLock

	// policies defines capability policies for different resources
	policies map[string]*ResourcePolicy

	// mu protects concurrent access
	mu sync.RWMutex
}

// lockKey represents a unique lock identifier
type lockKey struct {
	capability string
	key        string
}

// activeLock represents an active lock on a resource
type activeLock struct {
	holder    string    // identifier of the lock holder
	acquired  time.Time // when the lock was acquired
	expires   time.Time // when the lock expires
	exclusive bool      // whether this is an exclusive lock
}

// ResourcePolicy defines capability requirements for a resource type
type ResourcePolicy struct {
	ResourceType       string
	RequiredCapability types.Capability
	AllowConcurrent    bool
	MaxConcurrency     int
	DefaultTimeout     time.Duration
	Validators         []CapabilityValidator
}

// CapabilityValidator checks if a capability request should be allowed
type CapabilityValidator interface {
	Validate(ctx CapabilityContext) error
}

// CapabilityContext provides context for capability validation
type CapabilityContext struct {
	Effect     effectus.Effect
	Capability types.Capability
	Key        string
	Holder     string
	Timestamp  time.Time
}

// LockResult represents the result of acquiring a lock
type LockResult struct {
	LockID     string
	FenceToken int64
	ExpiresAt  time.Time
	Unlock     func()
}

// NewCapabilitySystem creates a new capability system
func NewCapabilitySystem() *CapabilitySystem {
	return &CapabilitySystem{
		locks:    make(map[lockKey]*activeLock),
		policies: make(map[string]*ResourcePolicy),
	}
}

// RegisterResourcePolicy registers a capability policy for a resource type
func (cs *CapabilitySystem) RegisterResourcePolicy(policy *ResourcePolicy) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.policies[policy.ResourceType] = policy
}

// AcquireLock attempts to acquire a capability-based lock
func (cs *CapabilitySystem) AcquireLock(capability types.Capability, key, holder string) (*LockResult, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	lockKey := lockKey{
		capability: capability.String(),
		key:        key,
	}

	// Check if lock already exists
	if existing, exists := cs.locks[lockKey]; exists {
		if !existing.expires.After(time.Now()) {
			// Lock has expired, remove it
			delete(cs.locks, lockKey)
		} else {
			// Lock is still active
			if existing.exclusive || capability == types.CapabilityModify ||
				capability == types.CapabilityCreate || capability == types.CapabilityDelete {
				return nil, fmt.Errorf("resource locked: capability %s on key %s held by %s until %v",
					capability.String(), key, existing.holder, existing.expires)
			}
		}
	}

	// Create new lock
	now := time.Now()
	timeout := 30 * time.Second // default timeout

	// Check resource policy for timeout
	if policy, exists := cs.policies[key]; exists {
		if policy.DefaultTimeout > 0 {
			timeout = policy.DefaultTimeout
		}
	}

	lock := &activeLock{
		holder:    holder,
		acquired:  now,
		expires:   now.Add(timeout),
		exclusive: capability != types.CapabilityRead,
	}

	cs.locks[lockKey] = lock

	// Create fence token (simplified)
	fenceToken := now.Unix()

	// Create unlock function
	unlock := func() {
		cs.mu.Lock()
		defer cs.mu.Unlock()
		delete(cs.locks, lockKey)
	}

	return &LockResult{
		LockID:     fmt.Sprintf("%s:%s:%s", capability.String(), key, holder),
		FenceToken: fenceToken,
		ExpiresAt:  lock.expires,
		Unlock:     unlock,
	}, nil
}

// ValidateCapability validates that an effect can be executed with the given capability
func (cs *CapabilitySystem) ValidateCapability(effect effectus.Effect, requiredCapability types.Capability) error {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	// Check if there's a policy for this resource type
	if policy, exists := cs.policies[effect.Verb]; exists {
		// Check if the required capability meets the policy
		if !requiredCapability.CanPerform(policy.RequiredCapability) {
			return fmt.Errorf("insufficient capability: effect requires %s but policy demands %s",
				requiredCapability.String(), policy.RequiredCapability.String())
		}

		// Run validators
		ctx := CapabilityContext{
			Effect:     effect,
			Capability: requiredCapability,
			Key:        extractKey(effect),
			Holder:     "system", // TODO: Extract from context
			Timestamp:  time.Now(),
		}

		for _, validator := range policy.Validators {
			if err := validator.Validate(ctx); err != nil {
				return fmt.Errorf("capability validation failed: %w", err)
			}
		}
	}

	return nil
}

// GetActiveLocks returns information about currently active locks
func (cs *CapabilitySystem) GetActiveLocks() map[string]LockInfo {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	result := make(map[string]LockInfo)
	now := time.Now()

	for key, lock := range cs.locks {
		if lock.expires.After(now) {
			result[fmt.Sprintf("%s:%s", key.capability, key.key)] = LockInfo{
				Capability: key.capability,
				Key:        key.key,
				Holder:     lock.holder,
				Acquired:   lock.acquired,
				Expires:    lock.expires,
				Exclusive:  lock.exclusive,
			}
		}
	}

	return result
}

// LockInfo provides information about a lock
type LockInfo struct {
	Capability string
	Key        string
	Holder     string
	Acquired   time.Time
	Expires    time.Time
	Exclusive  bool
}

// CleanupExpiredLocks removes expired locks from the system
func (cs *CapabilitySystem) CleanupExpiredLocks() {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	now := time.Now()
	for key, lock := range cs.locks {
		if !lock.expires.After(now) {
			delete(cs.locks, key)
		}
	}
}

// CheckConflicts checks if two effects would conflict based on their capabilities
func (cs *CapabilitySystem) CheckConflicts(effect1, effect2 effectus.Effect, cap1, cap2 types.Capability) bool {
	key1 := extractKey(effect1)
	key2 := extractKey(effect2)

	// Different keys don't conflict
	if key1 != key2 {
		return false
	}

	// Both read operations don't conflict
	if cap1 == types.CapabilityRead && cap2 == types.CapabilityRead {
		return false
	}

	// Any write operation conflicts with any other operation on the same key
	return cap1 != types.CapabilityRead || cap2 != types.CapabilityRead
}

// extractKey extracts a resource key from an effect
// This is a simplified implementation - in practice this would be more sophisticated
func extractKey(effect effectus.Effect) string {
	// Try to extract key from payload
	if payload, ok := effect.Payload.(map[string]interface{}); ok {
		if key, exists := payload["key"]; exists {
			if keyStr, ok := key.(string); ok {
				return keyStr
			}
		}
		if id, exists := payload["id"]; exists {
			if idStr, ok := id.(string); ok {
				return idStr
			}
		}
	}

	// Fallback to verb name
	return effect.Verb
}

// DefaultCapabilityValidator provides basic capability validation
type DefaultCapabilityValidator struct{}

// Validate implements CapabilityValidator
func (dcv *DefaultCapabilityValidator) Validate(ctx CapabilityContext) error {
	// Basic validation - ensure capability is not none
	if ctx.Capability == types.CapabilityNone {
		return fmt.Errorf("capability cannot be none")
	}

	return nil
}

// TimeBasedCapabilityValidator validates capabilities based on time constraints
type TimeBasedCapabilityValidator struct {
	AllowedHours []int // Hours when operations are allowed (0-23)
}

// Validate implements CapabilityValidator
func (tbcv *TimeBasedCapabilityValidator) Validate(ctx CapabilityContext) error {
	hour := ctx.Timestamp.Hour()

	for _, allowedHour := range tbcv.AllowedHours {
		if hour == allowedHour {
			return nil
		}
	}

	return fmt.Errorf("operations not allowed at hour %d", hour)
}

// CapabilityAwareExecutor wraps an executor with capability checking
type CapabilityAwareExecutor struct {
	executor  effectus.Executor
	capSystem *CapabilitySystem
	holderID  string
}

// NewCapabilityAwareExecutor creates a new capability-aware executor
func NewCapabilityAwareExecutor(executor effectus.Executor, capSystem *CapabilitySystem, holderID string) *CapabilityAwareExecutor {
	return &CapabilityAwareExecutor{
		executor:  executor,
		capSystem: capSystem,
		holderID:  holderID,
	}
}

// Do executes an effect with capability checking and locking
func (cae *CapabilityAwareExecutor) Do(effect effectus.Effect) (interface{}, error) {
	// Determine required capability based on verb
	capability := inferCapabilityFromVerb(effect.Verb)

	// Validate capability
	if err := cae.capSystem.ValidateCapability(effect, capability); err != nil {
		return nil, fmt.Errorf("capability validation failed: %w", err)
	}

	// Acquire lock
	key := extractKey(effect)
	lockResult, err := cae.capSystem.AcquireLock(capability, key, cae.holderID)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer lockResult.Unlock()

	// Execute the effect
	return cae.executor.Do(effect)
}

// inferCapabilityFromVerb infers the required capability from a verb name
// This is a simplified heuristic - in practice this would be configured per verb
func inferCapabilityFromVerb(verb string) types.Capability {
	switch {
	case containsAny(verb, []string{"read", "get", "list", "find", "query"}):
		return types.CapabilityRead
	case containsAny(verb, []string{"create", "add", "new", "insert"}):
		return types.CapabilityCreate
	case containsAny(verb, []string{"update", "modify", "edit", "change"}):
		return types.CapabilityModify
	case containsAny(verb, []string{"delete", "remove", "destroy"}):
		return types.CapabilityDelete
	default:
		return types.CapabilityModify // Default to modify for unknown verbs
	}
}

// containsAny checks if a string contains any of the given substrings
func containsAny(s string, substrs []string) bool {
	for _, substr := range substrs {
		if len(s) >= len(substr) {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
		}
	}
	return false
}
