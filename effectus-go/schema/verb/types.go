// Package verb provides the verb registry and specification for Effectus
package verb

import (
	"context"
)

// Executor defines the interface for verb implementations
type Executor interface {
	// Execute executes the verb with the given arguments
	Execute(ctx context.Context, args map[string]interface{}) (interface{}, error)
}

// Spec represents a verb specification
type Spec struct {
	// Name is the verb name
	Name string `json:"name"`

	// ArgTypes defines the types for verb arguments
	ArgTypes map[string]interface{} `json:"arg_types"`

	// ReturnType is the verb's return type
	ReturnType interface{} `json:"return_type"`

	// Capability is the required capability
	Capability Capability `json:"capability"`

	// Inverse is the name of the verb that reverses this one
	Inverse string `json:"inverse,omitempty"`

	// Description is a human-readable description
	Description string `json:"description,omitempty"`

	// Executor is the implementation of the verb
	Executor Executor `json:"-"`
}

// NewSpec creates a new verb specification
func NewSpec(name string, cap Capability, argTypes map[string]interface{}, returnType interface{}) *Spec {
	return &Spec{
		Name:       name,
		ArgTypes:   argTypes,
		ReturnType: returnType,
		Capability: cap,
	}
}

// WithInverse adds an inverse verb to the specification
func (s *Spec) WithInverse(inverse string) *Spec {
	s.Inverse = inverse
	return s
}

// WithDescription adds a description to the specification
func (s *Spec) WithDescription(desc string) *Spec {
	s.Description = desc
	return s
}

// WithExecutor adds an executor to the specification
func (s *Spec) WithExecutor(executor Executor) *Spec {
	s.Executor = executor
	return s
}

// FunctionExecutor is a simple executor that wraps a function
type FunctionExecutor struct {
	Fn func(ctx context.Context, args map[string]interface{}) (interface{}, error)
}

// Execute implements the Executor interface
func (e *FunctionExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return e.Fn(ctx, args)
}

// NewFunctionExecutor creates a new function executor
func NewFunctionExecutor(fn func(ctx context.Context, args map[string]interface{}) (interface{}, error)) *FunctionExecutor {
	return &FunctionExecutor{
		Fn: fn,
	}
}
