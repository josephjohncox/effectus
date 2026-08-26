package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRuleManagerPlaceholderPipelineFailsClosed(t *testing.T) {
	compiler := NewRuleCompiler(nil)
	compiled, err := compiler.CompileRuleset(t.Context(), "orders", nil)
	require.Nil(t, compiled)
	require.ErrorIs(t, err, ErrRuleManagerPipelineUnsupported)
	require.Equal(t, "unsupported", compiler.GetVersion())

	validator := NewRuleValidator(nil)
	require.ErrorIs(t, validator.ValidateRuleset(t.Context(), &CompiledRuleset{}), ErrRuleManagerPipelineUnsupported)

	controller := NewDeploymentController(nil, NewInMemoryRuleStorage())
	result, err := controller.Deploy(t.Context(), &StoredRuleset{}, "production", nil)
	require.Nil(t, result)
	require.ErrorIs(t, err, ErrRuleManagerPipelineUnsupported)
}
