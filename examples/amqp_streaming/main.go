package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/effectus/effectus-go/adapters"
	_ "github.com/effectus/effectus-go/adapters/amqp"
)

func main() {
	url := envOrDefault("AMQP_URL", "amqp://guest:guest@localhost:5672/")
	queue := envOrDefault("AMQP_QUEUE", "effectus.events")

	config := adapters.SourceConfig{
		SourceID: "amqp_example",
		Type:     "amqp",
		Config: map[string]interface{}{
			"url":           url,
			"queue":         queue,
			"queue_declare": true,
			"auto_ack":      true,
			"format":        "json",
			"schema_name":   "example.event",
		},
	}

	source, err := adapters.CreateSource(config)
	if err != nil {
		log.Fatalf("create source: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	facts, err := source.Subscribe(ctx, nil)
	if err != nil {
		log.Fatalf("subscribe: %v", err)
	}
	defer source.Stop(ctx)

	if err := publishMessages(url, queue); err != nil {
		log.Fatalf("publish: %v", err)
	}

	for i := 0; i < 2; i++ {
		select {
		case fact := <-facts:
			log.Printf("fact schema=%s source=%s", fact.SchemaName, fact.SourceID)
			log.Printf("raw=%s", string(fact.RawData))
		case <-ctx.Done():
			log.Printf("timeout waiting for fact")
			return
		}
	}
}

func publishMessages(url, queue string) error {
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

	payloads := []map[string]interface{}{
		{"type": "event.created", "id": "evt-1", "ts": time.Now().UTC().Format(time.RFC3339Nano)},
		{"type": "event.updated", "id": "evt-2", "ts": time.Now().UTC().Format(time.RFC3339Nano)},
	}
	for _, payload := range payloads {
		raw, _ := json.Marshal(payload)
		if err := channel.Publish("", queue, false, false, amqp.Publishing{
			ContentType: "application/json",
			Body:        raw,
		}); err != nil {
			return err
		}
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
