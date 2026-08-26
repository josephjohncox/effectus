package flow

import (
	"context"
	"errors"
	"testing"

	effectus "github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/common"
	"github.com/effectus/effectus-go/schema"
	"github.com/effectus/effectus-go/schema/verb"
	"github.com/stretchr/testify/require"
)

type recordingSagaStore struct {
	*schema.InMemorySagaStore
	startedIDs []string
}

func newRecordingSagaStore() *recordingSagaStore {
	return &recordingSagaStore{InMemorySagaStore: schema.NewInMemorySagaStore()}
}

func (s *recordingSagaStore) StartTransaction(sagaID, ruleName string) error {
	s.startedIDs = append(s.startedIDs, sagaID)
	return s.InMemorySagaStore.StartTransaction(sagaID, ruleName)
}

func registerTestVerb(
	t *testing.T,
	registry *verb.Registry,
	name string,
	inverse string,
	execute func(map[string]interface{}) (interface{}, error),
) {
	t.Helper()
	spec := &verb.Spec{
		Name:       name,
		ArgTypes:   map[string]string{},
		ReturnType: "any",
		Inverse:    inverse,
		Executor: verb.NewFunctionExecutor(func(_ context.Context, args map[string]interface{}) (interface{}, error) {
			return execute(args)
		}),
	}
	require.NoError(t, registry.RegisterVerb(spec))
}

func TestSagaExecutesWithoutCapabilitySystem(t *testing.T) {
	registry := verb.NewRegistry(nil)
	var executed []string
	for _, name := range []string{"one", "two", "three"} {
		name := name
		registerTestVerb(t, registry, name, "", func(map[string]interface{}) (interface{}, error) {
			executed = append(executed, name)
			return nil, nil
		})
	}

	store := newRecordingSagaStore()
	executor := NewExecutor(registry, WithSaga(store))
	program := FromList([]effectus.Effect{
		{Verb: "one", Payload: map[string]interface{}{}},
		{Verb: "two", Payload: map[string]interface{}{}},
		{Verb: "three", Payload: map[string]interface{}{}},
	})
	ctx := context.WithValue(context.Background(), "request_id", "req-123")

	_, err := executor.ExecuteProgram(ctx, "order", program, common.NewBasicFacts(nil, nil))
	require.NoError(t, err)
	require.Equal(t, []string{"one", "two", "three"}, executed)
	require.Equal(t, []string{"saga-flow-order-req-123"}, store.startedIDs)
}

func TestSagaReplayUsesPersistedSuccessfulResult(t *testing.T) {
	registry := verb.NewRegistry(nil)
	executions := 0
	registerTestVerb(t, registry, "charge", "refund", func(map[string]interface{}) (interface{}, error) {
		executions++
		return map[string]interface{}{"receipt": "receipt-1"}, nil
	})
	registerTestVerb(t, registry, "refund", "", func(map[string]interface{}) (interface{}, error) {
		return nil, nil
	})

	store := newRecordingSagaStore()
	executor := NewExecutor(registry, WithSaga(store))
	program := FromList([]effectus.Effect{{Verb: "charge", Payload: map[string]interface{}{"amount": 42}}})
	ctx := context.WithValue(context.Background(), "request_id", "replay-1")

	first, err := executor.ExecuteProgram(ctx, "payment", program, common.NewBasicFacts(nil, nil))
	require.NoError(t, err)
	second, err := executor.ExecuteProgram(ctx, "payment", program, common.NewBasicFacts(nil, nil))
	require.NoError(t, err)
	require.Equal(t, 1, executions)
	require.Equal(t, first, second)

	effects, err := store.GetTransactionEffects("saga-flow-payment-replay-1")
	require.NoError(t, err)
	require.Len(t, effects, 1)
	require.Equal(t, map[string]interface{}{"receipt": "receipt-1"}, effects[0].Result)
	require.Equal(t, schema.SagaEffectSuccess, effects[0].Status)
}

func TestSagaRequiresStore(t *testing.T) {
	executor := NewExecutor(verb.NewRegistry(nil), WithSaga(nil))
	_, err := executor.ExecuteProgram(
		context.Background(),
		"missing-store",
		Pure(nil),
		common.NewBasicFacts(nil, nil),
	)
	require.EqualError(t, err, "saga execution requires a saga store")
}

func TestSagaCompensatesRepeatedEffectsInReverseOrder(t *testing.T) {
	registry := verb.NewRegistry(nil)
	var trace []string
	registerTestVerb(t, registry, "apply", "undo", func(args map[string]interface{}) (interface{}, error) {
		trace = append(trace, "apply-"+args["id"].(string))
		return nil, nil
	})
	registerTestVerb(t, registry, "undo", "", func(args map[string]interface{}) (interface{}, error) {
		trace = append(trace, "undo-"+args["id"].(string))
		return nil, nil
	})
	registerTestVerb(t, registry, "fail", "", func(map[string]interface{}) (interface{}, error) {
		trace = append(trace, "fail")
		return nil, errors.New("forward failed")
	})

	store := newRecordingSagaStore()
	executor := NewExecutor(registry, WithSaga(store))
	program := FromList([]effectus.Effect{
		{Verb: "apply", Payload: map[string]interface{}{"id": "one"}},
		{Verb: "apply", Payload: map[string]interface{}{"id": "two"}},
		{Verb: "fail", Payload: map[string]interface{}{}},
	})

	_, err := executor.ExecuteProgram(context.Background(), "reverse", program, common.NewBasicFacts(nil, nil))
	require.ErrorContains(t, err, "forward failed")
	require.Equal(t, []string{"apply-one", "apply-two", "fail", "undo-two", "undo-one"}, trace)

	require.Len(t, store.startedIDs, 1)
	effects, storeErr := store.GetTransactionEffects(store.startedIDs[0])
	require.NoError(t, storeErr)
	require.Len(t, effects, 3)
	require.Equal(t, "step-000001", effects[0].ID)
	require.Equal(t, "step-000002", effects[1].ID)
	require.Equal(t, "step-000003", effects[2].ID)
	require.Equal(t, schema.SagaEffectCompensated, effects[0].Status)
	require.Equal(t, schema.SagaEffectCompensated, effects[1].Status)
	require.Equal(t, schema.SagaEffectFailed, effects[2].Status)
}

func TestSagaReturnsCompensationFailures(t *testing.T) {
	registry := verb.NewRegistry(nil)
	registerTestVerb(t, registry, "apply", "undo", func(map[string]interface{}) (interface{}, error) {
		return nil, nil
	})
	registerTestVerb(t, registry, "undo", "", func(map[string]interface{}) (interface{}, error) {
		return nil, errors.New("undo failed")
	})
	registerTestVerb(t, registry, "fail", "", func(map[string]interface{}) (interface{}, error) {
		return nil, errors.New("forward failed")
	})

	executor := NewExecutor(registry, WithSaga(schema.NewInMemorySagaStore()))
	program := FromList([]effectus.Effect{
		{Verb: "apply", Payload: map[string]interface{}{}},
		{Verb: "fail", Payload: map[string]interface{}{}},
	})

	_, err := executor.ExecuteProgram(context.Background(), "compensation-error", program, common.NewBasicFacts(nil, nil))
	require.ErrorContains(t, err, "forward failed")
	require.ErrorContains(t, err, "undo failed")
}
