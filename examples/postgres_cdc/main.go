package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/lib/pq"

	"github.com/effectus/effectus-go/adapters"
	"github.com/effectus/effectus-go/adapters/postgres"
)

func main() {
	dsn := envOrDefault("POSTGRES_DSN", "postgres://effectus:effectus@localhost:5432/effectus_cdc?sslmode=disable")

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS cdc_events (
		id SERIAL PRIMARY KEY,
		name TEXT,
		created_at TIMESTAMPTZ DEFAULT now()
	)`); err != nil {
		log.Fatalf("create table: %v", err)
	}

	slotName := fmt.Sprintf("effectus_example_%d", time.Now().UnixNano())
	source, err := postgres.NewCDCSource(&postgres.CDCConfig{
		SourceID:         "postgres_cdc_example",
		ConnectionString: dsn,
		SlotName:         slotName,
		Plugin:           "wal2json",
		CreateSlot:       true,
		PollInterval:     1 * time.Second,
		MaxChanges:       100,
		SchemaMapping: map[string]string{
			"public.cdc_events": "example.cdc_event",
		},
	})
	if err != nil {
		log.Fatalf("create source: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	facts, err := source.Subscribe(ctx, nil)
	if err != nil {
		log.Fatalf("subscribe: %v", err)
	}
	defer source.Stop(ctx)

	time.Sleep(500 * time.Millisecond)

	if _, err := db.Exec("INSERT INTO cdc_events(name) VALUES ($1)", "example"); err != nil {
		log.Fatalf("insert row: %v", err)
	}

	select {
	case fact := <-facts:
		log.Printf("fact schema=%s source=%s", fact.SchemaName, fact.SourceID)
		log.Printf("raw=%s", string(fact.RawData))
	case <-ctx.Done():
		log.Printf("timeout waiting for fact")
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
