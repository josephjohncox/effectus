package runtime

import (
	effectusv1 "github.com/josephjohncox/effectus/gen/effectus/v1"
	"github.com/josephjohncox/effectus/schema"
)

func grpcExecutionState(state string) effectusv1.ExecutionState {
	switch schema.ExecutionState(state) {
	case schema.ExecutionAdmitting:
		return effectusv1.ExecutionState_EXECUTION_STATE_ADMITTING
	case schema.ExecutionAccepted:
		return effectusv1.ExecutionState_EXECUTION_STATE_ACCEPTED
	case schema.ExecutionRunning:
		return effectusv1.ExecutionState_EXECUTION_STATE_RUNNING
	case schema.ExecutionCompleted:
		return effectusv1.ExecutionState_EXECUTION_STATE_COMPLETED
	case schema.ExecutionFailed:
		return effectusv1.ExecutionState_EXECUTION_STATE_FAILED
	case schema.ExecutionBlockedUnknown:
		return effectusv1.ExecutionState_EXECUTION_STATE_BLOCKED_UNKNOWN
	case schema.ExecutionBlockedFence:
		return effectusv1.ExecutionState_EXECUTION_STATE_BLOCKED_FENCE
	case schema.ExecutionBlockedDependency:
		return effectusv1.ExecutionState_EXECUTION_STATE_BLOCKED_DEPENDENCY
	case schema.ExecutionBlockedCompensation:
		return effectusv1.ExecutionState_EXECUTION_STATE_BLOCKED_COMPENSATION
	default:
		return effectusv1.ExecutionState_EXECUTION_STATE_UNSPECIFIED
	}
}
