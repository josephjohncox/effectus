// Package common provides shared execution interfaces and utilities
package common

import (
	"context"
	"fmt"

	eff "github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/schema"
	"github.com/effectus/effectus-go/schema/verb"
)

// VerbRegistry defines the interface for verb registry access
// This is shared between list and flow execution systems
type VerbRegistry interface {
	GetVerb(name string) (VerbSpec, bool)
}

// VerbSpec defines the interface for verb specifications
// This is shared between list and flow execution systems
type VerbSpec interface {
	GetCapability() verb.Capability
	GetResourceSet() verb.ResourceSet
	GetExecutor() VerbExecutor
	GetInverse() string // Method to get inverse verb name for compensation
	IsIdempotent() bool
	IsCommutative() bool
	IsExclusive() bool
}

// VerbExecutor defines the interface for verb execution
// This is shared between list and flow execution systems
type VerbExecutor interface {
	Execute(ctx context.Context, args map[string]interface{}) (interface{}, error)
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

	executor := verbSpec.GetExecutor()
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

	// Execute the verb
	return executor.Execute(context.Background(), args)
}

// resolveValue resolves a value from facts or literals
func (ea *ExecutorAdapter) resolveValue(value interface{}, facts Facts) (interface{}, error) {
	switch v := value.(type) {
	case string:
		// Check if it's a fact path reference
		factVal, exists := facts.Get(v)
		if !exists {
			return nil, fmt.Errorf("fact not found: %s", v)
		}
		return factVal, nil
	default:
		return v, nil
	}
}

// VerbRegistryAdapter adapts VerbRegistry to schema.SagaVerbRegistry
// This is shared between list and flow packages
type VerbRegistryAdapter struct {
	Registry VerbRegistry
}

// NewVerbRegistryAdapter creates a new registry adapter
func NewVerbRegistryAdapter(registry VerbRegistry) *VerbRegistryAdapter {
	return &VerbRegistryAdapter{Registry: registry}
}

// GetVerb implements schema.SagaVerbRegistry
func (vra *VerbRegistryAdapter) GetVerb(name string) (schema.SagaVerbSpec, bool) {
	spec, exists := vra.Registry.GetVerb(name)
	if !exists {
		return nil, false
	}
	return &VerbSpecAdapter{Spec: spec}, true
}

// VerbSpecAdapter adapts VerbSpec to schema.SagaVerbSpec
// This is shared between list and flow packages
type VerbSpecAdapter struct {
	Spec VerbSpec
}

// GetCapability implements schema.SagaVerbSpec
func (vsa *VerbSpecAdapter) GetCapability() verb.Capability {
	return vsa.Spec.GetCapability()
}

// GetResourceSet implements schema.SagaVerbSpec
func (vsa *VerbSpecAdapter) GetResourceSet() verb.ResourceSet {
	return vsa.Spec.GetResourceSet()
}

// GetInverse implements schema.SagaVerbSpec
func (vsa *VerbSpecAdapter) GetInverse() string {
	return vsa.Spec.GetInverse()
}

// IsIdempotent implements schema.SagaVerbSpec
func (vsa *VerbSpecAdapter) IsIdempotent() bool {
	return vsa.Spec.IsIdempotent()
}

// IsCommutative implements schema.SagaVerbSpec
func (vsa *VerbSpecAdapter) IsCommutative() bool {
	return vsa.Spec.IsCommutative()
}

// IsExclusive implements schema.SagaVerbSpec
func (vsa *VerbSpecAdapter) IsExclusive() bool {
	return vsa.Spec.IsExclusive()
}
