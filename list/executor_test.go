package list

import (
	"context"
	"testing"

	"github.com/effectus/effectus-go/common"
	"github.com/effectus/effectus-go/schema"
	"github.com/effectus/effectus-go/schema/verb"
	"github.com/stretchr/testify/require"
)

func TestListSagaExecutesEveryEffectWithoutCapabilitySystem(t *testing.T) {
	registry := verb.NewRegistry(nil)
	var executed []string
	for _, name := range []string{"one", "two", "three"} {
		name := name
		spec := &verb.Spec{
			Name:       name,
			ArgTypes:   map[string]string{},
			ReturnType: "any",
			Executor: verb.NewFunctionExecutor(func(context.Context, map[string]interface{}) (interface{}, error) {
				executed = append(executed, name)
				return nil, nil
			}),
		}
		require.NoError(t, registry.RegisterVerb(spec))
	}

	executor := NewExecutor(registry, WithSaga(schema.NewInMemorySagaStore()))
	rule := &CompiledRule{
		Name: "ordered",
		Effects: []*Effect{
			{Verb: "one", Args: map[string]interface{}{}},
			{Verb: "two", Args: map[string]interface{}{}},
			{Verb: "three", Args: map[string]interface{}{}},
		},
	}

	effects, err := executor.ExecuteRule(context.Background(), rule, common.NewBasicFacts(nil, nil))
	require.NoError(t, err)
	require.Len(t, effects, 3)
	require.Equal(t, []string{"one", "two", "three"}, executed)
}

func TestListSagaRequiresStore(t *testing.T) {
	executor := NewExecutor(verb.NewRegistry(nil), WithSaga(nil))
	_, err := executor.ExecuteRule(
		context.Background(),
		&CompiledRule{Name: "missing-store"},
		common.NewBasicFacts(nil, nil),
	)
	require.EqualError(t, err, "saga execution requires a saga store")
}
