// Package ledger defines durable execution admission and recovery contracts
// without database or queue implementations.
package ledger

import (
	"context"
	"encoding/json"
	"time"

	"github.com/effectus/effectus-go/schema/workflow"
)

type ExecutionState string

const (
	ExecutionAdmitting           ExecutionState = "admitting"
	ExecutionAccepted            ExecutionState = "accepted"
	ExecutionRunning             ExecutionState = "running"
	ExecutionCompleted           ExecutionState = "completed"
	ExecutionFailed              ExecutionState = "failed"
	ExecutionBlockedUnknown      ExecutionState = "blocked_unknown"
	ExecutionBlockedFence        ExecutionState = "blocked_fence"
	ExecutionBlockedDependency   ExecutionState = "blocked_dependency"
	ExecutionBlockedCompensation ExecutionState = "blocked_compensation"
)

func IsTerminalExecutionState(state ExecutionState) bool {
	switch state {
	case ExecutionCompleted, ExecutionFailed, ExecutionBlockedUnknown, ExecutionBlockedFence,
		ExecutionBlockedDependency, ExecutionBlockedCompensation:
		return true
	default:
		return false
	}
}

type ExecutionArtifact struct {
	GenerationDigest string
	IRDigest         string
	IRBytes          []byte
	Environment      json.RawMessage
	ExecutorManifest json.RawMessage
	FunctionManifest json.RawMessage
	SourceDigest     string
	CompilerMetadata json.RawMessage
	CreatedAt        time.Time
}

type ExecutionPlanRecord struct {
	ExecutionID string
	PlanID      string
	SagaID      string
	Ordinal     int
	State       string
}

type ExecutionRecord struct {
	ExecutionID       string
	AdmissionIdentity string
	RequestHash       string
	Ruleset           string
	Version           string
	TenantNamespace   string
	MergePolicy       string
	GenerationDigest  string
	EffectiveFacts    json.RawMessage
	State             ExecutionState
	Revision          uint64
	RecoveryOwner     string
	RecoveryToken     string
	RecoveryDeadline  time.Time
	LastError         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	Plans             []ExecutionPlanRecord
}

type FactApplication struct {
	ExecutionID     string
	FactEventID     string
	MergePolicy     string
	Facts           json.RawMessage
	AppliedRevision uint64
}

type DurableAdmission struct {
	Artifact        ExecutionArtifact
	Execution       ExecutionRecord
	FactApplication FactApplication
	Plans           []ExecutionPlanRecord
	Sagas           []workflow.CreateSagaRequest
	InitialSteps    []workflow.EnqueueStepRequest
}

type ExecutionLease struct {
	ExecutionID string
	Owner       string
	Token       string
	Deadline    time.Time
	Revision    uint64
}

type ExecutionLedger interface {
	PutArtifact(context.Context, ExecutionArtifact) error
	GetArtifact(context.Context, string) (ExecutionArtifact, error)
	AdmitExecution(context.Context, DurableAdmission) (ExecutionRecord, bool, error)
	GetExecution(context.Context, string) (ExecutionRecord, error)
	GetExecutionByAdmission(context.Context, string) (ExecutionRecord, error)
	SetExecutionState(context.Context, string, uint64, ExecutionState, string) (ExecutionRecord, error)
	LeaseExecutions(context.Context, string, int, time.Duration) ([]ExecutionLease, error)
	FinishExecutionLease(context.Context, ExecutionLease, ExecutionState, string) error
}

type AtomicAdmissionStore interface {
	ExecutionLedger
	AdmitExecutionAtomic(context.Context, DurableAdmission) (ExecutionRecord, bool, error)
}
