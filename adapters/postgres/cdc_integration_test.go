//go:build integration
// +build integration

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/lib/pq"

	"github.com/effectus/effectus-go/adapters"
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
	t.Cleanup(func() {
		_ = source.Stop(context.Background())
		_, _ = db.Exec("SELECT pg_drop_replication_slot($1)", slotName)
	})

	time.Sleep(500 * time.Millisecond)

	if _, err := db.Exec("INSERT INTO cdc_events(name) VALUES ($1)", "integration"); err != nil {
		t.Fatalf("insert row: %v", err)
	}

	select {
	case fact := <-facts:
		if fact == nil {
			t.Fatalf("expected fact")
		}
		if fact.Acknowledge == nil {
			t.Fatalf("expected durable acknowledgement")
		}
		if err := fact.Acknowledge(ctx); err != nil {
			t.Fatalf("acknowledge fact: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("timeout waiting for fact")
	}
}

func TestPostgresCDCDoesNotAdvanceSlotUnderBackpressureIntegration(t *testing.T) {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := waitForPostgres(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS cdc_lossless_events (id BIGSERIAL PRIMARY KEY, value TEXT)`); err != nil {
		t.Fatal(err)
	}

	slot := fmt.Sprintf("effectus_lossless_%d", time.Now().UnixNano())
	source, err := NewCDCSource(&CDCConfig{
		SourceID: "lossless", ConnectionString: dsn, SlotName: slot, Plugin: "wal2json", CreateSlot: true,
		PollInterval: 20 * time.Millisecond, MaxChanges: 10, BufferSize: 1,
		Tables: []string{"cdc_lossless_events"}, SchemaMapping: map[string]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	facts, err := source.Subscribe(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = source.Stop(context.Background())
		_, _ = db.Exec("SELECT pg_drop_replication_slot($1)", slot)
	})

	var initial string
	if err := db.QueryRow("SELECT confirmed_flush_lsn::text FROM pg_replication_slots WHERE slot_name=$1", slot).Scan(&initial); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec("INSERT INTO cdc_lossless_events(value) VALUES ('first'), ('second')"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// One fact fits in the output buffer. The second blocks the same WAL
	// record, and neither is durably acknowledged, so the slot cannot move.
	time.Sleep(200 * time.Millisecond)
	var advanced bool
	if err := db.QueryRow("SELECT confirmed_flush_lsn > $2::pg_lsn FROM pg_replication_slots WHERE slot_name=$1", slot, initial).Scan(&advanced); err != nil {
		t.Fatal(err)
	}
	if advanced {
		t.Fatal("slot advanced before the complete WAL record was handed off")
	}
	for i := 0; i < 2; i++ {
		select {
		case fact := <-facts:
			if fact == nil || fact.Acknowledge == nil {
				t.Fatal("channel closed or acknowledgement missing")
			}
			if err := fact.Acknowledge(ctx); err != nil {
				t.Fatalf("acknowledge CDC fact: %v", err)
			}
		case <-ctx.Done():
			t.Fatal("timed out draining CDC facts")
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for !advanced && time.Now().Before(deadline) {
		if err := db.QueryRow("SELECT confirmed_flush_lsn > $2::pg_lsn FROM pg_replication_slots WHERE slot_name=$1", slot, initial).Scan(&advanced); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !advanced {
		t.Fatal("slot did not advance after complete ordered handoff")
	}

	if _, err := db.Exec("INSERT INTO cdc_lossless_events(value) VALUES ('third')"); err != nil {
		t.Fatal(err)
	}
	select {
	case fact := <-facts:
		if fact == nil || fact.Acknowledge == nil {
			t.Fatal("channel closed before second batch or acknowledgement missing")
		}
		if err := fact.Acknowledge(ctx); err != nil {
			t.Fatalf("acknowledge second batch: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("second independently committed batch was not emitted")
	}
}

func TestPostgresPollerTupleCursorIntegration(t *testing.T) {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	table := fmt.Sprintf("poll_lossless_%d", time.Now().UnixNano())
	ledger := fmt.Sprintf("poll_ledger_%d", time.Now().UnixNano())
	if _, err := db.Exec(fmt.Sprintf(`CREATE TABLE %s (id BIGSERIAL PRIMARY KEY, happened_at TIMESTAMPTZ NOT NULL, value TEXT)`, pq.QuoteIdentifier(table))); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmt.Sprintf(`CREATE TABLE %s (source_id TEXT NOT NULL, record_key TEXT NOT NULL, processed_at TIMESTAMPTZ NOT NULL, PRIMARY KEY (source_id, record_key))`, pq.QuoteIdentifier(ledger))); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP TABLE " + pq.QuoteIdentifier(table))
	defer db.Exec("DROP TABLE " + pq.QuoteIdentifier(ledger))
	stamp := time.Now().UTC().Truncate(time.Microsecond)
	for i := 0; i < 5; i++ {
		if _, err := db.Exec("INSERT INTO "+pq.QuoteIdentifier(table)+"(happened_at,value) VALUES ($1,$2)", stamp, fmt.Sprintf("v%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	poller, err := NewPostgresPollerSource("poller", PollerConfig{
		ConnectionString: dsn, Query: "SELECT id, happened_at, value FROM " + pq.QuoteIdentifier(table),
		TimestampColumn: "happened_at", TieBreakColumn: "id", ProcessedLedger: ledger, MaxRows: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := poller.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer poller.Stop(context.Background())

	blocked := make(chan *adapters.TypedFact, 1)
	blockedCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- poller.executePoll(blockedCtx, blocked) }()
	for len(blocked) != 1 {
		time.Sleep(time.Millisecond)
	}
	var processed int
	if err := db.QueryRow("SELECT count(*) FROM " + pq.QuoteIdentifier(ledger) + " WHERE source_id = 'poller'").Scan(&processed); err != nil {
		t.Fatal(err)
	}
	if processed != 0 {
		t.Fatalf("processed ledger advanced before acknowledgement: %d", processed)
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("expected blocked poll to be canceled")
	}
	first := <-blocked
	if first.Acknowledge == nil {
		t.Fatal("poller fact has no durable acknowledgement")
	}
	if err := first.Acknowledge(t.Context()); err != nil {
		t.Fatal(err)
	}
	var firstRow map[string]interface{}
	if err := json.Unmarshal(first.RawData, &firstRow); err != nil {
		t.Fatal(err)
	}
	if firstRow["id"].(float64) != 1 {
		t.Fatalf("first id = %v", firstRow["id"])
	}

	remaining := make(chan *adapters.TypedFact, 10)
	received, err := executePollAcknowledged(t, poller, remaining, 4)
	if err != nil {
		t.Fatal(err)
	}
	for index, want := range []int{2, 3, 4, 5} {
		fact := received[index]
		var row map[string]interface{}
		if err := json.Unmarshal(fact.RawData, &row); err != nil {
			t.Fatal(err)
		}
		if got := int(row["id"].(float64)); got != want {
			t.Fatalf("id = %d, want %d", got, want)
		}
	}
}

func TestPostgresPollerFindsDelayedLowerCommit(t *testing.T) {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	table := fmt.Sprintf("poll_delayed_%d", time.Now().UnixNano())
	ledger := fmt.Sprintf("poll_delayed_ledger_%d", time.Now().UnixNano())
	if _, err := db.Exec(fmt.Sprintf(`CREATE TABLE %s (id BIGINT PRIMARY KEY, happened_at TIMESTAMPTZ NOT NULL, value TEXT)`, pq.QuoteIdentifier(table))); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(fmt.Sprintf(`CREATE TABLE %s (source_id TEXT NOT NULL, record_key TEXT NOT NULL, processed_at TIMESTAMPTZ NOT NULL, PRIMARY KEY (source_id, record_key))`, pq.QuoteIdentifier(ledger))); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP TABLE " + pq.QuoteIdentifier(table))
	defer db.Exec("DROP TABLE " + pq.QuoteIdentifier(ledger))

	lowerTime := time.Now().UTC().Add(-time.Minute)
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec("INSERT INTO "+pq.QuoteIdentifier(table)+"(id,happened_at,value) VALUES (1,$1,'lower')", lowerTime); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO "+pq.QuoteIdentifier(table)+"(id,happened_at,value) VALUES (2,$1,'higher')", lowerTime.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	poller, err := NewPostgresPollerSource("delayed", PollerConfig{
		ConnectionString: dsn, Query: "SELECT id, happened_at, value FROM " + pq.QuoteIdentifier(table),
		TimestampColumn: "happened_at", TieBreakColumn: "id", ProcessedLedger: ledger, MaxRows: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := poller.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer poller.Stop(context.Background())

	first := make(chan *adapters.TypedFact, 2)
	if _, err := executePollAcknowledged(t, poller, first, 1); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	second := make(chan *adapters.TypedFact, 2)
	received, err := executePollAcknowledged(t, poller, second, 1)
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]interface{}
	if err := json.Unmarshal(received[0].RawData, &row); err != nil {
		t.Fatal(err)
	}
	if row["value"] != "lower" {
		t.Fatalf("delayed value = %v", row["value"])
	}
}

func executePollAcknowledged(t *testing.T, poller *PostgresPollerSource, out chan *adapters.TypedFact, count int) ([]*adapters.TypedFact, error) {
	t.Helper()
	errCh := make(chan error, 1)
	go func() { errCh <- poller.executePoll(t.Context(), out) }()
	facts := make([]*adapters.TypedFact, 0, count)
	for index := 0; index < count; index++ {
		select {
		case fact := <-out:
			if fact == nil || fact.Acknowledge == nil {
				t.Fatalf("poller fact %d is nil or missing acknowledgement", index)
			}
			facts = append(facts, fact)
			if err := fact.Acknowledge(t.Context()); err != nil {
				t.Fatalf("acknowledge poller fact %d: %v", index, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for poller fact %d", index)
		}
	}
	return facts, <-errCh
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
