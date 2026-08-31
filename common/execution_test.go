package common

import (
	"context"
	"testing"

	effectus "github.com/josephjohncox/effectus"
	"github.com/josephjohncox/effectus/schema/verb"
	"github.com/stretchr/testify/require"
)

type contextOnlyExecutor struct{}

func (contextOnlyExecutor) DoContext(ctx context.Context, _ effectus.Effect) (interface{}, error) {
	return ctx.Value("key"), nil
}

var _ effectus.ContextExecutor = contextOnlyExecutor{}

func TestInvokeContextAcceptsContextOnlyExecutor(t *testing.T) {
	ctx := context.WithValue(context.Background(), "key", "value")
	result, err := effectus.InvokeContext(ctx, contextOnlyExecutor{}, effectus.Effect{})
	require.NoError(t, err)
	require.Equal(t, "value", result)
}

func TestExecutorAdapterValidatesTypedVerbsByDefault(t *testing.T) {
	registry := verb.NewRegistry(nil)
	require.NoError(t, registry.RegisterVerb(&verb.Spec{
		Name:       "typed",
		ArgTypes:   map[string]string{"id": "string"},
		ReturnType: "any",
		Executor: verb.NewFunctionExecutor(func(context.Context, map[string]interface{}) (interface{}, error) {
			return nil, nil
		}),
	}))

	_, err := effectus.Invoke(context.Background(), NewExecutorAdapter(registry, NewBasicFacts(nil, nil)), effectus.Effect{
		Verb:    "typed",
		Payload: map[string]interface{}{"id": 42},
	})
	require.ErrorContains(t, err, "argument id expected string")
}

func TestExecutorAdapterPropagatesCancellation(t *testing.T) {
	registry := verb.NewRegistry(nil)
	require.NoError(t, registry.RegisterVerb(&verb.Spec{
		Name:       "wait",
		ArgTypes:   map[string]string{},
		ReturnType: "any",
		Executor: verb.NewFunctionExecutor(func(ctx context.Context, _ map[string]interface{}) (interface{}, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}),
	}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := effectus.Invoke(ctx, NewExecutorAdapter(registry, NewBasicFacts(nil, nil)), effectus.Effect{
		Verb:    "wait",
		Payload: map[string]interface{}{},
	})
	require.ErrorIs(t, err, context.Canceled)
}
