package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"

	"github.com/effectus/effectus-go/adapters"
	_ "github.com/effectus/effectus-go/adapters/amqp"
	"github.com/effectus/effectus-go/adapters/mysql"
	"github.com/effectus/effectus-go/adapters/postgres"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	postgresDSN := envOrDefault("POSTGRES_DSN", "postgres://effectus:effectus@localhost:5432/effectus_cdc?sslmode=disable")
	mysqlHost := envOrDefault("MYSQL_HOST", "localhost")
	mysqlPort := envInt("MYSQL_PORT", 3306)
	mysqlUser := envOrDefault("MYSQL_USER", "effectus")
	mysqlPassword := envOrDefault("MYSQL_PASSWORD", "effectus")
	mysqlDatabase := envOrDefault("MYSQL_DATABASE", "effectus_cdc")
	mysqlDSN := envOrDefault("MYSQL_DSN", "effectus:effectus@tcp(localhost:3306)/effectus_cdc?parseTime=true")
	amqpURL := envOrDefault("AMQP_URL", "amqp://guest:guest@localhost:5672/")
	amqpQueue := envOrDefault("AMQP_QUEUE", "effectus.events")

	pgDB, err := sql.Open("postgres", postgresDSN)
	if err != nil {
		log.Fatalf("open postgres: %v", err)
	}
	defer pgDB.Close()

	if _, err := pgDB.Exec(`CREATE TABLE IF NOT EXISTS cdc_events (
		id SERIAL PRIMARY KEY,
		name TEXT,
		created_at TIMESTAMPTZ DEFAULT now()
	)`); err != nil {
		log.Fatalf("create postgres table: %v", err)
	}

	mysqlDB, err := sql.Open("mysql", mysqlDSN)
	if err != nil {
		log.Fatalf("open mysql: %v", err)
	}
	defer mysqlDB.Close()

	if _, err := mysqlDB.Exec(`CREATE TABLE IF NOT EXISTS cdc_events (
		id INT AUTO_INCREMENT PRIMARY KEY,
		name VARCHAR(255),
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		log.Fatalf("create mysql table: %v", err)
	}

	slotName := fmt.Sprintf("effectus_demo_%d", time.Now().UnixNano())
	pgSource, err := postgres.NewCDCSource(&postgres.CDCConfig{
		SourceID:         "postgres_cdc_demo",
		ConnectionString: postgresDSN,
		SlotName:         slotName,
		Plugin:           "wal2json",
		CreateSlot:       true,
		PollInterval:     1 * time.Second,
		MaxChanges:       100,
		SchemaMapping: map[string]string{
			"public.cdc_events": "demo.postgres_event",
		},
	})
	if err != nil {
		log.Fatalf("create postgres source: %v", err)
	}

	mysqlSource, err := mysql.NewCDCSource(&mysql.CDCConfig{
		SourceID:   "mysql_cdc_demo",
		Host:       mysqlHost,
		Port:       mysqlPort,
		User:       mysqlUser,
		Password:   mysqlPassword,
		Database:   mysqlDatabase,
		ServerID:   uint32(time.Now().UnixNano() % 100000),
		Flavor:     "mysql",
		BufferSize: 1000,
		Tables:     []string{mysqlDatabase + ".cdc_events"},
		SchemaMapping: map[string]string{
			mysqlDatabase + ".cdc_events": "demo.mysql_event",
		},
		DSN: mysqlDSN,
	})
	if err != nil {
		log.Fatalf("create mysql source: %v", err)
	}

	amqpSource, err := adapters.CreateSource(adapters.SourceConfig{
		SourceID: "amqp_demo",
		Type:     "amqp",
		Config: map[string]interface{}{
			"url":           amqpURL,
			"queue":         amqpQueue,
			"queue_declare": true,
			"auto_ack":      true,
			"format":        "json",
			"schema_name":   "demo.amqp_event",
		},
	})
	if err != nil {
		log.Fatalf("create amqp source: %v", err)
	}

	pgFacts, err := pgSource.Subscribe(ctx, nil)
	if err != nil {
		log.Fatalf("subscribe postgres: %v", err)
	}
	defer pgSource.Stop(ctx)
	defer pgDB.Exec("SELECT pg_drop_replication_slot($1)", slotName)

	mysqlFacts, err := mysqlSource.Subscribe(ctx, nil)
	if err != nil {
		log.Fatalf("subscribe mysql: %v", err)
	}
	defer mysqlSource.Stop(ctx)

	amqpFacts, err := amqpSource.Subscribe(ctx, nil)
	if err != nil {
		log.Fatalf("subscribe amqp: %v", err)
	}
	defer amqpSource.Stop(ctx)

	time.Sleep(500 * time.Millisecond)

	if _, err := pgDB.Exec("INSERT INTO cdc_events(name) VALUES ($1)", "demo"); err != nil {
		log.Fatalf("insert postgres: %v", err)
	}
	if _, err := mysqlDB.Exec("INSERT INTO cdc_events(name) VALUES (?)", "demo"); err != nil {
		log.Fatalf("insert mysql: %v", err)
	}
	if err := publishAMQP(amqpURL, amqpQueue); err != nil {
		log.Fatalf("publish amqp: %v", err)
	}

	pgCount := 0
	mysqlCount := 0
	amqpCount := 0

	for pgCount+mysqlCount+amqpCount < 3 {
		select {
		case fact := <-pgFacts:
			if fact != nil {
				pgCount++
				log.Printf("postgres fact schema=%s raw=%s", fact.SchemaName, string(fact.RawData))
			}
		case fact := <-mysqlFacts:
			if fact != nil {
				mysqlCount++
				log.Printf("mysql fact schema=%s raw=%s", fact.SchemaName, string(fact.RawData))
			}
		case fact := <-amqpFacts:
			if fact != nil {
				amqpCount++
				log.Printf("amqp fact schema=%s raw=%s", fact.SchemaName, string(fact.RawData))
			}
		case <-ctx.Done():
			log.Printf("timeout waiting for facts")
			return
		}
	}
}

func publishAMQP(url, queue string) error {
	conn, err := amqp.Dial(url)
	if err != nil {
		return err
	}
	defer conn.Close()

	channel, err := conn.Channel()
	if err != nil {
		return err
	}
	defer channel.Close()

	if _, err := channel.QueueDeclare(queue, false, false, false, false, nil); err != nil {
		return err
	}

	payload := map[string]interface{}{
		"type": "demo.amqp_event",
		"id":   "evt-1",
		"ts":   time.Now().UTC().Format(time.RFC3339Nano),
	}
	raw, _ := json.Marshal(payload)

	return channel.Publish("", queue, false, false, amqp.Publishing{
		ContentType: "application/json",
		Body:        raw,
	})
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
