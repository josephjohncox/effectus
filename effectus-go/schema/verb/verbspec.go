package verb

import (
	"context"
)

// VerbExecutor defines the interface for verb execution
type VerbExecutor interface {
	Execute(ctx context.Context, args map[string]interface{}) (interface{}, error)
}

// StandardVerbSpec is a standard implementation of a verb specification
// Note: This is kept for compatibility with extension loaders.
// The main registry uses *verb.Spec internally.
type StandardVerbSpec struct {
	Name         string
	Description  string
	Cap          Capability
	Resources    ResourceSet
	ArgTypes     map[string]string // Argument name -> type
	RequiredArgs []string
	ReturnType   string
	InverseVerb  string
	ExecutorImpl VerbExecutor
}

// GetCapability returns the verb's capability
func (s *StandardVerbSpec) GetCapability() Capability {
	return s.Cap
}

// GetResourceSet returns the set of resources this verb affects
func (s *StandardVerbSpec) GetResourceSet() ResourceSet {
	return s.Resources
}

// GetExecutor returns the executor implementation for this verb
func (s *StandardVerbSpec) GetExecutor() VerbExecutor {
	return s.ExecutorImpl
}

// GetInverse returns the name of the inverse verb (for compensation)
func (s *StandardVerbSpec) GetInverse() string {
	return s.InverseVerb
}

// IsIdempotent returns whether the verb is idempotent
func (s *StandardVerbSpec) IsIdempotent() bool {
	return s.Cap&CapIdempotent != 0
}

// IsCommutative returns whether the verb is commutative
func (s *StandardVerbSpec) IsCommutative() bool {
	return s.Cap&CapCommutative != 0
}

// IsExclusive returns whether the verb requires exclusive access
func (s *StandardVerbSpec) IsExclusive() bool {
	return s.Cap&CapExclusive != 0
}

// ErrVerbNotFound is returned when a requested verb is not found
type ErrVerbNotFound struct {
	Verb string
}

func (e ErrVerbNotFound) Error() string {
	return "verb not found: " + e.Verb
}
