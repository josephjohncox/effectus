package compiler

import (
	"bytes"
	"errors"
	"testing"

	effectusv1 "github.com/effectus/effectus-go/gen/effectus/v1"
	"github.com/effectus/effectus-go/ir"
	"github.com/stretchr/testify/require"
)

func checkedTestEnvironment() ir.Environment {
	return ir.Environment{
		Facts: map[string]string{
			"order.id": "string", "order.amount": "int", "order.ready": "bool", "order.tags": "list<string>",
		},
		Verbs: map[string]ir.VerbContract{
			"charge": {
				Arguments: map[string]string{"amount": "int", "order_id": "string"}, RequiredArgs: []string{"order_id", "amount"}, ResultType: "string",
				InverseVerb: "refund", RetryPolicy: ir.RetryPolicy{MaxAttempts: 3, InitialBackoffMillis: 10, MaxBackoffMillis: 100},
				IdempotencyPolicy: ir.IdempotencySinkGuaranteed, FencingRequired: true,
			},
			"refund": {
				Arguments: map[string]string{"amount": "int", "order_id": "string"}, RequiredArgs: []string{"amount", "order_id"}, ResultType: "void",
				IdempotencyPolicy: ir.IdempotencySinkGuaranteed, FencingRequired: true,
			},
			"record": {Arguments: map[string]string{"receipt": "string"}, RequiredArgs: []string{"receipt"}, ResultType: "void"},
		},
		Functions: map[string]ir.FunctionContract{
			"lower": {ArgumentTypes: []string{"string"}, ReturnType: "string", Pure: true, Total: true},
		},
	}
}

func TestCompileCheckedMixedSourcesDeterministicAndRoundTrips(t *testing.T) {
	environment := checkedTestEnvironment()
	listSource := Source{Path: "rules/z.eff", Data: []byte(`
rule "charge-list" priority 8 {
  when { order.ready == true }
  then { receipt = charge(order_id: order.id, amount: order.amount) }
  when { order.amount >= 10 }
  then { record(receipt: $receipt) }
}`)}
	flowSource := Source{Path: "flows/a.effx", Data: []byte(`
flow "charge-flow" priority 4 {
  when { lower(order.id) == "abc" && order.tags contains "vip" }
  steps {
    receipt = charge(amount: 12, order_id: order.id)
    record(receipt: $receipt)
  }
}`)}

	first, err := CompileChecked(t.Context(), []Source{listSource, flowSource}, environment, CompileOptions{})
	require.NoError(t, err)
	second, err := CompileChecked(t.Context(), []Source{flowSource, listSource}, environment, CompileOptions{})
	require.NoError(t, err)
	require.Equal(t, first.Digest(), second.Digest())
	require.True(t, bytes.Equal(first.Marshal(), second.Marshal()))

	parsed, err := ir.Parse(first.Marshal(), environment, ir.Limits{})
	require.NoError(t, err)
	require.Equal(t, first.Digest(), parsed.Digest())
	require.True(t, bytes.Equal(first.Marshal(), parsed.Marshal()))

	artifact := first.CloneArtifact()
	require.Len(t, artifact.Plans, 2)
	require.Equal(t, effectusv1.SourceDialect_SOURCE_DIALECT_LIST, artifact.Plans[0].SourceDialect)
	require.Equal(t, effectusv1.SourceDialect_SOURCE_DIALECT_FLOW, artifact.Plans[1].SourceDialect)
	require.Equal(t, uint32(0), artifact.Plans[0].Steps[0].GetResultSlot())
	require.Equal(t, uint32(0), artifact.Plans[0].Steps[1].Arguments[0].Value.GetResultSlot())
	require.Equal(t, []string{"amount", "order_id"}, []string{artifact.Plans[0].Steps[0].Arguments[0].Name, artifact.Plans[0].Steps[0].Arguments[1].Name})
	require.Equal(t, uint32(3), artifact.Plans[0].Steps[0].RetryPolicy.MaxAttempts)
	require.Equal(t, effectusv1.IdempotencyPolicy_IDEMPOTENCY_POLICY_SINK_GUARANTEED, artifact.Plans[0].Steps[0].IdempotencyPolicy)
	require.Equal(t, effectusv1.FencingRequirement_FENCING_REQUIREMENT_REQUIRED, artifact.Plans[0].Steps[0].FencingRequirement)
	require.Equal(t, "refund", artifact.Plans[0].Steps[0].Compensation.InverseVerb)
}

func TestCompileCheckedCanonicalPriorityAndSourceOrder(t *testing.T) {
	environment := ir.Environment{Verbs: map[string]ir.VerbContract{}}
	sources := []Source{
		{Path: "b.eff", Data: []byte(`rule "b" priority 1 {}`)},
		{Path: "a.eff", Data: []byte(`rule "low" priority 1 {} rule "high" priority 9 {}`)},
	}
	checked, err := CompileChecked(t.Context(), sources, environment, CompileOptions{})
	require.NoError(t, err)
	plans := checked.CloneArtifact().Plans
	require.Equal(t, []string{"high", "low", "b"}, []string{plans[0].Id, plans[1].Id, plans[2].Id})
	require.Equal(t, []uint32{1, 0, 2}, []uint32{plans[0].SourceOrder, plans[1].SourceOrder, plans[2].SourceOrder})
}

func TestCompileCheckedRejectsResultSlotAndContractErrors(t *testing.T) {
	environment := checkedTestEnvironment()
	tests := []struct {
		name   string
		source string
		part   string
	}{
		{name: "forward result", source: `flow "bad" priority 1 { when {} steps { record(receipt: $later) later = charge(amount: 1, order_id: order.id) } }`, part: "not available"},
		{name: "duplicate result", source: `flow "bad" priority 1 { when {} steps { value = charge(amount: 1, order_id: order.id) value = charge(amount: 1, order_id: order.id) } }`, part: "redefines"},
		{name: "void result", source: `flow "bad" priority 1 { when {} steps { value = record(receipt: "x") } }`, part: "binds a void result"},
		{name: "contract mismatch", source: `flow "bad" priority 1 { when {} steps { charge(amount: "wrong", order_id: order.id) } }`, part: "incompatible"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := CompileChecked(t.Context(), []Source{{Path: "bad.effx", Data: []byte(test.source)}}, environment, CompileOptions{})
			require.ErrorContains(t, err, test.part)
		})
	}
}

func TestCompileCheckedCompensationValidation(t *testing.T) {
	environment := checkedTestEnvironment()
	source := Source{Path: "flow.effx", Data: []byte(`flow "saga" priority 1 { when {} steps { charge(amount: 1, order_id: order.id) } }`)}
	checked, err := CompileChecked(t.Context(), []Source{source}, environment, CompileOptions{ExecutionPolicy: ExecutionPolicyCompensating})
	require.NoError(t, err)
	require.Equal(t, ExecutionPolicyCompensating, checked.CloneArtifact().Plans[0].ExecutionPolicy)

	contract := environment.Verbs["charge"]
	contract.InverseVerb = ""
	environment.Verbs["charge"] = contract
	_, err = CompileChecked(t.Context(), []Source{source}, environment, CompileOptions{ExecutionPolicy: ExecutionPolicyCompensating})
	require.ErrorContains(t, err, "declare an inverse")
}

func TestCompileCheckedRejectsUnsafeAndUnsupportedPredicates(t *testing.T) {
	environment := checkedTestEnvironment()
	environment.Functions["clock"] = ir.FunctionContract{ReturnType: "int", Pure: false, Total: true}
	_, err := CompileChecked(t.Context(), []Source{{Path: "bad.eff", Data: []byte(`rule "bad" priority 1 { when { clock() > 0 } then {} }`)}}, environment, CompileOptions{})
	require.ErrorContains(t, err, "not declared pure and total")

	_, err = CompileChecked(t.Context(), []Source{{Path: "bad.eff", Data: []byte(`rule "bad" priority 1 { when { order.ready ? true : false } then {} }`)}}, environment, CompileOptions{})
	require.Error(t, err)
}

func TestCompileCheckedHonorsArtifactLimits(t *testing.T) {
	_, err := CompileChecked(t.Context(), []Source{{Path: "a.eff", Data: []byte(`rule "one" priority 1 {} rule "two" priority 1 {}`)}}, ir.Environment{}, CompileOptions{Limits: ir.Limits{MaxPlans: 1}})
	require.Error(t, err)
	require.True(t, errors.Is(err, ir.ErrLimitExceeded))
}

func TestCompileCheckedContractChangeChangesArtifact(t *testing.T) {
	source := Source{Path: "flow.effx", Data: []byte(`flow "charge" priority 1 { when {} steps { charge(amount: 1, order_id: order.id) } }`)}
	firstEnvironment := checkedTestEnvironment()
	first, err := CompileChecked(t.Context(), []Source{source}, firstEnvironment, CompileOptions{})
	require.NoError(t, err)
	secondEnvironment := checkedTestEnvironment()
	contract := secondEnvironment.Verbs["charge"]
	contract.RetryPolicy.MaxAttempts++
	secondEnvironment.Verbs["charge"] = contract
	second, err := CompileChecked(t.Context(), []Source{source}, secondEnvironment, CompileOptions{})
	require.NoError(t, err)
	require.NotEqual(t, first.Digest(), second.Digest())
}
