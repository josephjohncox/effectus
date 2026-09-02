package flow

import (
	"testing"

	"github.com/josephjohncox/effectus/ast"
	"github.com/stretchr/testify/require"
)

func TestCompileStepsRejectsUndefinedLaterBindingBeforeExecution(t *testing.T) {
	program, err := compileSteps([]*ast.Step{
		{Verb: "First"},
		{Verb: "Second", Args: []*ast.StepArg{{Name: "value", Value: &ast.ArgValue{VarRef: "$missing"}}}},
	}, nil, nil)
	require.Nil(t, program)
	require.ErrorContains(t, err, "undefined variable reference: $missing")
}

func TestCompileStepsResolvesCheckedResultSlot(t *testing.T) {
	program, err := compileSteps([]*ast.Step{
		{Verb: "First", BindName: "result"},
		{Verb: "Second", Args: []*ast.StepArg{{Name: "value", Value: &ast.ArgValue{VarRef: "$result"}}}},
	}, nil, nil)
	require.NoError(t, err)

	executor := NewMockExecutor()
	executor.results = []interface{}{"first-result", nil}
	executor.errors = []error{nil, nil}
	_, err = Run(program, executor)
	require.NoError(t, err)
	require.Len(t, executor.effects, 2)
	require.Equal(t, "first-result", executor.effects[1].Payload.(map[string]interface{})["value"])
}
