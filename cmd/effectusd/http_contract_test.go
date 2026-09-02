package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/josephjohncox/effectus/bundle"
	"github.com/josephjohncox/effectus/embedded"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
	"github.com/josephjohncox/effectus/runtime"
	"github.com/josephjohncox/effectus/schema"
	"github.com/stretchr/testify/require"
)

type failingHTTPContractExecutor struct{ calls *int }

func (executor failingHTTPContractExecutor) Invoke(context.Context, invocation.Request) invocation.Outcome {
	*executor.calls++
	return invocation.Outcome{Class: invocation.OutcomePermanentFailure, Err: io.ErrUnexpectedEOF}
}

func newHTTPContractDaemon(t *testing.T) (*daemon, *int, func()) {
	t.Helper()
	descriptor, err := invocation.NewDescriptor(invocation.DescriptorSpec{Type: invocation.DescriptorHTTP, ResolverID: "test/http-contract/v1"})
	require.NoError(t, err)
	source, err := bundle.New(bundle.Spec{
		Name: "orders", Version: "1",
		Sources: []bundle.Source{{Path: "rules/orders.eff", Content: `rule "Review" priority 1 { when { order.risk > 80 } then { RequestReview(orderId: order.id) } }`}},
		Environment: ir.Environment{
			Facts: map[string]string{"order.risk": "int", "order.id": "string"},
			Verbs: map[string]ir.VerbContract{"RequestReview": {Arguments: map[string]string{"orderId": "string"}, RequiredArgs: []string{"orderId"}, ResultType: "string"}},
		},
		Executors: map[string]invocation.Descriptor{"RequestReview": descriptor},
	})
	require.NoError(t, err)
	calls := 0
	resolvers, err := invocation.NewRegistry([]invocation.ResolverRegistration{{ID: "test/http-contract/v1", Resolver: invocation.ResolverFunc(func(context.Context, invocation.Descriptor) (invocation.Executor, io.Closer, error) {
		return failingHTTPContractExecutor{calls: &calls}, nil, nil
	})}})
	require.NoError(t, err)
	runner, err := embedded.Open(t.Context(), source, resolvers)
	require.NoError(t, err)
	return &daemon{engine: runner.Engine()}, &calls, func() { require.NoError(t, runner.Close()) }
}

func executeHTTPContractRequest(t *testing.T, handler http.Handler, token, key string, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/v1/execute", bytes.NewBufferString(body))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		request.Header.Set(invocation.HeaderIdempotencyKey, key)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

// contendedAdmissionLedger holds all callers at the admission boundary. It
// deterministically exercises the race where every request misses the initial
// idempotency lookup before one payload is persisted.
type contendedAdmissionLedger struct {
	schema.ExecutionLedger
	admissions sync.WaitGroup
}

func (ledger *contendedAdmissionLedger) AdmitExecution(ctx context.Context, admission schema.DurableAdmission) (schema.ExecutionRecord, bool, error) {
	ledger.admissions.Done()
	ledger.admissions.Wait()
	return ledger.ExecutionLedger.AdmitExecution(ctx, admission)
}

func TestHTTPAdmissionContract(t *testing.T) {
	d, calls, closeDaemon := newHTTPContractDaemon(t)
	defer closeDaemon()
	handler := d.httpHandler("secret")
	body := `{"namespace":"tenant-a","facts":{"order":{"id":"42","risk":99}}}`

	t.Run("requires authentication", func(t *testing.T) {
		response := executeHTTPContractRequest(t, handler, "", "key-1", body)
		require.Equal(t, http.StatusUnauthorized, response.Code)
	})
	t.Run("requires idempotency header", func(t *testing.T) {
		response := executeHTTPContractRequest(t, handler, "secret", "", body)
		require.Equal(t, http.StatusBadRequest, response.Code)
		require.Contains(t, response.Body.String(), invocation.HeaderIdempotencyKey)
	})
	t.Run("enforces body limit", func(t *testing.T) {
		response := executeHTTPContractRequest(t, handler, "secret", "too-large", `{"facts":"`+string(bytes.Repeat([]byte("x"), maxHTTPBodyBytes))+`"}`)
		require.Equal(t, http.StatusRequestEntityTooLarge, response.Code)
	})

	first := executeHTTPContractRequest(t, handler, "secret", "key-1", body)
	require.Equal(t, http.StatusAccepted, first.Code)
	var accepted runtime.ExecuteResult
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &accepted))
	require.True(t, accepted.DurablyAccepted)
	require.False(t, accepted.Completed)
	require.Zero(t, *calls, "WaitAccepted must not expose an executor failure before durable admission")

	t.Run("matching retry returns same identity", func(t *testing.T) {
		response := executeHTTPContractRequest(t, handler, "secret", "key-1", body)
		require.Equal(t, http.StatusAccepted, response.Code)
		var replay runtime.ExecuteResult
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &replay))
		require.Equal(t, accepted.ExecutionID, replay.ExecutionID)
	})
	t.Run("changed content conflicts", func(t *testing.T) {
		response := executeHTTPContractRequest(t, handler, "secret", "key-1", `{"namespace":"tenant-a","facts":{"order":{"id":"43","risk":99}}}`)
		require.Equal(t, http.StatusConflict, response.Code)
	})
	t.Run("concurrent changed payloads always conflict instead of returning bad request", func(t *testing.T) {
		raceDaemon, _, closeRaceDaemon := newHTTPContractDaemon(t)
		defer closeRaceDaemon()
		const workers = 64
		ledger := &contendedAdmissionLedger{ExecutionLedger: schema.NewInMemoryExecutionLedger()}
		ledger.admissions.Add(workers)
		require.NoError(t, raceDaemon.engine.ConfigureLedger(ledger, nil))
		raceHandler := raceDaemon.httpHandler("secret")

		type response struct {
			payload string
			status  int
		}
		responses := make(chan response, workers)
		var callers sync.WaitGroup
		callers.Add(workers)
		for worker := range workers {
			payload := "low"
			body := `{"namespace":"tenant-race","facts":{"order":{"id":"42","risk":1}}}`
			if worker%2 == 0 {
				payload = "high"
				body = `{"namespace":"tenant-race","facts":{"order":{"id":"42","risk":99}}}`
			}
			go func(payload, body string) {
				defer callers.Done()
				result := executeHTTPContractRequest(t, raceHandler, "secret", "race-key", body)
				responses <- response{payload: payload, status: result.Code}
			}(payload, body)
		}
		callers.Wait()
		close(responses)

		statuses := map[string][]int{"high": nil, "low": nil}
		for result := range responses {
			require.NotEqual(t, http.StatusBadRequest, result.status, "payload %s must not be downgraded to HTTP 400", result.payload)
			require.Contains(t, []int{http.StatusAccepted, http.StatusConflict}, result.status)
			statuses[result.payload] = append(statuses[result.payload], result.status)
		}
		acceptedPayload := ""
		for payload, values := range statuses {
			for _, status := range values {
				if status == http.StatusAccepted {
					if acceptedPayload == "" {
						acceptedPayload = payload
					}
					require.Equal(t, acceptedPayload, payload, "only one payload may win an idempotency race")
				}
			}
		}
		require.NotEmpty(t, acceptedPayload)
		conflictingPayload := "high"
		if acceptedPayload == conflictingPayload {
			conflictingPayload = "low"
		}
		for _, status := range statuses[conflictingPayload] {
			require.Equal(t, http.StatusConflict, status, "changed payload conflicts must be HTTP 409")
		}
	})
	t.Run("executor failures happen after admission and do not change replay identity", func(t *testing.T) {
		_, err := d.engine.Execute(t.Context(), runtime.ExecuteRequest{ResumeExecutionID: accepted.ExecutionID, WaitMode: runtime.WaitTerminal})
		require.Error(t, err)
		require.Positive(t, *calls)
		response := executeHTTPContractRequest(t, handler, "secret", "key-1", body)
		require.Equal(t, http.StatusAccepted, response.Code)
		var replay runtime.ExecuteResult
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &replay))
		require.Equal(t, accepted.ExecutionID, replay.ExecutionID)
	})
	t.Run("stale generation conflicts", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/v1/execute", bytes.NewBufferString(body))
		request.Header.Set("Authorization", "Bearer secret")
		request.Header.Set(invocation.HeaderIdempotencyKey, "key-stale")
		request.Header.Set("If-Match", `"stale-generation"`)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		require.Equal(t, http.StatusConflict, response.Code)
	})
}
