package main

import (
	"encoding/json"
	"testing"

	"github.com/josephjohncox/effectus/compiler"
	"github.com/josephjohncox/effectus/ir"
	"github.com/stretchr/testify/require"
)

func TestRenderLegacyWorkflowsProducesCompilableEffx(t *testing.T) {
	output, err := renderLegacyWorkflows([]legacyWorkflowDefinition{{
		Name: "ordered", Priority: 3,
		Steps: []legacyWorkflowStepDefinition{
			{ID: "produce", Verb: "produce", Arguments: map[string]legacyWorkflowValue{"value": {Literal: json.RawMessage(`{"b":2,"a":1}`)}}, Result: "result"},
			{ID: "consume", Verb: "consume", Arguments: map[string]legacyWorkflowValue{"value": {Result: "result"}}},
		},
	}})
	require.NoError(t, err)
	require.Contains(t, output, `produce(value: {a: 1 b: 2})`)
	_, err = compiler.CompileChecked(t.Context(), []compiler.Source{{Path: "migration.effx", Data: []byte(output)}}, ir.Environment{
		Types: map[string]ir.TypeDefinition{"Payload": {Kind: ir.TypeKindObject, Fields: map[string]string{"a": "int", "b": "int"}, RequiredFields: []string{"a", "b"}}},
		Verbs: map[string]ir.VerbContract{
			"produce": {Arguments: map[string]string{"value": "Payload"}, RequiredArgs: []string{"value"}, ResultType: "string"},
			"consume": {Arguments: map[string]string{"value": "string"}, RequiredArgs: []string{"value"}, ResultType: "void"},
		},
	}, compiler.CompileOptions{})
	require.NoError(t, err)
}

func TestDecodeLegacyWorkflowsRejectsAmbiguousJSON(t *testing.T) {
	_, err := decodeLegacyWorkflows([]byte(`{"workflows":[],"workflows":[]}`))
	require.ErrorContains(t, err, "duplicate JSON object key")
	_, err = decodeLegacyWorkflows([]byte(`{"workflows":[{"name":"bad","steps":[],"unknown":true}]}`))
	require.ErrorContains(t, err, "unknown field")
}

func TestRenderLegacyWorkflowsRefusesAmbiguousBehavior(t *testing.T) {
	_, err := renderLegacyWorkflows([]legacyWorkflowDefinition{{Name: "parallel", Parallel: true}})
	require.ErrorContains(t, err, "parallel")
	_, err = renderLegacyWorkflows([]legacyWorkflowDefinition{{Name: "facts", Facts: map[string]string{"order.id": "string"}}})
	require.ErrorContains(t, err, "fact declarations")
	_, err = renderLegacyWorkflows([]legacyWorkflowDefinition{{Name: "forward", Steps: []legacyWorkflowStepDefinition{{Verb: "consume", Arguments: map[string]legacyWorkflowValue{"value": {Result: "future"}}}}}})
	require.ErrorContains(t, err, "not available")
}
