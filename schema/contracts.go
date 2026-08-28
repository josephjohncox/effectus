package schema

import (
	"github.com/effectus/effectus-go/schema/ledger"
	"github.com/effectus/effectus-go/schema/workflow"
)

// Execution-ledger compatibility aliases. New runtime code imports schema/ledger.
type ExecutionState = ledger.ExecutionState
type ExecutionArtifact = ledger.ExecutionArtifact
type ExecutionPlanRecord = ledger.ExecutionPlanRecord
type ExecutionRecord = ledger.ExecutionRecord
type FactApplication = ledger.FactApplication
type DurableAdmission = ledger.DurableAdmission
type ExecutionLease = ledger.ExecutionLease
type ExecutionLedger = ledger.ExecutionLedger
type AtomicAdmissionStore = ledger.AtomicAdmissionStore

const (
	ExecutionAdmitting           = ledger.ExecutionAdmitting
	ExecutionAccepted            = ledger.ExecutionAccepted
	ExecutionRunning             = ledger.ExecutionRunning
	ExecutionCompleted           = ledger.ExecutionCompleted
	ExecutionFailed              = ledger.ExecutionFailed
	ExecutionBlockedUnknown      = ledger.ExecutionBlockedUnknown
	ExecutionBlockedFence        = ledger.ExecutionBlockedFence
	ExecutionBlockedDependency   = ledger.ExecutionBlockedDependency
	ExecutionBlockedCompensation = ledger.ExecutionBlockedCompensation
)

func IsTerminalExecutionState(state ExecutionState) bool {
	return ledger.IsTerminalExecutionState(state)
}

// Durable workflow compatibility aliases. New contract consumers import schema/workflow.
type SagaState = workflow.SagaState
type DispatchState = workflow.DispatchState
type StepState = workflow.StepState
type FencingRequirement = workflow.FencingRequirement
type SagaInstance = workflow.SagaInstance
type CreateSagaRequest = workflow.CreateSagaRequest
type EnqueueStepRequest = workflow.EnqueueStepRequest
type SagaStep = workflow.SagaStep
type Dispatch = workflow.Dispatch
type ClaimOptions = workflow.ClaimOptions
type Completion = workflow.Completion
type DispatchAttempt = workflow.DispatchAttempt
type UnknownOutcomeRetryPolicy = workflow.UnknownOutcomeRetryPolicy
type OutboxStore = workflow.OutboxStore

const (
	SagaRunning             = workflow.SagaRunning
	SagaCompleted           = workflow.SagaCompleted
	SagaCompensating        = workflow.SagaCompensating
	SagaCompensated         = workflow.SagaCompensated
	SagaFailed              = workflow.SagaFailed
	SagaBlockedUnknown      = workflow.SagaBlockedUnknown
	SagaBlockedDependency   = workflow.SagaBlockedDependency
	SagaBlockedFence        = workflow.SagaBlockedFence
	SagaBlockedCompensation = workflow.SagaBlockedCompensation
	DispatchQueued          = workflow.DispatchQueued
	DispatchInFlight        = workflow.DispatchInFlight
	DispatchSucceeded       = workflow.DispatchSucceeded
	DispatchRetryWait       = workflow.DispatchRetryWait
	DispatchFailedPermanent = workflow.DispatchFailedPermanent
	DispatchBlockedUnknown  = workflow.DispatchBlockedUnknown
	DispatchBlockedFence    = workflow.DispatchBlockedFence
	StepPending             = workflow.StepPending
	StepSucceeded           = workflow.StepSucceeded
	StepFailed              = workflow.StepFailed
	StepCompensated         = workflow.StepCompensated
)
