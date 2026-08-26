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
}

func TestPostgresMigrationDriverIsRegistered(t *testing.T) {
	require.Contains(t, sql.Drivers(), "postgres")
}
