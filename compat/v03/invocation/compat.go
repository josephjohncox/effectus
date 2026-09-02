// Package invocation preserves the v0.3 invocation vocabulary during migration.
package invocation

import root "github.com/josephjohncox/effectus/invocation"

type Direction = root.Direction
type OutcomeClass = root.OutcomeClass
type FencingStatus = root.FencingStatus
type FencingGrant = root.FencingGrant
type Saga = root.Saga
type Context = root.Context
type Request = root.Request
type Outcome = root.Outcome
type Executor = root.Executor
type HTTPExecutor = root.HTTPExecutor

// ResolverDescriptorProvider is the v0.3 resolver-descriptor contract.
//
// It intentionally remains owned by this package: the current root interface
// returns a typed descriptor and an error, while v0.3 returned any.
type ResolverDescriptorProvider interface {
	InvocationResolverDescriptor() any
}

const (
	DirectionForward                  = root.DirectionForward
	DirectionCompensation             = root.DirectionCompensation
	OutcomeSuccess                    = root.OutcomeSuccess
	OutcomeRetryableKnownNotCommitted = root.OutcomeRetryableKnownNotCommitted
	OutcomePermanentFailure           = root.OutcomePermanentFailure
	OutcomeUnknown                    = root.OutcomeUnknown
	OutcomeStaleFence                 = root.OutcomeStaleFence
	FencingNotRequested               = root.FencingNotRequested
	FencingLocalLockOnly              = root.FencingLocalLockOnly
	FencingPropagated                 = root.FencingPropagated
	FencingAcknowledged               = root.FencingAcknowledged
	FencingStaleRejected              = root.FencingStaleRejected
	HeaderExecutionID                 = root.HeaderExecutionID
	HeaderSagaID                      = root.HeaderSagaID
	HeaderEffectID                    = root.HeaderEffectID
	HeaderAttempt                     = root.HeaderAttempt
	HeaderDirection                   = root.HeaderDirection
	HeaderArgumentHash                = root.HeaderArgumentHash
	HeaderContractHash                = root.HeaderContractHash
	HeaderFencingGrants               = root.HeaderFencingGrants
	HeaderDeadline                    = root.HeaderDeadline
	HeaderOutcome                     = root.HeaderOutcome
	HeaderIdempotencyKey              = root.HeaderIdempotencyKey
)

var NewHTTPExecutor = root.NewHTTPExecutor
var ValidateOutcome = root.ValidateOutcome
