package runtime

import (
	"testing"

	"github.com/effectus/effectus-go/compiler"
	"github.com/stretchr/testify/require"
)

func TestExecuteWorkflowFailsClosedOnEmptyPlan(t *testing.T) {
	runtime := &ExecutionRuntime{
		compiledUnit: &compiler.CompiledUnit{ExecutionPlan: &compiler.ExecutionPlan{}},
		state:        StateReady,
	}

	err := runtime.ExecuteWorkflow(t.Context(), nil)
	require.ErrorIs(t, err, compiler.ErrExtensionExecutionPlanUnsupported)
	require.Equal(t, StateReady, runtime.state)
}
