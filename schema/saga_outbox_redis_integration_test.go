//go:build integration

package schema

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/effectus/effectus-go/invocation"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRedisOutboxCrashRecoveryAndLeaseExpiry(t *testing.T) {
	options := redisLiveOptions(t, 0)
	first := newLiveRedisOutbox(t, options)
	createOutboxSaga(t, first, "crash-recovery")
	dispatch := enqueueOutboxStep(t, first, "crash-recovery", "effect-1", "charge", 1)
	claimed, err := first.ClaimDispatch(t.Context(), ClaimOptions{Owner: "crashed-worker", LeaseDuration: 150 * time.Millisecond})
	require.NoError(t, err)
	require.Equal(t, dispatch.ID, claimed.ID)
	require.NoError(t, first.Close())

	second := newLiveRedisOutbox(t, options)
	t.Cleanup(func() { _ = second.Close() })
	recovered, err := second.GetDispatch(t.Context(), dispatch.ID)
	require.NoError(t, err)
	require.Equal(t, DispatchInFlight, recovered.State)
	require.Equal(t, claimed.LeaseToken, recovered.LeaseToken)

	var reclaimed *Dispatch
	require.Eventually(t, func() bool {
		reclaimed, err = second.ClaimDispatch(context.Background(), ClaimOptions{Owner: "recovery-worker", LeaseDuration: time.Second})
		return err == nil
	}, 3*time.Second, 25*time.Millisecond)
	require.Equal(t, uint64(2), reclaimed.Attempt)
	require.Equal(t, claimed.IdempotencyKey, reclaimed.IdempotencyKey)
	require.NotEqual(t, claimed.LeaseToken, reclaimed.LeaseToken)
	require.ErrorIs(t, second.CompleteDispatch(t.Context(), Completion{
		DispatchID: claimed.ID, Attempt: claimed.Attempt, LeaseToken: claimed.LeaseToken,
		Outcome: invocation.OutcomeSuccess, Result: []byte(`{"stale":true}`),
	}), ErrStaleLease)
	require.NoError(t, second.CompleteDispatch(t.Context(), Completion{
		DispatchID: reclaimed.ID, Attempt: reclaimed.Attempt, LeaseToken: reclaimed.LeaseToken,
		Outcome: invocation.OutcomeSuccess, Result: []byte(`{"recovered":true}`),
	}))
}

func TestRedisTargetedDispatchSelectsOwningSaga(t *testing.T) {
	store := newLiveRedisOutbox(t, redisLiveOptions(t, 0))
	t.Cleanup(func() { _ = store.Close() })
	createOutboxSaga(t, store, "earlier-saga")
	_ = enqueueOutboxStep(t, store, "earlier-saga", "effect-1", "charge", 1)
	time.Sleep(time.Millisecond)
	createOutboxSaga(t, store, "target-saga")
	target := enqueueOutboxStep(t, store, "target-saga", "effect-1", "charge", 1)
	claimed, err := store.ClaimDispatch(t.Context(), ClaimOptions{Owner: "target-worker", LeaseDuration: time.Second, TargetDispatchID: target.ID})
	require.NoError(t, err)
	require.Equal(t, target.ID, claimed.ID)
	require.Equal(t, "target-saga", claimed.SagaID)
}

func TestRedisOutboxDuplicateDeliveryIsIdempotent(t *testing.T) {
	store := newLiveRedisOutbox(t, redisLiveOptions(t, 0))
	t.Cleanup(func() { _ = store.Close() })
	createOutboxSaga(t, store, "duplicate")

	const workers = 12
	ids := make(chan string, workers)
	errorsChannel := make(chan error, workers)
	var wait sync.WaitGroup
	wait.Add(workers)
	for range workers {
		go func() {
			defer wait.Done()
			dispatch, err := store.EnqueueStep(context.Background(), EnqueueStepRequest{
				SagaID: "duplicate", EffectID: "effect-1", Sequence: 1, Verb: "charge",
				ContractHash: "contract", Arguments: map[string]any{"delivery": "same"},
			})
			if err != nil {
				errorsChannel <- err
				return
			}
			ids <- dispatch.ID
		}()
	}
	wait.Wait()
	close(ids)
	close(errorsChannel)
	for err := range errorsChannel {
		require.NoError(t, err)
	}
	var expected string
	for id := range ids {
		if expected == "" {
			expected = id
		}
		require.Equal(t, expected, id)
	}
	dispatches, err := store.ListDispatches(t.Context(), "duplicate")
	require.NoError(t, err)
	require.Len(t, dispatches, 1)
}

func TestRedisOutboxConcurrentSagasDoNotRewriteOrContendWithEachOther(t *testing.T) {
	store := newLiveRedisOutbox(t, redisLiveOptions(t, 0))
	t.Cleanup(func() { _ = store.Close() })

	const workers = 24
	results := make(chan error, workers)
	var wait sync.WaitGroup
	wait.Add(workers)
	for index := 0; index < workers; index++ {
		index := index
		go func() {
			defer wait.Done()
			_, err := store.CreateSaga(context.Background(), CreateSagaRequest{
				Namespace: "test", SagaID: uuid.NewSHA1(uuid.Nil, []byte{byte(index)}).String(),
				ExecutionID: "execution", PlanID: "plan", PlanDigest: "digest", Serial: true, allowUnstableIdentityForTest: true,
			})
			results <- err
		}()
	}
	wait.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	keys, err := store.client.Keys(t.Context(), store.base+"saga:*").Result()
	require.NoError(t, err)
	require.Len(t, keys, workers)
	require.Zero(t, store.OptimisticConflictRetries(), "unrelated saga keys must not contend")
}

func TestRedisOutboxLegacyMigrationIsExplicitAndResumable(t *testing.T) {
	options := redisLiveOptions(t, 0)
	client := redis.NewClient(&redis.Options{Addr: options.Addr, Password: options.Password, DB: options.DB})
	t.Cleanup(func() { _ = client.Close() })
	legacy := newRedisOutboxState()
	legacy.Sagas["legacy"] = &SagaInstance{Namespace: "test", SagaID: "legacy", ExecutionID: "execution", PlanID: "plan", PlanDigest: "digest", State: SagaRunning, Serial: true, Revision: 1}
	legacy.Steps["legacy"] = map[string]*SagaStep{}
	payload, err := json.Marshal(legacy)
	require.NoError(t, err)
	require.NoError(t, client.Set(t.Context(), options.Prefix+"saga-outbox:v2", payload, 0).Err())
	_, err = NewRedisOutboxStore(options)
	require.ErrorContains(t, err, "run MigrateRedisOutboxV2")
	require.NoError(t, MigrateRedisOutboxV2(t.Context(), options))
	require.NoError(t, MigrateRedisOutboxV2(t.Context(), options), "migration restart must be idempotent")
	store := newLiveRedisOutbox(t, options)
	t.Cleanup(func() { _ = store.Close() })
	saga, err := store.GetSaga(t.Context(), "legacy")
	require.NoError(t, err)
	require.Equal(t, SagaRunning, saga.State)
	exists, err := client.Exists(t.Context(), options.Prefix+"saga-outbox:v2").Result()
	require.NoError(t, err)
	require.Equal(t, int64(1), exists, "legacy backup must be retained")
}

func TestRedisOutboxTTLStartsOnlyAfterRecoveryStateIsTerminal(t *testing.T) {
	options := redisLiveOptions(t, 250*time.Millisecond)
	store := newLiveRedisOutbox(t, options)
	createOutboxSaga(t, store, "ttl")
	require.Never(t, func() bool {
		_, err := store.GetSaga(context.Background(), "ttl")
		return err != nil
	}, 750*time.Millisecond, 25*time.Millisecond)
	require.NoError(t, store.CompleteSaga(t.Context(), "ttl"))
	require.Eventually(t, func() bool {
		_, err := store.GetSaga(context.Background(), "ttl")
		return err != nil
	}, 3*time.Second, 25*time.Millisecond)
	require.NoError(t, store.Close())
}

func TestRedisOutboxIdentityConflictDoesNotOverwrite(t *testing.T) {
	store := newLiveRedisOutbox(t, redisLiveOptions(t, 0))
	t.Cleanup(func() { _ = store.Close() })
	createOutboxSaga(t, store, "identity")
	first := enqueueOutboxStep(t, store, "identity", "effect-1", "charge", 1)
	_, err := store.EnqueueStep(t.Context(), EnqueueStepRequest{
		SagaID: "identity", EffectID: "effect-1", Sequence: 1, Verb: "charge",
		ContractHash: "contract-charge", Arguments: map[string]any{"id": "different"},
	})
	require.ErrorIs(t, err, ErrIdentityConflict)
	stored, err := store.GetDispatch(t.Context(), first.ID)
	require.NoError(t, err)
	require.Equal(t, first.ArgumentHash, stored.ArgumentHash)
}

func redisLiveOptions(t *testing.T, ttl time.Duration) RedisOutboxStoreOptions {
	t.Helper()
	address := os.Getenv("REDIS_ADDR")
	if address == "" {
		t.Skip("REDIS_ADDR is required for live Redis integration tests")
	}
	return RedisOutboxStoreOptions{
		Addr: address, Prefix: "effectus-test:" + uuid.NewString() + ":", TTL: ttl, MaxRetries: 256,
	}
}

func newLiveRedisOutbox(t *testing.T, options RedisOutboxStoreOptions) *RedisOutboxStore {
	t.Helper()
	store, err := NewRedisOutboxStore(options)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			t.Skipf("Redis is unavailable: %v", err)
		}
		require.NoError(t, err)
	}
	return store
}
