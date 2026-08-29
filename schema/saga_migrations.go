package schema

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var sagaMigrations embed.FS

// MigrateSagaV2 applies the versioned durable runtime migrations. Production
// deployments should use a separate DDL credential and validate at runtime.
func MigrateSagaV2(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("saga migration database is required")
	}
	lockConnection, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire saga migration lock connection: %w", err)
	}
	defer lockConnection.Close()
	const migrationLockID int64 = 0x4566666563747573
	for {
		var acquired bool
		if err := lockConnection.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, migrationLockID).Scan(&acquired); err != nil {
			return fmt.Errorf("acquire saga migration lock: %w", err)
		}
		if acquired {
			break
		}
		// A blocking advisory-lock statement keeps a transaction snapshot alive.
		// That snapshot can deadlock CREATE INDEX CONCURRENTLY in the lock holder.
		// Poll only between completed statements so no waiting snapshot remains.
		select {
		case <-ctx.Done():
			return fmt.Errorf("acquire saga migration lock: %w", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	defer func() {
		_, _ = lockConnection.ExecContext(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, migrationLockID)
	}()
	provider, err := newSagaMigrationProvider(db)
	if err != nil {
		return err
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("apply saga V2 migrations: %w", err)
	}
	return nil
}

// ValidateSagaV2 verifies that every embedded durable runtime migration is
// applied. It executes SELECT statements only and is safe for a DML role.
func ValidateSagaV2(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("saga migration database is required")
	}
	entries, err := fs.ReadDir(sagaMigrations, "migrations")
	if err != nil {
		return fmt.Errorf("list durable runtime migrations: %w", err)
	}
	var targetVersion int64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		versionText, _, ok := strings.Cut(entry.Name(), "_")
		if !ok {
			return fmt.Errorf("invalid durable runtime migration name %q", entry.Name())
		}
		version, err := strconv.ParseInt(versionText, 10, 64)
		if err != nil {
			return fmt.Errorf("parse durable runtime migration %q: %w", entry.Name(), err)
		}
		if version > targetVersion {
			targetVersion = version
		}
		var applied bool
		err = db.QueryRowContext(ctx, `
			SELECT is_applied
			FROM effectus_saga_goose_db_version
			WHERE version_id = $1
			ORDER BY id DESC
			LIMIT 1
		`, version).Scan(&applied)
		if err != nil {
			return fmt.Errorf("validate durable runtime migration %d: %w", version, err)
		}
		if !applied {
			return fmt.Errorf("durable runtime migration %d is not applied", version)
		}
	}
	var databaseVersion int64
	err = db.QueryRowContext(ctx, `
		WITH latest AS (
			SELECT DISTINCT ON (version_id) version_id, is_applied
			FROM effectus_saga_goose_db_version
			ORDER BY version_id, id DESC
		)
		SELECT COALESCE(max(version_id), 0) FROM latest WHERE is_applied
	`).Scan(&databaseVersion)
	if err != nil {
		return fmt.Errorf("read durable runtime migration version: %w", err)
	}
	if databaseVersion > targetVersion {
		return fmt.Errorf("database migration version %d is newer than binary version %d", databaseVersion, targetVersion)
	}
	return nil
}

func newSagaMigrationProvider(db *sql.DB) (*goose.Provider, error) {
	migrations, err := fs.Sub(sagaMigrations, "migrations")
	if err != nil {
		return nil, fmt.Errorf("open saga migrations: %w", err)
	}
	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		db,
		migrations,
		goose.WithTableName("effectus_saga_goose_db_version"),
	)
	if err != nil {
		return nil, fmt.Errorf("create saga migration provider: %w", err)
	}
	return provider, nil
}
