// Package common provides shared execution interfaces and utilities
package common

import (
	"context"
	"fmt"

	eff "github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/schema/verb"
)

// VerbRegistry defines the interface for verb registry access
// This is shared between list and flow execution systems
type VerbRegistry interface {
	GetVerb(name string) (*verb.Spec, bool)
}

// ExecutorAdapter implements effectus.Executor using shared interfaces
// This eliminates duplication between list and flow packages
type ExecutorAdapter struct {
	VerbRegistry VerbRegistry
	Facts        Facts
}

// NewExecutorAdapter creates a new shared executor adapter
func NewExecutorAdapter(verbRegistry VerbRegistry, facts Facts) *ExecutorAdapter {
	return &ExecutorAdapter{
		VerbRegistry: verbRegistry,
		Facts:        facts,
	}
}

// Do implements effectus.Executor
func (ea *ExecutorAdapter) Do(effect eff.Effect) (interface{}, error) {
	// Get verb spec and executor
	verbSpec, exists := ea.VerbRegistry.GetVerb(effect.Verb)
	if !exists {
		return nil, fmt.Errorf("unknown verb: %s", effect.Verb)
	}

	executor := verbSpec.Executor
	if executor == nil {
		return nil, fmt.Errorf("verb %s has no executor", effect.Verb)
	}

	// Prepare arguments with fact resolution
	args := make(map[string]interface{})
	if payload, ok := effect.Payload.(map[string]interface{}); ok {
		for name, value := range payload {
			resolvedValue, err := ea.resolveValue(value, ea.Facts)
			if err != nil {
				return nil, fmt.Errorf("resolving argument %s: %w", name, err)
			}
			args[name] = resolvedValue
		}
	}

	if err := validateVerbArgs(verbSpec, args, ea.VerbRegistry); err != nil {
		return nil, fmt.Errorf("invalid args for %s: %w", verbSpec.Name, err)
	}

	// Execute the verb
	result, err := executor.Execute(context.Background(), args)
	if err != nil {
		return nil, err
	}

	if err := validateVerbReturn(verbSpec, result, ea.VerbRegistry); err != nil {
		return nil, fmt.Errorf("invalid return from %s: %w", verbSpec.Name, err)
	}

	return result, nil
}

// resolveValue resolves a value from facts or literals
func (ea *ExecutorAdapter) resolveValue(value interface{}, facts Facts) (interface{}, error) {
	switch v := value.(type) {
	case FactRef:
		factVal, exists := facts.Get(string(v))
		if !exists {
			return nil, fmt.Errorf("fact not found: %s", v)
		}
		return factVal, nil
	case string:
		return v, nil
	case []interface{}:
		resolved := make([]interface{}, 0, len(v))
		for _, item := range v {
			converted, err := ea.resolveValue(item, facts)
			if err != nil {
				return nil, err
			}
			resolved = append(resolved, converted)
		}
		return resolved, nil
	case map[string]interface{}:
		resolved := make(map[string]interface{}, len(v))
		for key, item := range v {
			converted, err := ea.resolveValue(item, facts)
			if err != nil {
				return nil, err
			}
			resolved[key] = converted
		}
		return resolved, nil
	default:
		return v, nil
	}
}

// NOTE: schema.SagaVerbRegistry now accepts *verb.Spec directly,
// so no adapter is required.
