package runtime

import (
	"testing"

	effectusv1 "github.com/josephjohncox/effectus/gen/effectus/v1"
	"github.com/josephjohncox/effectus/ir"
	"github.com/josephjohncox/effectus/schema"
	"github.com/stretchr/testify/require"
)

type bytesLiteralCase struct {
	name    string
	typeID  string
	literal *effectusv1.Literal
	want    any
}

func bytesLiteralCases() []bytesLiteralCase {
	bytes := func(value []byte) *effectusv1.Literal {
		return &effectusv1.Literal{Kind: &effectusv1.Literal_BytesValue{BytesValue: value}}
	}
	list := &effectusv1.Literal{Kind: &effectusv1.Literal_ListValue{ListValue: &effectusv1.LiteralList{Values: []*effectusv1.Literal{bytes(nil)}}}}
	object := &effectusv1.Literal{Kind: &effectusv1.Literal_ObjectValue{ObjectValue: &effectusv1.LiteralObject{Fields: []*effectusv1.LiteralField{
		{Name: "blob", Value: bytes(nil)}, {Name: "values", Value: list},
	}}}}
	return []bytesLiteralCase{
		{name: "nil-bytes", typeID: "bytes", literal: bytes(nil), want: ""},
		{name: "empty-bytes", typeID: "bytes", literal: bytes([]byte{}), want: ""},
		{name: "nonempty-bytes", typeID: "bytes", literal: bytes([]byte{0, 255}), want: "AP8="},
		{name: "nested-list", typeID: "list<bytes>", literal: list, want: []any{""}},
		{name: "nested-object", typeID: "Envelope", literal: object, want: map[string]any{"blob": "", "values": []any{""}}},
	}
}

func checkedBytesLiteral(t *testing.T, test bytesLiteralCase) (ir.Environment, *ir.Checked) {
	t.Helper()
	environment := ir.Environment{
		Facts: map[string]string{"input": test.typeID},
		Types: map[string]ir.TypeDefinition{"Envelope": {
			Kind: ir.TypeKindObject, Fields: map[string]string{"blob": "bytes", "values": "list<bytes>"}, RequiredFields: []string{"blob", "values"},
		}},
		Verbs: map[string]ir.VerbContract{"Send": {Arguments: map[string]string{"value": test.typeID}, ResultType: "void"}},
	}
	checked := compileLanguage(t, environment, `rule "literal" priority 1 { when { true } then { Send(value: input) } }`, "eff")
	artifact := checked.CloneArtifact()
	artifact.Plans[0].Steps[0].Arguments[0].Value = &effectusv1.Value{Kind: &effectusv1.Value_Literal{Literal: test.literal}}
	checked, err := ir.Check(artifact, environment, ir.Limits{})
	require.NoError(t, err)
	parsed, err := ir.Parse(checked.Marshal(), environment, ir.Limits{})
	require.NoError(t, err)
	require.Equal(t, checked.Digest(), parsed.Digest())
	return environment, parsed
}

func TestCheckedBytesLiteralInitialIntentMatchesReenqueue(t *testing.T) {
	for _, test := range bytesLiteralCases() {
		t.Run(test.name, func(t *testing.T) {
			_, checked := checkedBytesLiteral(t, test)
			plan := checked.CloneArtifact().Plans[0]
			outbox := schema.NewInMemoryOutboxStore()
			sagaID := schema.StableSagaID(test.name, plan.Id)
			_, err := outbox.CreateSaga(t.Context(), schema.CreateSagaRequest{Namespace: "bytes", SagaID: sagaID, ExecutionID: test.name, PlanID: plan.Id, PlanDigest: checked.Digest(), Serial: true})
			require.NoError(t, err)
			initial, err := durableInitialStep(plan, nil, sagaID)
			require.NoError(t, err)
			persisted, err := outbox.EnqueueStep(t.Context(), initial)
			require.NoError(t, err)
			wantJSON, wantHash, err := schema.CanonicalJSON(map[string]any{"value": test.want})
			require.NoError(t, err)
			require.Equal(t, wantJSON, persisted.Arguments)
			require.Equal(t, wantHash, persisted.ArgumentHash)
			// This deliberately seeds initial intent. The in-memory execution
			// ledger alone does not persist DurableAdmission.InitialSteps.
			again, err := schema.EnqueueCheckedStep(t.Context(), outbox, checked, schema.CheckedEnqueueRequest{SagaID: sagaID, PlanID: plan.Id, EffectID: plan.Steps[0].Id})
			require.NoError(t, err)
			require.Equal(t, persisted, again)
		})
	}
}

func TestCheckedBytesLiteralKeepsExistingNonemptyIdentityAndRejectsConflict(t *testing.T) {
	test := bytesLiteralCases()[2]
	_, checked := checkedBytesLiteral(t, test)
	plan := checked.CloneArtifact().Plans[0]
	outbox := schema.NewInMemoryOutboxStore()
	sagaID := schema.StableSagaID("existing", plan.Id)
	_, err := outbox.CreateSaga(t.Context(), schema.CreateSagaRequest{Namespace: "bytes", SagaID: sagaID, ExecutionID: "existing", PlanID: plan.Id, PlanDigest: checked.Digest(), Serial: true})
	require.NoError(t, err)
	initial, err := durableInitialStep(plan, nil, sagaID)
	require.NoError(t, err)
	// The older literal resolver supplied raw bytes. Existing canonical JSON,
	// hashes, dispatch IDs and idempotency keys must stay unchanged.
	initial.Arguments["value"] = []byte{0, 255}
	persisted, err := outbox.EnqueueStep(t.Context(), initial)
	require.NoError(t, err)
	require.Equal(t, `{"value":"AP8="}`, string(persisted.Arguments))
	again, err := schema.EnqueueCheckedStep(t.Context(), outbox, checked, schema.CheckedEnqueueRequest{SagaID: sagaID, PlanID: plan.Id, EffectID: plan.Steps[0].Id})
	require.NoError(t, err)
	require.Equal(t, persisted, again)
	initial.Arguments["value"] = ""
	_, err = outbox.EnqueueStep(t.Context(), initial)
	require.ErrorIs(t, err, schema.ErrIdentityConflict)
	unchanged, err := outbox.GetDispatch(t.Context(), persisted.ID)
	require.NoError(t, err)
	require.Equal(t, persisted, unchanged)
}
