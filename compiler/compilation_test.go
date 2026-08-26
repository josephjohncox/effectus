package compiler

import (
	"testing"

	"github.com/effectus/effectus-go/loader"
	"github.com/stretchr/testify/require"
)

func TestExtensionCompilerFailsClosedWithoutExecutionPlanner(t *testing.T) {
	result, err := NewExtensionCompiler().Compile(t.Context(), loader.NewExtensionManager())
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.Success)
	require.Nil(t, result.CompiledUnit)
	require.Len(t, result.Errors, 1)
	require.Equal(t, "planning_error", result.Errors[0].Type)
	require.Contains(t, result.Errors[0].Message, ErrExtensionExecutionPlanUnsupported.Error())
}
