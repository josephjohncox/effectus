package loader

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/effectus/effectus-go/invocation"
	"github.com/stretchr/testify/require"
)

func TestHTTPExecutorRejectsNonPositiveTimeout(t *testing.T) {
	_, err := NewHTTPExecutor(map[string]interface{}{"url": "http://example.invalid", "timeout": "0s"})
	require.ErrorContains(t, err, "positive duration")
	_, err = NewHTTPExecutor(map[string]interface{}{"url": "http://example.invalid", "timeout": "invalid"})
	require.ErrorContains(t, err, "positive duration")
}

func TestHTTPExecutorPropagatesInvocationMetadataToTransport(t *testing.T) {
	captured := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		captured <- request.Header.Clone()
		_ = json.NewEncoder(writer).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()
	executor, err := NewHTTPExecutor(map[string]interface{}{"url": server.URL, "timeout": time.Second.String(), "allowPrivateNetwork": true})
	require.NoError(t, err)
	request := invocation.Request{Metadata: invocation.Context{
		RequestID: "request", ExecutionID: "execution", Deadline: time.Now().Add(time.Minute),
		Saga:          invocation.Saga{SagaID: "saga", EffectID: "effect", Attempt: 2, Direction: invocation.DirectionForward, IdempotencyKey: "key"},
		FencingGrants: []invocation.FencingGrant{{Authority: "sink", Resource: "account", Token: 7}},
	}, Verb: "charge", Arguments: map[string]any{"amount": 1}, ArgumentHash: "argument", ContractHash: "contract"}
	outcome := executor.Invoke(t.Context(), request)
	require.Equal(t, invocation.OutcomeSuccess, outcome.Class)
	headers := <-captured
	require.Equal(t, "execution", headers.Get("X-Effectus-Execution-ID"))
	require.Equal(t, "effect", headers.Get("X-Effectus-Effect-ID"))
	require.Equal(t, "2", headers.Get("X-Effectus-Attempt"))
	require.Equal(t, "key", headers.Get("X-Effectus-Idempotency-Key"))
	require.Equal(t, "argument", headers.Get("X-Effectus-Argument-Hash"))
	require.Contains(t, headers.Get("X-Effectus-Fencing-Grants"), `"token":7`)
}

func TestHTTPStreamPropagatesInvocationAndFencingMetadata(t *testing.T) {
	captured := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		captured <- request.Header.Clone()
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	executor, err := NewStreamExecutor(map[string]any{"publisher": "http", "url": server.URL, "allowPrivateNetwork": true, "timeout": "1s"})
	require.NoError(t, err)
	outcome := executor.Invoke(t.Context(), invocation.Request{Metadata: invocation.Context{ExecutionID: "execution", Saga: invocation.Saga{EffectID: "effect", Attempt: 5, IdempotencyKey: "key"}, FencingGrants: []invocation.FencingGrant{{Authority: "sink", Resource: "account", Token: 12}}}, Arguments: map[string]any{"id": "42"}})
	require.Equal(t, invocation.OutcomeSuccess, outcome.Class)
	headers := <-captured
	require.Equal(t, "execution", headers.Get("X-Effectus-Execution-ID"))
	require.Equal(t, "5", headers.Get("X-Effectus-Attempt"))
	require.Contains(t, headers.Get("X-Effectus-Fencing-Grants"), `"token":12`)
}

func TestHTTPExecutorBoundsAndChecksResponseReads(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(strings.Repeat("x", (1<<20)+1)))
	}))
	defer server.Close()
	executor, err := NewHTTPExecutor(map[string]interface{}{"url": server.URL, "timeout": time.Second.String(), "allowPrivateNetwork": true})
	require.NoError(t, err)
	_, err = executor.Execute(t.Context(), map[string]interface{}{"value": true})
	require.ErrorContains(t, err, "exceeds")
}
