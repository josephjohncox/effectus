package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/schema"
	"github.com/josephjohncox/effectus/schema/fencing"
	"github.com/stretchr/testify/require"
)

type remediationDispatchExecutor struct {
	calls atomic.Int64
	last  invocation.Request
}

func (executor *remediationDispatchExecutor) Invoke(ctx context.Context, request invocation.Request) invocation.Outcome {
	if err := ctx.Err(); err != nil {
		return invocation.Outcome{Class: invocation.OutcomeRetryableKnownNotCommitted, Err: err}
	}
	executor.calls.Add(1)
	executor.last = request
	return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: true}
}

func newRemediationDispatchFixture(tb testing.TB, payloadBytes int, fenced bool) (*schema.Dispatcher, *schema.InMemoryOutboxStore, *remediationDispatchExecutor, string) {
	tb.Helper()
	store := schema.NewInMemoryOutboxStore()
	sagaID := schema.StableSagaID("dispatch-benchmark", "plan")
	_, err := store.CreateSaga(context.Background(), schema.CreateSagaRequest{
		Namespace: "benchmark", ExecutionID: "dispatch-benchmark", PlanID: "plan", SagaID: sagaID, PlanDigest: strings.Repeat("a", 64), Serial: true,
	})
	if err != nil {
		tb.Fatal(err)
	}
	var provider fencing.Provider
	var requirements []schema.FencingRequirement
	if fenced {
		provider = fencing.NewInMemoryProvider()
		requirements = []schema.FencingRequirement{{Authority: "benchmark", Resource: "order-1"}}
	}
	dispatch, err := store.EnqueueStep(context.Background(), schema.EnqueueStepRequest{
		SagaID: sagaID, EffectID: "review", Sequence: 1, Verb: "Review", ContractHash: "benchmark-review-contract",
		Arguments: map[string]any{"orderId": "order-1", "payload": strings.Repeat("x", payloadBytes)}, Fencing: requirements,
	})
	if err != nil {
		tb.Fatal(err)
	}
	executor := &remediationDispatchExecutor{}
	dispatcher, err := schema.NewDispatcher(store, provider, executor, schema.DispatcherOptions{Owner: "benchmark", LeaseDuration: time.Minute})
	if err != nil {
		tb.Fatal(err)
	}
	return dispatcher, store, executor, dispatch.ID
}

func checkRemediationDispatch(tb testing.TB, store *schema.InMemoryOutboxStore, executor *remediationDispatchExecutor, dispatch *schema.Dispatch, id string, fenced bool) {
	tb.Helper()
	if dispatch == nil || dispatch.ID != id || dispatch.State != schema.DispatchSucceeded || executor.calls.Load() != 1 {
		tb.Fatal("dispatch must complete one queued step and invoke exactly once")
	}
	wantGrants := 0
	if fenced {
		wantGrants = 1
	}
	if len(executor.last.Metadata.FencingGrants) != wantGrants || executor.last.Metadata.Saga.Attempt != 1 || executor.last.Metadata.Saga.IdempotencyKey == "" {
		tb.Fatal("dispatch did not carry the expected authority and attempt metadata")
	}
	attempts, err := store.ListAttempts(context.Background(), id)
	if err != nil || len(attempts) != 1 || attempts[0].Outcome != invocation.OutcomeSuccess {
		tb.Fatalf("dispatch did not record its successful attempt: %v", err)
	}
}

func BenchmarkRemediationDispatch(b *testing.B) {
	for _, payloadBytes := range []int{256, 4096} {
		for _, fenced := range []bool{false, true} {
			b.Run(fmt.Sprintf("bytes-%d/fenced-%t", payloadBytes, fenced), func(b *testing.B) {
				b.StopTimer()
				b.ReportAllocs()
				b.ResetTimer()
				b.ReportMetric(1, "dispatches/op")
				b.ReportMetric(float64(payloadBytes), "payload-bytes/op")
				for index := 0; index < b.N; index++ {
					dispatcher, store, executor, id := newRemediationDispatchFixture(b, payloadBytes, fenced)
					b.StartTimer()
					dispatch, err := dispatcher.DispatchOne(b.Context())
					b.StopTimer()
					if err != nil {
						b.Fatal(err)
					}
					checkRemediationDispatch(b, store, executor, dispatch, id, fenced)
				}
			})
		}
	}
}

func TestRemediationBenchmarkFixtures(t *testing.T) {
	for _, config := range remediationBenchmarkCases {
		t.Run(config.name, func(t *testing.T) {
			fixture := newRemediationBenchmarkFixture(t, config)
			engine, durable, executor := newRemediationBenchmarkEngine(t, fixture)
			defer closeRemediationBenchmarkEngine(t, engine)
			for _, risk := range []int{0, 1, config.plans} {
				fixture.facts["order.risk"] = int64(risk)
				evaluations, err := engine.DryRun(t.Context(), fixture.facts)
				require.NoError(t, err)
				require.Len(t, evaluations, config.plans)
				matched := 0
				for _, evaluation := range evaluations {
					if evaluation.Matched {
						matched++
					}
				}
				require.Equal(t, risk, matched)
			}
			request := remediationBenchmarkRequest(fixture, "fixture-check", WaitAccepted)
			accepted, err := engine.Execute(t.Context(), request)
			require.NoError(t, err)
			require.True(t, accepted.DurablyAccepted)
			require.False(t, accepted.Completed)
			require.Zero(t, executor.calls.Load())
			record, err := durable.GetExecution(t.Context(), accepted.ExecutionID)
			require.NoError(t, err)
			require.Len(t, record.Plans, config.plans)
			worker := &RecoveryWorker{Engine: engine, Store: durable, Owner: "fixture-check", BatchSize: 1, LeaseDuration: time.Minute}
			processed, err := worker.RunOnce(t.Context())
			require.NoError(t, err)
			require.Equal(t, 1, processed)
			require.Equal(t, int64(config.plans), executor.calls.Load())
			request.WaitMode = WaitTerminal
			done, err := engine.Execute(t.Context(), request)
			require.NoError(t, err)
			require.True(t, done.Completed)
			require.Equal(t, int64(config.plans), executor.calls.Load())
		})
	}
	for _, payload := range []int{256, 4096} {
		for _, fenced := range []bool{false, true} {
			t.Run(fmt.Sprintf("dispatch-%d-%t", payload, fenced), func(t *testing.T) {
				dispatcher, store, executor, id := newRemediationDispatchFixture(t, payload, fenced)
				dispatch, err := dispatcher.DispatchOne(t.Context())
				require.NoError(t, err)
				checkRemediationDispatch(t, store, executor, dispatch, id, fenced)
			})
		}
	}
}
