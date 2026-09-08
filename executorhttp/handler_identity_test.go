package executorhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/josephjohncox/effectus/invocation"
	"github.com/stretchr/testify/require"
)

func TestVerifiedArgumentsPreserveBusinessIdempotency(t *testing.T) {
	calls, commits := 0, 0
	seen := map[string]string{}
	handler, err := NewHandler(Options{}, func(_ context.Context, request invocation.Request) invocation.Outcome {
		calls++
		key := request.Metadata.Saga.IdempotencyKey
		if prior, ok := seen[key]; ok {
			if prior != request.ArgumentHash {
				return Permanent(errors.New("idempotency conflict"))
			}
			return Success(true)
		}
		seen[key] = request.ArgumentHash
		commits++
		return Success(true)
	})
	require.NoError(t, err)
	for _, upper := range []bool{false, true} {
		request := validRequest(`{"orderId":"one"}`)
		if upper {
			request.Header.Set(invocation.HeaderArgumentHash, strings.ToUpper(request.Header.Get(invocation.HeaderArgumentHash)))
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code)
	}
	require.Equal(t, 2, calls)
	require.Equal(t, 1, commits)
	forged := validRequest(`{"orderId":"two"}`)
	forged.Header.Set(invocation.HeaderArgumentHash, testArgumentHash(`{"orderId":"one"}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, forged)
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.Equal(t, 2, calls)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, validRequest(`{"orderId":"two"}`))
	require.Equal(t, http.StatusUnprocessableEntity, response.Code)
	require.Equal(t, 3, calls)
	require.Equal(t, 1, commits)
}
