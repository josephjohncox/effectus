// Package workflow defines durable saga and outbox contracts without storage implementations.
package workflow

import (
	"context"
	"encoding/json"
	"time"

	"github.com/josephjohncox/effectus/invocation"
)

type SagaState string

const (
	SagaRunning             SagaState = "running"
	SagaCompleted           SagaState = "completed"
	SagaCompensating        SagaState = "compensating"
	SagaCompensated         SagaState = "compensated"
	SagaFailed              SagaState = "failed"
	SagaBlockedUnknown      SagaState = "blocked_unknown"
	SagaBlockedDependency   SagaState = "blocked_dependency"
	SagaBlockedFence        SagaState = "blocked_fence"
	SagaBlockedCompensation SagaState = "blocked_compensation"
)

type DispatchState string

const (
	DispatchQueued          DispatchState = "queued"
	DispatchInFlight        DispatchState = "in_flight"
	DispatchSucceeded       DispatchState = "succeeded"
	DispatchRetryWait       DispatchState = "retry_wait"
	DispatchFailedPermanent DispatchState = "failed_permanent"
	DispatchBlockedUnknown  DispatchState = "blocked_unknown"
	DispatchBlockedFence    DispatchState = "blocked_fence"
)

type StepState string

const (
	StepPending     StepState = "pending"
	StepSucceeded   StepState = "succeeded"
	StepFailed      StepState = "failed"
	StepCompensated StepState = "compensated"
)

type FencingRequirement struct {
	Authority string `json:"authority"`
	Resource  string `json:"resource"`
}

type SagaInstance struct {
	Namespace   string
	SagaID      string
	ExecutionID string
	PlanID      string
	PlanDigest  string
	State       SagaState
	Serial      bool
	Revision    uint64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type CreateSagaRequest struct {
	Namespace   string
	SagaID      string
	ExecutionID string
	PlanID      string
	PlanDigest  string
	Serial      bool

	// AllowUnstableIdentityForTest is restricted to backend contract tests.
	AllowUnstableIdentityForTest bool `json:"-"`
}

type EnqueueStepRequest struct {
	SagaID                string
	EffectID              string
	Sequence              int
	Verb                  string
	ContractHash          string
	Arguments             map[string]any
	CompensationVerb      string
	CompensationContract  string
	CompensationArguments map[string]any
	Fencing               []FencingRequirement
}

type SagaStep struct {
	SagaID                   string
	EffectID                 string
	Sequence                 int
	Verb                     string
	ContractHash             string
	Arguments                json.RawMessage
	ArgumentHash             string
	CompensationVerb         string
	CompensationContract     string
	CompensationArguments    json.RawMessage
	CompensationArgumentHash string
	Fencing                  []FencingRequirement
	State                    StepState
	Result                   json.RawMessage
}

type Dispatch struct {
	ID             string
	SagaID         string
	EffectID       string
	Sequence       int
	Direction      invocation.Direction
	Verb           string
	ContractHash   string
	Arguments      json.RawMessage
	ArgumentHash   string
	IdempotencyKey string
	State          DispatchState
	Attempt        uint64
	LeaseOwner     string
	LeaseToken     string
	LeaseDeadline  time.Time
	NextAttemptAt  time.Time
	Fencing        []FencingRequirement
	FencingGrants  []invocation.FencingGrant
	LastOutcome    invocation.OutcomeClass
	LastError      string
	Result         json.RawMessage
	Revision       uint64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type ClaimOptions struct {
	Owner            string
	LeaseDuration    time.Duration
	Now              time.Time
	TargetDispatchID string
}

type Completion struct {
	DispatchID    string
	Attempt       uint64
	LeaseToken    string
	Outcome       invocation.OutcomeClass
	Result        json.RawMessage
	Error         string
	NextAttemptAt time.Time
	Exhausted     bool
	Now           time.Time
}

type DispatchAttempt struct {
	DispatchID    string
	Attempt       uint64
	LeaseOwner    string
	LeaseToken    string
	LeaseDeadline time.Time
	FencingGrants []invocation.FencingGrant
	Outcome       invocation.OutcomeClass
	Error         string
	StartedAt     time.Time
	CompletedAt   time.Time
}

type UnknownOutcomeRetryPolicy interface {
	RetryUnknownOutcome(invocation.Request) bool
}

type OutboxStore interface {
	CreateSaga(context.Context, CreateSagaRequest) (*SagaInstance, error)
	EnqueueStep(context.Context, EnqueueStepRequest) (*Dispatch, error)
	ClaimDispatch(context.Context, ClaimOptions) (*Dispatch, error)
	SaveFencingGrants(context.Context, string, uint64, string, []invocation.FencingGrant) error
	CompleteDispatch(context.Context, Completion) error
	CompleteSaga(context.Context, string) error
	GetSaga(context.Context, string) (*SagaInstance, error)
	GetDispatch(context.Context, string) (*Dispatch, error)
	ListDispatches(context.Context, string) ([]*Dispatch, error)
	ListAttempts(context.Context, string) ([]DispatchAttempt, error)
}
