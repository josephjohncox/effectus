package verb

import (
	"context"
	"fmt"
	"sync"

	"github.com/effectus/effectus-go"
)

// UnifiedVerbSystem provides a single interface for all verb operations
type UnifiedVerbSystem struct {
	// registry holds all registered verbs
	registry *VerbRegistry

	// executors maps verb names to their executus executors (for backward compatibility)
	executors map[string]effectus.Executor

	// mu protects concurrent access
	mu sync.RWMutex
}

// NewUnifiedVerbSystem creates a new unified verb system
func NewUnifiedVerbSystem() *UnifiedVerbSystem {
	return &UnifiedVerbSystem{
		registry:  NewVerbRegistry(),
		executors: make(map[string]effectus.Executor),
	}
}

// RegisterVerb registers a verb with its specification
func (uvs *UnifiedVerbSystem) RegisterVerb(spec *StandardVerbSpec, executor effectus.Executor) error {
	uvs.mu.Lock()
	defer uvs.mu.Unlock()

	// Register the spec (no error returned from Register)
	uvs.registry.Register(spec)

	// Register the executor for backward compatibility
	uvs.executors[spec.Name] = executor

	return nil
}

// GetVerbSpec returns the specification for a verb
func (uvs *UnifiedVerbSystem) GetVerbSpec(name string) (*StandardVerbSpec, bool) {
	uvs.mu.RLock()
	defer uvs.mu.RUnlock()

	return uvs.registry.GetVerb(name)
}

// CreateEffect creates a properly structured effect from verb name and args
func (uvs *UnifiedVerbSystem) CreateEffect(verbName string, args map[string]interface{}) (*effectus.Effect, error) {
	uvs.mu.RLock()
	spec, exists := uvs.registry.GetVerb(verbName)
	uvs.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("verb '%s' not registered", verbName)
	}

	// Validate arguments
	if err := uvs.validateArgs(spec, args); err != nil {
		return nil, fmt.Errorf("invalid arguments for verb '%s': %w", verbName, err)
	}

	// Create effect with current effectus.Effect structure
	effect := &effectus.Effect{
		Verb:    verbName,
		Payload: args,
	}

	return effect, nil
}

// Execute executes an effect using the registered executor
func (uvs *UnifiedVerbSystem) Execute(ctx context.Context, effect *effectus.Effect) (interface{}, error) {
	uvs.mu.RLock()
	spec, specExists := uvs.registry.GetVerb(effect.Verb)
	executor, executorExists := uvs.executors[effect.Verb]
	uvs.mu.RUnlock()

	if !specExists {
		return nil, fmt.Errorf("no specification found for verb '%s'", effect.Verb)
	}

	// Use the verb's built-in executor if available
	if spec.ExecutorImpl != nil {
		if args, ok := effect.Payload.(map[string]interface{}); ok {
			return spec.ExecutorImpl.Execute(ctx, args)
		}
		return nil, fmt.Errorf("invalid payload type for verb '%s'", effect.Verb)
	}

	// Fall back to external executor for backward compatibility
	if executorExists {
		return executor.Do(*effect)
	}

	return nil, fmt.Errorf("no executor available for verb '%s'", effect.Verb)
}

// CanExecute checks if a verb can be executed with the given arguments
func (uvs *UnifiedVerbSystem) CanExecute(verbName string, args map[string]interface{}) error {
	uvs.mu.RLock()
	defer uvs.mu.RUnlock()

	spec, exists := uvs.registry.GetVerb(verbName)
	if !exists {
		return fmt.Errorf("verb '%s' not registered", verbName)
	}

	return uvs.validateArgs(spec, args)
}

// GetAllVerbs returns all registered verb specifications
func (uvs *UnifiedVerbSystem) GetAllVerbs() []*StandardVerbSpec {
	uvs.mu.RLock()
	defer uvs.mu.RUnlock()

	var specs []*StandardVerbSpec
	for _, spec := range uvs.registry.verbs {
		specs = append(specs, spec)
	}
	return specs
}

// GetCapabilityRequirement returns the capability requirement for a verb
func (uvs *UnifiedVerbSystem) GetCapabilityRequirement(verbName string) (Capability, error) {
	uvs.mu.RLock()
	defer uvs.mu.RUnlock()

	spec, exists := uvs.registry.GetVerb(verbName)
	if !exists {
		return Capability(0), fmt.Errorf("verb '%s' not registered", verbName)
	}

	return spec.Cap, nil
}

// validateArgs validates that the provided arguments match the verb specification
func (uvs *UnifiedVerbSystem) validateArgs(spec *StandardVerbSpec, args map[string]interface{}) error {
	// Check required arguments
	for _, required := range spec.RequiredArgs {
		if _, exists := args[required]; !exists {
			return fmt.Errorf("missing required argument: %s", required)
		}
	}

	// Check argument types
	for argName, argValue := range args {
		expectedType, exists := spec.ArgTypes[argName]
		if !exists {
			return fmt.Errorf("unexpected argument: %s", argName)
		}

		if !uvs.validateArgType(argValue, expectedType) {
			return fmt.Errorf("argument '%s' has invalid type, expected %s", argName, expectedType)
		}
	}

	return nil
}

// validateArgType checks if an argument value matches the expected type
func (uvs *UnifiedVerbSystem) validateArgType(value interface{}, expectedType string) bool {
	switch expectedType {
	case "string":
		_, ok := value.(string)
		return ok
	case "int", "integer":
		_, ok := value.(int)
		if !ok {
			_, ok = value.(int64)
		}
		return ok
	case "float", "double":
		_, ok := value.(float64)
		if !ok {
			_, ok = value.(float32)
		}
		return ok
	case "bool", "boolean":
		_, ok := value.(bool)
		return ok
	case "object", "map":
		_, ok := value.(map[string]interface{})
		return ok
	case "array", "list":
		_, ok := value.([]interface{})
		return ok
	default:
		// For custom types, assume validation passes
		// This should be enhanced with proper schema validation
		return true
	}
}

// GetVerbCapability returns the capability for a verb (helper method)
func (uvs *UnifiedVerbSystem) GetVerbCapability(verbName string) Capability {
	uvs.mu.RLock()
	defer uvs.mu.RUnlock()

	spec, exists := uvs.registry.GetVerb(verbName)
	if !exists {
		return CapNone
	}

	return spec.Cap
}

// CheckResourceConflicts checks if two verbs have conflicting resource access
func (uvs *UnifiedVerbSystem) CheckResourceConflicts(verb1, verb2 string) bool {
	uvs.mu.RLock()
	defer uvs.mu.RUnlock()

	hasConflicts, _ := uvs.registry.CheckVerbConflicts(verb1, verb2)
	return hasConflicts
}
