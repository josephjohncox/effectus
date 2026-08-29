package executorhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/effectus/effectus-go/invocation"
	"github.com/stretchr/testify/require"
)

func TestHandlerDecodesInvocationAndWritesSuccess(t *testing.T) {
	handler, err := NewHandler(Options{}, func(_ context.Context, request Request) invocation.Outcome {
		require.Equal(t, "execution-1", request.Metadata.ExecutionID)
		require.Equal(t, "effect-1", request.Metadata.Saga.EffectID)
		require.Equal(t, "key-1", request.Metadata.Saga.IdempotencyKey)
		require.Equal(t, uint64(2), request.Metadata.Saga.Attempt)
		require.Equal(t, "order-1", request.Arguments["orderId"])
		return Success(map[string]any{"reviewId": "review-1"})
	})
	require.NoError(t, err)

	request := validRequest(`{"orderId":"order-1"}`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.JSONEq(t, `{"reviewId":"review-1"}`, response.Body.String())
}

func TestHandlerWritesExplicitRetryableOutcome(t *testing.T) {
	handler, err := NewHandler(Options{}, func(_ context.Context, _ Request) invocation.Outcome {
		return Retryable(errors.New("database unavailable before commit"))
	})
	require.NoError(t, err)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, validRequest(`{"orderId":"order-1"}`))

	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Equal(t, string(invocation.OutcomeRetryableKnownNotCommitted), response.Header().Get(invocation.HeaderOutcome))
}

func TestHandlerRejectsUnencodableSuccessResult(t *testing.T) {
	handler, err := NewHandler(Options{}, func(_ context.Context, _ Request) invocation.Outcome {
		return Success(make(chan int))
	})
	require.NoError(t, err)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, validRequest(`{"orderId":"order-1"}`))

	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.Equal(t, string(invocation.OutcomeUnknown), response.Header().Get(invocation.HeaderOutcome))
}

func TestHandlerRejectsMissingIdempotencyKey(t *testing.T) {
	handler, err := NewHandler(Options{}, func(_ context.Context, _ Request) invocation.Outcome {
		t.Fatal("invalid request must not reach the business handler")
		return Success(nil)
	})
	require.NoError(t, err)
	request := validRequest(`{"orderId":"order-1"}`)
	request.Header.Del(invocation.HeaderIdempotencyKey)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), invocation.HeaderIdempotencyKey)
}

func TestHandlerRejectsOversizedBody(t *testing.T) {
	handler, err := NewHandler(Options{MaxRequestBytes: 8}, func(_ context.Context, _ Request) invocation.Outcome {
		t.Fatal("oversized request must not reach the business handler")
		return Success(nil)
	})
	require.NoError(t, err)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, validRequest(`{"orderId":"order-1"}`))

	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Contains(t, response.Body.String(), "exceeds 8 bytes")
}

func validRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/execute", strings.NewReader(body))
	request.Header.Set(invocation.HeaderExecutionID, "execution-1")
	request.Header.Set(invocation.HeaderSagaID, "saga-1")
	request.Header.Set(invocation.HeaderEffectID, "effect-1")
	request.Header.Set(invocation.HeaderAttempt, "2")
	request.Header.Set(invocation.HeaderDirection, string(invocation.DirectionForward))
	request.Header.Set(invocation.HeaderIdempotencyKey, "key-1")
	request.Header.Set(invocation.HeaderArgumentHash, "arguments")
	request.Header.Set(invocation.HeaderContractHash, "contract")
	return request
}
