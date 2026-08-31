package embedded

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/josephjohncox/effectus/invocation"
	"github.com/stretchr/testify/require"
)

const orderRules = `
rule "ReviewLargeOrder" priority 10 {
  when {
    order.total > 1000
  }
  then {
    RequestReview(orderId: order.id, reason: "high_value")
  }
}
`

func TestRuntimeExecutesCheckedRulesAndDeduplicatesAdmission(t *testing.T) {
	var calls atomic.Int32
	application, err := New("orders", "1.0.0").
		AddFact("order.id", "").
		AddFact("order.total", 0.0).
		AddSource("orders.eff", []byte(orderRules)).
		AddVerb(Verb{
			Name:         "RequestReview",
			Description:  "Create a manual order review",
			ArgTypes:     map[string]string{"orderId": "string", "reason": "string"},
			RequiredArgs: []string{"orderId", "reason"},
			ReturnType:   "bool",
			Capabilities: []string{"write", "create", "idempotent"},
			Resources: []Resource{{
				Name: "order_review", Capabilities: []string{"write", "create", "idempotent"},
			}},
			Handler: func(_ context.Context, request invocation.Request) invocation.Outcome {
				calls.Add(1)
				require.Equal(t, "order-100", request.Arguments["orderId"])
				return Success(true)
			},
		}).
		Build(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, application.Close()) })

	request := Request{
		Namespace:      "tenant-a",
		IdempotencyKey: "order-100-created",
		Facts: map[string]any{
			"order": map[string]any{"id": "order-100", "total": 2500.0},
		},
	}
	first, err := application.Execute(t.Context(), request)
	require.NoError(t, err)
	require.True(t, first.Completed)

	second, err := application.Execute(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, first.ExecutionID, second.ExecutionID)
	require.Equal(t, int32(1), calls.Load())
}

func TestRuntimeRejectsConflictingIdempotentAdmission(t *testing.T) {
	application, err := newTestRuntime(t)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, application.Close()) })

	request := Request{
		Namespace:      "tenant-a",
		IdempotencyKey: "order-created",
		Facts: map[string]any{
			"order": map[string]any{"id": "order-100", "total": 2500.0},
		},
	}
	_, err = application.Execute(t.Context(), request)
	require.NoError(t, err)
	request.Facts["order"] = map[string]any{"id": "order-101", "total": 2500.0}
	_, err = application.Execute(t.Context(), request)
	require.Error(t, err)
}

func TestRuntimeRequiresIdempotencyKey(t *testing.T) {
	application, err := newTestRuntime(t)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, application.Close()) })

	_, err = application.Execute(t.Context(), Request{
		Namespace: "tenant-a",
		Facts:     map[string]any{"order": map[string]any{"id": "order-100", "total": 100.0}},
	})
	require.EqualError(t, err, "embedded idempotency key is required")
}

func newTestRuntime(t *testing.T) (*Runtime, error) {
	t.Helper()
	return New("orders", "1.0.0").
		AddFact("order.id", "").
		AddFact("order.total", 0.0).
		AddSource("orders.eff", []byte(orderRules)).
		AddVerb(Verb{
			Name:         "RequestReview",
			ArgTypes:     map[string]string{"orderId": "string", "reason": "string"},
			RequiredArgs: []string{"orderId", "reason"},
			ReturnType:   "bool",
			Capabilities: []string{"write", "create", "idempotent"},
			Resources: []Resource{{
				Name: "order_review", Capabilities: []string{"write", "create", "idempotent"},
			}},
			Handler: func(context.Context, invocation.Request) invocation.Outcome {
				return Success(true)
			},
		}).
		Build(t.Context())
}
