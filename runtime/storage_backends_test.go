package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInMemoryDeploymentLifecycleMatchesAtomicReplacement(t *testing.T) {
	ctx := context.Background()
	storage := NewInMemoryRuleStorage()
	for _, version := range []string{"1.0.0", "2.0.0"} {
		require.NoError(t, storage.StoreRuleset(ctx, &StoredRuleset{
			Name:        "orders",
			Version:     version,
			Ruleset:     &CompiledRuleset{},
			Deployments: make(map[string]*Deployment),
		}))
	}

	require.ErrorIs(t, storage.DeployRuleset(ctx, "orders", "1.0.0", "production", &DeploymentConfig{Strategy: "canary"}), ErrUnsupportedDeploymentStrategy)
	require.NoError(t, storage.DeployRuleset(ctx, "orders", "1.0.0", "production", &DeploymentConfig{Strategy: "atomic"}))
	require.ErrorIs(t, storage.DeleteRuleset(ctx, "orders", "1.0.0"), ErrRulesetActive)
	require.NoError(t, storage.DeployRuleset(ctx, "orders", "2.0.0", "production", &DeploymentConfig{Strategy: "atomic"}))
	active, err := storage.GetActiveVersion(ctx, "orders", "production")
	require.NoError(t, err)
	require.Equal(t, "2.0.0", active.Version)

	v1, err := storage.GetRuleset(ctx, "orders", "1.0.0")
	require.NoError(t, err)
	require.Equal(t, DeploymentStatusInactive, v1.Deployments["production"].Status)

	require.NoError(t, storage.RollbackDeployment(ctx, "orders", "production", "1.0.0"))
	active, err = storage.GetActiveVersion(ctx, "orders", "production")
	require.NoError(t, err)
	require.Equal(t, "1.0.0", active.Version)

	err = storage.RollbackDeployment(ctx, "orders", "production", "missing")
	require.Error(t, err)
	active, activeErr := storage.GetActiveVersion(ctx, "orders", "production")
	require.NoError(t, activeErr)
	require.Equal(t, "1.0.0", active.Version)
}
