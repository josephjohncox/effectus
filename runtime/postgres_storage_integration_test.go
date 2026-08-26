//go:build integration

package runtime

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPostgresStorageLifecycle(t *testing.T) {
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		t.Skip("DB_DSN is required for PostgreSQL integration tests")
	}

	storage, err := NewPostgresStorage(&PostgresStorageConfig{
		DSN:         dsn,
		AutoMigrate: true,
	})
	require.NoError(t, err)
	t.Cleanup(storage.Close)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	_, err = storage.pool.Exec(ctx, "TRUNCATE audit_log, deployments, rulesets CASCADE")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = storage.pool.Exec(context.Background(), "TRUNCATE audit_log, deployments, rulesets CASCADE")
	})

	storeVersion := func(version string) {
		t.Helper()
		now := time.Now().UTC()
		require.NoError(t, storage.StoreRuleset(ctx, &StoredRuleset{
			Ruleset:         &CompiledRuleset{Name: "orders", Version: version, FactSchema: &Schema{Name: "facts"}},
			Name:            "orders",
			Version:         version,
			Environment:     "production",
			Status:          RulesetStatusReady,
			CreatedAt:       now,
			UpdatedAt:       now,
			CompiledAt:      now,
			CreatedBy:       "integration-test",
			UpdatedBy:       "integration-test",
			CompilerVersion: "test",
		}))
	}
	storeVersion("1.0.0")
	storeVersion("2.0.0")

	versions, err := storage.GetRulesetVersions(ctx, "orders")
	require.NoError(t, err)
	require.Len(t, versions, 2)

	require.ErrorIs(t, storage.DeployRuleset(ctx, "orders", "1.0.0", "production", &DeploymentConfig{Strategy: "canary"}), ErrUnsupportedDeploymentStrategy)
	require.NoError(t, storage.DeployRuleset(ctx, "orders", "1.0.0", "production", &DeploymentConfig{Strategy: "atomic"}))
	require.ErrorIs(t, storage.DeleteRuleset(ctx, "orders", "1.0.0"), ErrRulesetActive)
	active, err := storage.GetActiveVersion(ctx, "orders", "production")
	require.NoError(t, err)
	require.Equal(t, "1.0.0", active.Version)

	require.NoError(t, storage.SetActiveVersion(ctx, "orders", "production", "2.0.0"))
	active, err = storage.GetActiveVersion(ctx, "orders", "production")
	require.NoError(t, err)
	require.Equal(t, "2.0.0", active.Version)
	status, err := storage.GetDeploymentStatus(ctx, "orders", "production")
	require.NoError(t, err)
	require.Equal(t, DeploymentStatusActive, *status)

	require.NoError(t, storage.RollbackDeployment(ctx, "orders", "production", "1.0.0"))
	active, err = storage.GetActiveVersion(ctx, "orders", "production")
	require.NoError(t, err)
	require.Equal(t, "1.0.0", active.Version)

	require.NoError(t, storage.DeleteRuleset(ctx, "orders", "2.0.0"))
	_, err = storage.GetRuleset(ctx, "orders", "2.0.0")
	require.ErrorContains(t, err, "ruleset not found")
}
