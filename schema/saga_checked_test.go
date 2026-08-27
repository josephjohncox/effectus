package schema

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	effectusv1 "github.com/effectus/effectus-go/gen/effectus/v1"
	"github.com/effectus/effectus-go/ir"
	"github.com/stretchr/testify/require"
)

func TestEnqueueCheckedStepDerivesImmutableIdentity(t *testing.T) {
	environment := ir.Environment{Verbs: map[string]ir.VerbContract{
		"charge": {Arguments: map[string]string{"amount": "int"}, ResultType: "string"},
	}}
	environmentDigest, err := ir.EnvironmentDigest(environment)
	require.NoError(t, err)
	contractHash, err := ir.ContractHash(environment.Verbs["charge"])
	require.NoError(t, err)
	build := sha256.Sum256([]byte("compiler"))
	artifact := &effectusv1.RuleArtifact{
		FormatVersion: ir.FormatVersion, EnvironmentDigest: environmentDigest,
		Compiler: &effectusv1.CompilerMetadata{Name: "effectusc", Version: "test", BuildDigest: hex.EncodeToString(build[:])},
		Plans: []*effectusv1.Plan{{
			Id: "plan-checked", SourceDialect: effectusv1.SourceDialect_SOURCE_DIALECT_LIST,
			ExecutionPolicy: effectusv1.ExecutionPolicy_EXECUTION_POLICY_DURABLE_FAIL_FAST,
			Predicate: &effectusv1.Predicate{Expression: &effectusv1.Expression{Kind: &effectusv1.Expression_Literal{
				Literal: &effectusv1.Literal{Kind: &effectusv1.Literal_BoolValue{BoolValue: true}},
			}}},
			Steps: []*effectusv1.Step{{
				Id: "effect-checked", Verb: "charge", ContractHash: contractHash,
				Arguments: []*effectusv1.Argument{{Name: "amount", Value: &effectusv1.Value{Kind: &effectusv1.Value_Literal{
					Literal: &effectusv1.Literal{Kind: &effectusv1.Literal_IntValue{IntValue: 42}},
				}}}},
			}},
		}},
	}
	checked, err := ir.Check(artifact, environment, ir.Limits{})
	require.NoError(t, err)
	store := NewInMemoryOutboxStore()
	sagaID := StableSagaID("execution-checked", "plan-checked")
	_, err = store.CreateSaga(t.Context(), CreateSagaRequest{
		Namespace: "test", SagaID: sagaID, ExecutionID: "execution-checked",
		PlanID: "plan-checked", PlanDigest: checked.Digest(), Serial: true,
	})
	require.NoError(t, err)
	dispatch, err := EnqueueCheckedStep(t.Context(), store, checked, CheckedEnqueueRequest{
		SagaID: sagaID, PlanID: "plan-checked", EffectID: "effect-checked",
		Arguments: map[string]any{"amount": int64(42)},
	})
	require.NoError(t, err)
	require.Equal(t, "effect-checked", dispatch.EffectID)
	require.Equal(t, "charge", dispatch.Verb)
	require.Equal(t, contractHash, dispatch.ContractHash)
	require.Equal(t, 1, dispatch.Sequence)

	_, err = EnqueueCheckedStep(t.Context(), store, checked, CheckedEnqueueRequest{
		SagaID: sagaID, PlanID: "plan-checked", EffectID: "effect-checked",
		Arguments: map[string]any{"amount": int64(999)},
	})
	require.ErrorIs(t, err, ErrIdentityConflict)

	otherBuild := sha256.Sum256([]byte("other"))
	artifact.Compiler.BuildDigest = hex.EncodeToString(otherBuild[:])
	other, err := ir.Check(artifact, environment, ir.Limits{})
	require.NoError(t, err)
	_, err = EnqueueCheckedStep(t.Context(), store, other, CheckedEnqueueRequest{
		SagaID: sagaID, PlanID: "plan-checked", EffectID: "effect-checked", Arguments: map[string]any{"amount": int64(42)},
	})
	require.ErrorIs(t, err, ErrIdentityConflict)
}

func TestEnqueueCheckedStepRejectsUnstableSagaIdentity(t *testing.T) {
	environment := ir.Environment{Verbs: map[string]ir.VerbContract{"noop": {ResultType: "void"}}}
	environmentDigest, err := ir.EnvironmentDigest(environment)
	require.NoError(t, err)
	contractHash, err := ir.ContractHash(environment.Verbs["noop"])
	require.NoError(t, err)
	build := sha256.Sum256([]byte("compiler"))
	checked, err := ir.Check(&effectusv1.RuleArtifact{
		FormatVersion: ir.FormatVersion, EnvironmentDigest: environmentDigest,
		Compiler: &effectusv1.CompilerMetadata{Name: "effectusc", Version: "test", BuildDigest: hex.EncodeToString(build[:])},
		Plans: []*effectusv1.Plan{{
			Id: "plan", SourceDialect: effectusv1.SourceDialect_SOURCE_DIALECT_LIST,
			ExecutionPolicy: effectusv1.ExecutionPolicy_EXECUTION_POLICY_DURABLE_FAIL_FAST,
			Predicate:       &effectusv1.Predicate{Expression: &effectusv1.Expression{Kind: &effectusv1.Expression_Literal{Literal: &effectusv1.Literal{Kind: &effectusv1.Literal_BoolValue{BoolValue: true}}}}},
			Steps:           []*effectusv1.Step{{Id: "step", Verb: "noop", ContractHash: contractHash}},
		}},
	}, environment, ir.Limits{})
	require.NoError(t, err)
	store := NewInMemoryOutboxStore()
	_, err = store.CreateSaga(t.Context(), CreateSagaRequest{
		Namespace: "test", SagaID: "unstable", ExecutionID: "execution", PlanID: "plan", PlanDigest: checked.Digest(), Serial: true,
	})
	require.ErrorIs(t, err, ErrIdentityConflict)
}
