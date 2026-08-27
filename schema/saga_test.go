package schema

import (
	"errors"
	"testing"

	effectus "github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/schema/capability"
	"github.com/effectus/effectus-go/schema/verb"
	"github.com/stretchr/testify/require"
)

type recordingEffectExecutor struct {
	verbs    []string
	failures map[string]error
	results  map[string]interface{}
}

func (e *recordingEffectExecutor) Do(effect effectus.Effect) (interface{}, error) {
	e.verbs = append(e.verbs, effect.Verb)
	return e.results[effect.Verb], e.failures[effect.Verb]
}

func TestSagaExecutorPreservesSourceOrder(t *testing.T) {
	registry := verb.NewRegistry(nil)
	register := func(name, resource string) {
		t.Helper()
		require.NoError(t, registry.RegisterVerb(&verb.Spec{
			Name: name,
			Resources: verb.ResourceSet{
				{Resource: resource, Cap: verb.CapWrite},
			},
		}))
	}
	register("A", "X")
	register("B", "X")
	register("C", "Y")

	store := NewInMemorySagaStore()
	recorder := &recordingEffectExecutor{results: map[string]interface{}{
		"A": map[string]interface{}{"receipt": "a-1"},
	}}
	executor := NewSagaExecutor(
		recorder,
		store,
		capability.NewCapabilitySystem(),
		registry,
		"test-holder",
	)

	effectsToExecute := []effectus.Effect{
		{Verb: "A", Payload: map[string]interface{}{}},
		{Verb: "B", Payload: map[string]interface{}{}},
		{Verb: "C", Payload: map[string]interface{}{}},
	}
	firstResults, err := executor.ExecuteWithSaga(t.Context(), "saga-order", "ordered", effectsToExecute)
	require.NoError(t, err)
	secondResults, err := executor.ExecuteWithSaga(t.Context(), "saga-order", "ordered", effectsToExecute)
	require.NoError(t, err)
	require.Equal(t, firstResults, secondResults)
	require.Equal(t, []string{"A", "B", "C"}, recorder.verbs)

	effects, err := store.GetTransactionEffects("saga-order")
	require.NoError(t, err)
	require.Len(t, effects, 3)
	require.Equal(t, []string{"step-000001", "step-000002", "step-000003"}, []string{effects[0].ID, effects[1].ID, effects[2].ID})
	require.Equal(t, []int{1, 2, 3}, []int{effects[0].Sequence, effects[1].Sequence, effects[2].Sequence})
	require.Equal(t, map[string]interface{}{"receipt": "a-1"}, effects[0].Result)
	for _, effect := range effects {
		require.Equal(t, SagaEffectSuccess, effect.Status)
	}
	active, err := store.GetActiveSagas()
	require.NoError(t, err)
	require.Empty(t, active)
}

func TestInMemorySagaStoreRejectsEffectIdentityConflicts(t *testing.T) {
	store := NewInMemorySagaStore()
	require.NoError(t, store.StartTransaction("saga", "rule"))
	require.NoError(t, store.RecordEffect("saga", "step-000001", 1, "apply", map[string]interface{}{"id": "one"}))
	require.NoError(t, store.RecordEffect("saga", "step-000001", 1, "apply", map[string]interface{}{"id": "one"}))
	require.ErrorContains(t,
		store.RecordEffect("saga", "step-000001", 1, "apply", map[string]interface{}{"id": "different"}),
		"effect identity conflict",
	)
}

func TestSagaExecutorReturnsCompensationFailures(t *testing.T) {
	registry := verb.NewRegistry(nil)
	require.NoError(t, registry.RegisterVerb(&verb.Spec{Name: "apply", Inverse: "undo"}))
	require.NoError(t, registry.RegisterVerb(&verb.Spec{Name: "undo"}))
	require.NoError(t, registry.RegisterVerb(&verb.Spec{Name: "fail"}))

	recorder := &recordingEffectExecutor{failures: map[string]error{
		"fail": errors.New("forward failed"),
		"undo": errors.New("undo failed"),
	}}
	executor := NewSagaExecutor(
		recorder,
		NewInMemorySagaStore(),
		nil,
		registry,
		"test-holder",
	)

	_, err := executor.ExecuteWithSaga(t.Context(), "saga-failure", "failure", []effectus.Effect{
		{Verb: "apply", Payload: map[string]interface{}{}},
		{Verb: "fail", Payload: map[string]interface{}{}},
	})
	require.ErrorContains(t, err, "forward failed")
	require.ErrorContains(t, err, "undo failed")
	require.Equal(t, []string{"apply", "fail", "undo"}, recorder.verbs)
}
