//go:build integration
// +build integration

package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func TestMySQLCDCIntegration(t *testing.T) {
	host := os.Getenv("MYSQL_HOST")
	user := os.Getenv("MYSQL_USER")
	password := os.Getenv("MYSQL_PASSWORD")
	database := os.Getenv("MYSQL_DATABASE")
	dsn := os.Getenv("MYSQL_DSN")
	port := envInt("MYSQL_PORT", 3306)

	if host == "" || user == "" || password == "" || database == "" || dsn == "" {
		t.Skip("MYSQL_HOST, MYSQL_USER, MYSQL_PASSWORD, MYSQL_DATABASE, MYSQL_DSN must be set")
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS cdc_events (
		id INT AUTO_INCREMENT PRIMARY KEY,
		name VARCHAR(255),
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	source, err := NewCDCSource(&CDCConfig{
		SourceID:   "mysql_cdc_test",
		Host:       host,
		Port:       port,
		User:       user,
		Password:   password,
		Database:   database,
		ServerID:   uint32(time.Now().UnixNano() % 100000),
		Flavor:     "mysql",
		BufferSize: 1000,
		Tables:     []string{fmt.Sprintf("%s.cdc_events", database)},
		SchemaMapping: map[string]string{
			fmt.Sprintf("%s.cdc_events", database): "test.cdc_event",
		},
		DSN: dsn,
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

	time.Sleep(500 * time.Millisecond)

	if _, err := db.Exec("INSERT INTO cdc_events(name) VALUES (?)", "integration"); err != nil {
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

func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
