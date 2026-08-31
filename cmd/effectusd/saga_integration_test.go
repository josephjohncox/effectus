//go:build integration
// +build integration

package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/josephjohncox/effectus/schema"
	"github.com/josephjohncox/effectus/schema/capability"
	"github.com/josephjohncox/effectus/schema/types"
	"github.com/josephjohncox/effectus/schema/verb"
	"github.com/josephjohncox/effectus/unified"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

type sagaTestExecutor struct {
	name  string
	calls *[]string
	fail  bool
}

func (e *sagaTestExecutor) Execute(_ context.Context, _ map[string]interface{}) (interface{}, error) {
	if e.calls != nil {
		*e.calls = append(*e.calls, e.name)
	}
	if e.fail {
		return nil, fmt.Errorf("intentional failure")
	}
	return true, nil
}

func TestSagaIntegrationPostgres(t *testing.T) {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN not set")
	}

	store := waitForPostgresSagaStore(t, dsn)
	runSagaIntegration(t, store, func(sagaID string) {
		_ = cleanupPostgresSaga(dsn, sagaID)
	})
}

func TestSagaIntegrationRedis(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR not set")
	}

	prefix := fmt.Sprintf("effectus_test:%d:", time.Now().UnixNano())
	store := waitForRedisSagaStore(t, schema.RedisSagaStoreOptions{
		Addr:   addr,
		Prefix: prefix,
		TTL:    5 * time.Minute,
	})
	runSagaIntegration(t, store, func(sagaID string) {
		_ = cleanupRedisSaga(addr, prefix, sagaID)
	})
}

func runSagaIntegration(t *testing.T, store schema.SagaStore, cleanup func(string)) {
	t.Helper()
	if store == nil {
		t.Fatal("saga store not configured")
	}

	flowContent := `
flow "ChargeOrder" priority 1 {
  when {
    order.total > 100
  }
  steps {
    ReserveInventory(orderId: order.id)
    ChargeCard(orderId: order.id, amount: order.total)
  }
}
`

	typeSystem := types.NewTypeSystem()
	typeSystem.RegisterFactType("order.id", types.NewStringType())
	typeSystem.RegisterFactType("order.total", types.NewFloatType())
	require.NoError(t, typeSystem.RegisterVerbType("ReserveInventory", map[string]*types.Type{
		"orderId": types.NewStringType(),
	}, types.NewBoolType()))
	require.NoError(t, typeSystem.RegisterVerbType("ReleaseInventory", map[string]*types.Type{
		"orderId": types.NewStringType(),
	}, types.NewBoolType()))
	require.NoError(t, typeSystem.RegisterVerbType("ChargeCard", map[string]*types.Type{
		"orderId": types.NewStringType(),
		"amount":  types.NewFloatType(),
	}, types.NewBoolType()))

	calls := []string{}
	verbReg := verb.NewRegistry(typeSystem)
	require.NoError(t, verbReg.RegisterVerb(verb.NewSpec("ReserveInventory", verb.CapWrite, map[string]string{
		"orderId": "string",
	}, "bool").WithInverse("ReleaseInventory").WithExecutor(&sagaTestExecutor{name: "ReserveInventory", calls: &calls})))
	require.NoError(t, verbReg.RegisterVerb(verb.NewSpec("ReleaseInventory", verb.CapWrite, map[string]string{
		"orderId": "string",
	}, "bool").WithExecutor(&sagaTestExecutor{name: "ReleaseInventory", calls: &calls})))
	require.NoError(t, verbReg.RegisterVerb(verb.NewSpec("ChargeCard", verb.CapWrite, map[string]string{
		"orderId": "string",
		"amount":  "float",
	}, "bool").WithExecutor(&sagaTestExecutor{name: "ChargeCard", calls: &calls, fail: true})))

	bundle := &unified.Bundle{
		Name:    "saga-integration",
		Version: "1.0.0",
		RuleSources: []unified.RuleSource{
			{Path: "rules/charge.effx", Format: "effx", Content: flowContent},
		},
	}

	prepared, err := compileBundleRules(bundle, typeSystem, verbReg, false)
	require.NoError(t, err)
	require.NotNil(t, prepared.FlowSpec)

	state := newServerState(
		prepared,
		nil,
		nil,
		factStoreConfig{},
		apiAuth{mode: "disabled"},
		nil,
		nil,
		typeSystem,
		nil,
		verbReg,
		false,
		nil,
		true,
		store,
		capability.NewCapabilitySystem(),
	)

	requestID := time.Now().UnixNano()
	ctx := context.WithValue(context.Background(), requestIDContextKey, requestID)

	facts := map[string]interface{}{
		"order": map[string]interface{}{
			"id":    "ORD-9",
			"total": 250.0,
		},
	}

	err = state.ExecuteFacts(ctx, factEnvelope{Universe: "default", Facts: facts})
	require.Error(t, err)
	require.Equal(t, []string{"ReserveInventory", "ChargeCard", "ReleaseInventory"}, calls)

	active, err := store.GetActiveSagas()
	require.NoError(t, err)
	require.NotEmpty(t, active)
	sagaID := active[0]

	effects, err := store.GetTransactionEffects(sagaID)
	require.NoError(t, err)
	require.Len(t, effects, 2)

	statuses := map[string]string{}
	for _, eff := range effects {
		statuses[eff.Verb] = eff.Status
	}
	require.Contains(t, []string{"success", "compensated"}, statuses["ReserveInventory"])
	require.Equal(t, "failed", statuses["ChargeCard"])

	if cleanup != nil {
		cleanup(sagaID)
	}
}

func waitForPostgresSagaStore(t *testing.T, dsn string) *schema.PostgresSagaStore {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		store, err := schema.NewPostgresSagaStore(dsn)
		if err == nil {
			return store
		}
		lastErr = err
		time.Sleep(1 * time.Second)
	}
	t.Fatalf("postgres saga store not ready: %v", lastErr)
	return nil
}

func waitForRedisSagaStore(t *testing.T, opts schema.RedisSagaStoreOptions) *schema.RedisSagaStore {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		store, err := schema.NewRedisSagaStore(opts)
		if err == nil {
			return store
		}
		lastErr = err
		time.Sleep(1 * time.Second)
	}
	t.Fatalf("redis saga store not ready: %v", lastErr)
	return nil
}

func cleanupPostgresSaga(dsn, sagaID string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(`DELETE FROM effectus_saga_effects WHERE saga_id = $1`, sagaID); err != nil {
		return err
	}
	_, err = db.Exec(`DELETE FROM effectus_sagas WHERE saga_id = $1`, sagaID)
	return err
}

func cleanupRedisSaga(addr, prefix, sagaID string) error {
	client := redis.NewClient(&redis.Options{Addr: addr})
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	keys := []string{
		prefix + "saga:" + sagaID,
		prefix + "saga:" + sagaID + ":effects",
	}
	return client.Del(ctx, keys...).Err()
}
