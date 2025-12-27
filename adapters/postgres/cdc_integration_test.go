//go:build integration
// +build integration

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

func TestPostgresCDCIntegration(t *testing.T) {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN not set")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	readyCtx, readyCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer readyCancel()
	if err := waitForPostgres(readyCtx, db); err != nil {
		t.Fatalf("db not ready: %v", err)
	}

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS cdc_events (
		id SERIAL PRIMARY KEY,
		name TEXT,
		created_at TIMESTAMPTZ DEFAULT now()
	)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	slotName := fmt.Sprintf("effectus_test_%d", time.Now().UnixNano())
	source, err := NewCDCSource(&CDCConfig{
		SourceID:         "postgres_cdc_test",
		ConnectionString: dsn,
		SlotName:         slotName,
		Plugin:           "wal2json",
		CreateSlot:       true,
		PollInterval:     1 * time.Second,
		MaxChanges:       50,
		SchemaMapping: map[string]string{
			"public.cdc_events": "test.cdc_event",
		},
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	facts, err := source.Subscribe(ctx, nil)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer source.Stop(ctx)
	defer db.Exec("SELECT pg_drop_replication_slot($1)", slotName)

	time.Sleep(500 * time.Millisecond)

	if _, err := db.Exec("INSERT INTO cdc_events(name) VALUES ($1)", "integration"); err != nil {
		t.Fatalf("insert row: %v", err)
	}

	select {
	case fact := <-facts:
		if fact == nil {
			t.Fatalf("expected fact")
		}
	case <-ctx.Done():
		t.Fatalf("timeout waiting for fact")
	}
}

func waitForPostgres(ctx context.Context, db *sql.DB) error {
	var lastErr error
	for i := 0; i < 30; i++ {
		if err := db.PingContext(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
	return lastErr
}
