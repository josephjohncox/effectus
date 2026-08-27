// Package invocation defines metadata that must cross every external effect boundary.
package invocation

import (
	"context"
	"fmt"
	"time"
)

// Direction distinguishes a forward mutation from its compensation.
type Direction string

const (
	DirectionForward      Direction = "forward"
	DirectionCompensation Direction = "compensation"
)

// OutcomeClass states whether the destination can know that a mutation committed.
type OutcomeClass string

const (
	OutcomeSuccess                    OutcomeClass = "success"
	OutcomeRetryableKnownNotCommitted OutcomeClass = "retryable_failure_known_not_committed"
	OutcomePermanentFailure           OutcomeClass = "permanent_failure"
	OutcomeUnknown                    OutcomeClass = "unknown_outcome"
	OutcomeStaleFence                 OutcomeClass = "stale_fence"
)

// FencingStatus describes observation without claiming destination enforcement.
type FencingStatus string

const (
	FencingNotRequested  FencingStatus = "not_requested"
	FencingLocalLockOnly FencingStatus = "local_lock_only"
	FencingPropagated    FencingStatus = "propagated"
	FencingAcknowledged  FencingStatus = "acknowledged"
	FencingStaleRejected FencingStatus = "stale_rejected"
)

// FencingGrant is immutable system metadata. Adapters must not source these
// values from caller arguments or caller headers.
type FencingGrant struct {
	Authority string `json:"authority"`
	Resource  string `json:"resource"`
	Token     uint64 `json:"token"`
}

// Saga identifies one durable effect occurrence and dispatch attempt.
type Saga struct {
	SagaID         string    `json:"saga_id"`
	EffectID       string    `json:"effect_id"`
	Attempt        uint64    `json:"attempt"`
	Direction      Direction `json:"direction"`
	IdempotencyKey string    `json:"idempotency_key"`
}

// Context contains transport-neutral invocation metadata.
type Context struct {
	RequestID     string         `json:"request_id"`
	ExecutionID   string         `json:"execution_id"`
	Saga          Saga           `json:"saga"`
	FencingGrants []FencingGrant `json:"fencing_grants,omitempty"`
	Deadline      time.Time      `json:"deadline"`
}

// Request is the immutable call delivered to an invocation-aware executor.
type Request struct {
	Metadata     Context        `json:"metadata"`
	Verb         string         `json:"verb"`
	Arguments    map[string]any `json:"arguments"`
	ArgumentHash string         `json:"argument_hash"`
	ContractHash string         `json:"contract_hash"`
}

// Outcome is the executor's explicit result classification.
type Outcome struct {
	Class  OutcomeClass
	Result any
	Err    error
}

// Executor accepts stable identity and fencing metadata as part of its API.
type Executor interface {
	Invoke(context.Context, Request) Outcome
}

// ResolverDescriptorProvider returns immutable, JSON-serializable resolver
// input covered by a generation digest.
type ResolverDescriptorProvider interface {
	InvocationResolverDescriptor() any
}

// ValidateOutcome rejects incomplete or contradictory classifications.
func ValidateOutcome(outcome Outcome) error {
	switch outcome.Class {
	case OutcomeSuccess:
		if outcome.Err != nil {
			return fmt.Errorf("successful invocation has an error")
		}
	case OutcomeRetryableKnownNotCommitted, OutcomePermanentFailure, OutcomeUnknown, OutcomeStaleFence:
		if outcome.Err == nil {
			return fmt.Errorf("invocation outcome %s requires an error", outcome.Class)
		}
	default:
		return fmt.Errorf("unknown invocation outcome class %q", outcome.Class)
	}
	return nil
}
