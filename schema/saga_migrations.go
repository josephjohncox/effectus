package schema

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var sagaMigrations embed.FS

// MigrateSagaV2 applies the versioned saga outbox and fencing migrations.
// Production deployments should normally run the same files before startup.
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
	if _, err := lockConnection.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		return fmt.Errorf("acquire saga migration lock: %w", err)
	}
	defer func() {
		_, _ = lockConnection.ExecContext(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, migrationLockID)
	}()
	migrations, err := fs.Sub(sagaMigrations, "migrations")
	if err != nil {
		return fmt.Errorf("open saga migrations: %w", err)
	}
	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		db,
		migrations,
		goose.WithTableName("effectus_saga_goose_db_version"),
	)
	if err != nil {
		return fmt.Errorf("create saga migration provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("apply saga V2 migrations: %w", err)
	}
	return nil
}
