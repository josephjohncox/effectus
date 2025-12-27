package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"strconv"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/effectus/effectus-go/adapters/mysql"
)

func main() {
	host := envOrDefault("MYSQL_HOST", "localhost")
	port := envInt("MYSQL_PORT", 3306)
	user := envOrDefault("MYSQL_USER", "effectus")
	password := envOrDefault("MYSQL_PASSWORD", "effectus")
	database := envOrDefault("MYSQL_DATABASE", "effectus_cdc")
	dsn := envOrDefault("MYSQL_DSN", "effectus:effectus@tcp(localhost:3306)/effectus_cdc?parseTime=true")

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS cdc_events (
		id INT AUTO_INCREMENT PRIMARY KEY,
		name VARCHAR(255),
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		log.Fatalf("create table: %v", err)
	}

	source, err := mysql.NewCDCSource(&mysql.CDCConfig{
		SourceID:   "mysql_cdc_example",
		Host:       host,
		Port:       port,
		User:       user,
		Password:   password,
		Database:   database,
		ServerID:   uint32(time.Now().UnixNano() % 100000),
		Flavor:     "mysql",
		BufferSize: 1000,
		Tables:     []string{database + ".cdc_events"},
		SchemaMapping: map[string]string{
			database + ".cdc_events": "example.cdc_event",
		},
		DSN: dsn,
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

	if _, err := db.Exec("INSERT INTO cdc_events(name) VALUES (?)", "example"); err != nil {
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
