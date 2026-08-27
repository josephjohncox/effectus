//go:build integration

package schema

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRedisSagaStoreCrashRecoveryAndDuplicateDelivery(t *testing.T) {
	options := redisLiveSagaOptions(t, 0)
	first := newLiveRedisSagaStore(t, options)
	require.NoError(t, first.StartTransaction("recovery", "rule"))
	require.NoError(t, first.RecordEffect("recovery", "step-000001", 1, "charge", map[string]interface{}{"amount": 42}))
	require.NoError(t, first.MarkSuccess("recovery", "step-000001", map[string]interface{}{"receipt": "one"}))
	require.NoError(t, first.Close())

	second := newLiveRedisSagaStore(t, options)
	t.Cleanup(func() { _ = second.Close() })
	require.NoError(t, second.StartTransaction("recovery", "rule"))
	require.NoError(t, second.RecordEffect("recovery", "step-000001", 1, "charge", map[string]interface{}{"amount": 42}))
	effects, err := second.GetTransactionEffects("recovery")
	require.NoError(t, err)
	require.Len(t, effects, 1)
	require.Equal(t, SagaEffectSuccess, effects[0].Status)
	require.NoError(t, second.CompleteSaga("recovery"))
	require.NoError(t, second.StartTransaction("recovery", "rule"), "completed replay must not reopen the saga")
	active, err := second.GetActiveSagas()
	require.NoError(t, err)
	require.NotContains(t, active, "recovery")
	require.ErrorContains(t, second.StartTransaction("recovery", "different-rule"), "identity conflict")
}

func TestRedisSagaStoreRetriesOptimisticRecordConflicts(t *testing.T) {
	store := newLiveRedisSagaStore(t, redisLiveSagaOptions(t, 0))
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.StartTransaction("optimistic", "rule"))

	const effectsCount = 24
	results := make(chan error, effectsCount)
	var wait sync.WaitGroup
	wait.Add(effectsCount)
	for index := 0; index < effectsCount; index++ {
		index := index
		go func() {
			defer wait.Done()
			results <- store.RecordEffect("optimistic", SagaEffectID(index+1), index+1, "write", map[string]interface{}{"index": index})
		}()
	}
	wait.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	effects, err := store.GetTransactionEffects("optimistic")
	require.NoError(t, err)
	require.Len(t, effects, effectsCount)
}

func TestRedisSagaStoreTTLStartsOnlyAfterCompletion(t *testing.T) {
	store := newLiveRedisSagaStore(t, redisLiveSagaOptions(t, 250*time.Millisecond))
	require.NoError(t, store.StartTransaction("ttl", "rule"))
	require.NoError(t, store.RecordEffect("ttl", "step-000001", 1, "write", map[string]interface{}{}))
	require.Never(t, func() bool {
		active, err := store.GetActiveSagas()
		return err != nil || len(active) == 0
	}, 750*time.Millisecond, 25*time.Millisecond)
	require.NoError(t, store.CompleteSaga("ttl"))
	require.Eventually(t, func() bool {
		effects, err := store.GetTransactionEffects("ttl")
		if err != nil || len(effects) != 0 {
			return false
		}
		active, err := store.GetActiveSagas()
		return err == nil && len(active) == 0
	}, 3*time.Second, 25*time.Millisecond)
	require.NoError(t, store.Close())
}

func redisLiveSagaOptions(t *testing.T, ttl time.Duration) RedisSagaStoreOptions {
	t.Helper()
	return redisLiveSagaOptionsWithPrefix(t, ttl, "effectus-legacy-test:"+uuid.NewString()+":")
}

func redisLiveSagaOptionsWithPrefix(t *testing.T, ttl time.Duration, prefix string) RedisSagaStoreOptions {
	t.Helper()
	address := os.Getenv("REDIS_ADDR")
	if address == "" {
		t.Skip("REDIS_ADDR is required for live Redis integration tests")
	}
	return RedisSagaStoreOptions{Addr: address, Prefix: prefix, TTL: ttl}
}

func newLiveRedisSagaStore(t *testing.T, options RedisSagaStoreOptions) *RedisSagaStore {
	t.Helper()
	store, err := NewRedisSagaStore(options)
	require.NoError(t, err)
	return store
}
