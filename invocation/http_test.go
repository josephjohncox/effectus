package invocation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHTTPExecutorPropagatesSystemInvocationHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "execution-1", request.Header.Get(HeaderExecutionID))
		require.Equal(t, "saga-1", request.Header.Get(HeaderSagaID))
		require.Equal(t, "effect-1", request.Header.Get(HeaderEffectID))
		require.Equal(t, "2", request.Header.Get(HeaderAttempt))
		require.Equal(t, "forward", request.Header.Get(HeaderDirection))
		require.Equal(t, "stable-key", request.Header.Get(HeaderIdempotencyKey))
		require.Equal(t, "args-hash", request.Header.Get(HeaderArgumentHash))
		require.JSONEq(t, `[{"authority":"db","resource":"account-1","token":9}]`, request.Header.Get(HeaderFencingGrants))
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	executor, err := NewHTTPExecutor(HTTPExecutor{URL: server.URL})
	require.NoError(t, err)
	outcome := executor.Invoke(context.Background(), Request{
		Metadata: Context{
			ExecutionID:   "execution-1",
			Saga:          Saga{SagaID: "saga-1", EffectID: "effect-1", Attempt: 2, Direction: DirectionForward, IdempotencyKey: "stable-key"},
			FencingGrants: []FencingGrant{{Authority: "db", Resource: "account-1", Token: 9}},
			Deadline:      time.Now().Add(time.Minute),
		},
		Verb: "charge", Arguments: map[string]any{"amount": 42}, ArgumentHash: "args-hash", ContractHash: "contract-hash",
	})
	require.NoError(t, outcome.Err)
	require.Equal(t, OutcomeSuccess, outcome.Class)
	require.Equal(t, true, outcome.Result.(map[string]any)["ok"])
}

func TestHTTPExecutorRejectsReservedStaticHeaders(t *testing.T) {
	_, err := NewHTTPExecutor(HTTPExecutor{
		URL:     "https://example.invalid",
		Headers: map[string]string{"idempotency-key": "caller-value"},
	})
	require.ErrorContains(t, err, "reserved")
	_, err = NewHTTPExecutor(HTTPExecutor{
		URL:     "https://example.invalid",
		Headers: map[string]string{"X-Effectus-Fencing-Grants": `[{"token":999}]`},
	})
	require.ErrorContains(t, err, "reserved")
}

func TestHTTPExecutorRequiresExplicitFailureClassification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = writer.Write([]byte("unclassified"))
	}))
	defer server.Close()
	executor, err := NewHTTPExecutor(HTTPExecutor{URL: server.URL})
	require.NoError(t, err)
	outcome := executor.Invoke(t.Context(), Request{})
	require.Equal(t, OutcomeUnknown, outcome.Class)
	require.Error(t, outcome.Err)
}
