package runtime

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewPostgresStorageRejectsInvalidConfig(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		storage, err := NewPostgresStorage(nil)
		require.Nil(t, storage)
		require.EqualError(t, err, "PostgreSQL storage config is required")
	})

	t.Run("empty DSN", func(t *testing.T) {
		storage, err := NewPostgresStorage(&PostgresStorageConfig{})
		require.Nil(t, storage)
		require.EqualError(t, err, "PostgreSQL DSN is required")
	})

	t.Run("negative max connections", func(t *testing.T) {
		storage, err := NewPostgresStorage(&PostgresStorageConfig{DSN: "postgres://localhost/test", MaxConnections: -1})
		require.Nil(t, storage)
		require.EqualError(t, err, "PostgreSQL max connections cannot be negative")
	})
}

func TestValidateDeploymentIdentity(t *testing.T) {
	require.EqualError(t, validateDeploymentIdentity("", "production", "1"), "ruleset name is required")
	require.EqualError(t, validateDeploymentIdentity("orders", "", "1"), "deployment environment is required")
	require.EqualError(t, validateDeploymentIdentity("orders", "production", " "), "ruleset version is required")
	require.NoError(t, validateDeploymentIdentity("orders", "production", "1"))
}

func TestPostgresMigrationDriverIsRegistered(t *testing.T) {
	require.Contains(t, sql.Drivers(), "postgres")
}
