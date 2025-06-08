package verb

import (
	"context"
	"fmt"
	"time"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/schema/capability"
	"github.com/effectus/effectus-go/schema/types"
)

// CapabilityIntegratedVerbSystem combines the unified verb system with capability protection
type CapabilityIntegratedVerbSystem struct {
	verbSystem       *UnifiedVerbSystem
	capabilitySystem *capability.CapabilitySystem
	holderID         string
}

// NewCapabilityIntegratedVerbSystem creates a new integrated system
func NewCapabilityIntegratedVerbSystem(holderID string) *CapabilityIntegratedVerbSystem {
	return &CapabilityIntegratedVerbSystem{
		verbSystem:       NewUnifiedVerbSystem(),
		capabilitySystem: capability.NewCapabilitySystem(),
		holderID:         holderID,
	}
}

// RegisterVerb registers a verb with capability requirements
func (civs *CapabilityIntegratedVerbSystem) RegisterVerb(spec *StandardVerbSpec, executor effectus.Executor, requiredCapability types.Capability) error {
	// Register with unified verb system
	if err := civs.verbSystem.RegisterVerb(spec, executor); err != nil {
		return fmt.Errorf("failed to register verb: %w", err)
	}

	// Register capability policy
	policy := &capability.ResourcePolicy{
		ResourceType:       spec.Name,
		RequiredCapability: requiredCapability,
		AllowConcurrent:    !spec.IsExclusive(),
		DefaultTimeout:     30 * time.Second,
		Validators: []capability.CapabilityValidator{
			&capability.DefaultCapabilityValidator{},
		},
	}

	civs.capabilitySystem.RegisterResourcePolicy(policy)

	return nil
}

// ExecuteWithCapabilityCheck executes an effect with full capability validation and locking
func (civs *CapabilityIntegratedVerbSystem) ExecuteWithCapabilityCheck(ctx context.Context, verbName string, args map[string]interface{}, key string) (interface{}, error) {
	// Create effect using unified verb system
	effect, err := civs.verbSystem.CreateEffect(verbName, args)
	if err != nil {
		return nil, fmt.Errorf("failed to create effect: %w", err)
	}

	// Infer capability from verb name
	capability := inferCapabilityFromVerb(verbName)

	// Validate capability
	if err := civs.capabilitySystem.ValidateCapability(*effect, capability); err != nil {
		return nil, fmt.Errorf("capability validation failed: %w", err)
	}

	// Acquire lock
	lockResult, err := civs.capabilitySystem.AcquireLock(capability, key, civs.holderID)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer lockResult.Unlock()

	// Execute using unified verb system
	return civs.verbSystem.Execute(ctx, effect)
}

// GetVerbCapabilityRequirement returns the capability requirement for a verb
func (civs *CapabilityIntegratedVerbSystem) GetVerbCapabilityRequirement(verbName string) (types.Capability, error) {
	spec, exists := civs.verbSystem.GetVerbSpec(verbName)
	if !exists {
		return types.CapabilityNone, fmt.Errorf("verb '%s' not registered", verbName)
	}

	// Map verb capability to types capability
	return mapVerbCapToTypesCapability(spec.Cap), nil
}

// GetActiveLocks returns information about currently active locks
func (civs *CapabilityIntegratedVerbSystem) GetActiveLocks() map[string]capability.LockInfo {
	return civs.capabilitySystem.GetActiveLocks()
}

// CanExecuteConcurrently checks if two verbs can execute concurrently
func (civs *CapabilityIntegratedVerbSystem) CanExecuteConcurrently(verb1, verb2 string, key1, key2 string) bool {
	// Different keys can always execute concurrently
	if key1 != key2 {
		return true
	}

	// Check capability requirements
	cap1, err1 := civs.GetVerbCapabilityRequirement(verb1)
	cap2, err2 := civs.GetVerbCapabilityRequirement(verb2)

	if err1 != nil || err2 != nil {
		return false // Conservative approach - if we can't determine, don't allow
	}

	// Create dummy effects to check conflicts
	effect1 := effectus.Effect{Verb: verb1, Payload: map[string]interface{}{"key": key1}}
	effect2 := effectus.Effect{Verb: verb2, Payload: map[string]interface{}{"key": key2}}

	return !civs.capabilitySystem.CheckConflicts(effect1, effect2, cap1, cap2)
}

// RegisterTimeBasedPolicy registers a time-based policy for a verb
func (civs *CapabilityIntegratedVerbSystem) RegisterTimeBasedPolicy(verbName string, allowedHours []int) error {
	policy := &capability.ResourcePolicy{
		ResourceType:       verbName,
		RequiredCapability: types.CapabilityModify, // Default
		AllowConcurrent:    true,
		DefaultTimeout:     30 * time.Second,
		Validators: []capability.CapabilityValidator{
			&capability.DefaultCapabilityValidator{},
			&capability.TimeBasedCapabilityValidator{AllowedHours: allowedHours},
		},
	}

	civs.capabilitySystem.RegisterResourcePolicy(policy)
	return nil
}

// CleanupExpiredLocks removes expired locks
func (civs *CapabilityIntegratedVerbSystem) CleanupExpiredLocks() {
	civs.capabilitySystem.CleanupExpiredLocks()
}

// mapVerbCapToTypesCapability maps verb.Capability to types.Capability
func mapVerbCapToTypesCapability(verbCap Capability) types.Capability {
	switch {
	case verbCap&CapDelete != 0:
		return types.CapabilityDelete
	case verbCap&CapCreate != 0:
		return types.CapabilityCreate
	case verbCap&CapReadWrite != 0, verbCap&CapWrite != 0:
		return types.CapabilityModify
	case verbCap&CapRead != 0:
		return types.CapabilityRead
	default:
		return types.CapabilityNone
	}
}

// inferCapabilityFromVerb infers the required capability from a verb name
func inferCapabilityFromVerb(verb string) types.Capability {
	// Convert to lowercase for comparison
	verbLower := ""
	for _, char := range verb {
		if char >= 'A' && char <= 'Z' {
			verbLower += string(char + 32) // Convert to lowercase
		} else {
			verbLower += string(char)
		}
	}

	switch {
	case containsAnySubstring(verbLower, []string{"read", "get", "list", "find", "query", "search"}):
		return types.CapabilityRead
	case containsAnySubstring(verbLower, []string{"create", "add", "new", "insert", "make"}):
		return types.CapabilityCreate
	case containsAnySubstring(verbLower, []string{"update", "modify", "edit", "change", "set"}):
		return types.CapabilityModify
	case containsAnySubstring(verbLower, []string{"delete", "remove", "destroy", "drop"}):
		return types.CapabilityDelete
	default:
		return types.CapabilityModify // Default to modify for unknown verbs
	}
}

// containsAnySubstring checks if a string contains any of the given substrings
func containsAnySubstring(s string, substrs []string) bool {
	for _, substr := range substrs {
		if len(s) >= len(substr) {
			for i := 0; i <= len(s)-len(substr); i++ {
				match := true
				for j := 0; j < len(substr); j++ {
					if s[i+j] != substr[j] {
						match = false
						break
					}
				}
				if match {
					return true
				}
			}
		}
	}
	return false
}

// Example usage and helper functions for setting up common scenarios

// ExecutorAdapter adapts StandardVerbExecutor to effectus.Executor interface
type ExecutorAdapter struct {
	verbExecutor VerbExecutor
}

// NewExecutorAdapter creates a new adapter
func NewExecutorAdapter(verbExecutor VerbExecutor) *ExecutorAdapter {
	return &ExecutorAdapter{verbExecutor: verbExecutor}
}

// Do implements effectus.Executor by adapting to VerbExecutor.Execute
func (ea *ExecutorAdapter) Do(effect effectus.Effect) (interface{}, error) {
	// Convert payload to args map
	args, ok := effect.Payload.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid payload type for effect %s", effect.Verb)
	}

	// Execute using VerbExecutor
	return ea.verbExecutor.Execute(context.Background(), args)
}

// NOTE: Standard business verbs have been removed from the core system.
// Users should register their own domain-specific verbs.
// See examples/ directory for how to set up business verbs.
