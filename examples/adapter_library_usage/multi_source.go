package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/effectus/effectus-go/adapters"
	_ "github.com/effectus/effectus-go/adapters/files"
	_ "github.com/effectus/effectus-go/adapters/postgres"
	_ "github.com/effectus/effectus-go/adapters/redis"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Configuration for multiple sources
	configs := []adapters.SourceConfig{
		{
			SourceID: "postgres_users",
			Type:     "postgres_poller",
			Config: map[string]interface{}{
				"connection_string": "postgres://effectus:password@localhost:5432/app_db",
				"query":             "SELECT id, name, email, created_at FROM users",
				"interval_seconds":  30,
				"timestamp_column":  "created_at",
				"max_rows":          100,
				"schema_name":       "user_profile",
			},
		},
		{
			SourceID: "redis_events",
			Type:     "redis_streams",
			Config: map[string]interface{}{
				"redis_addr":     "localhost:6379",
				"redis_db":       0,
				"streams":        []string{"user:events", "order:events", "system:logs"},
				"consumer_group": "effectus_consumers",
				"consumer_name":  "consumer_1",
				"batch_size":     50,
				"block_time":     "2s",
				"schema_name":    "redis_event",
			},
		},
		{
			SourceID: "config_files",
			Type:     "file_watcher",
			Config: map[string]interface{}{
				"paths":         []string{"./config", "/etc/app"},
				"patterns":      []string{"*.json", "*.yaml", "*.toml"},
				"events":        []string{"CREATE", "WRITE", "REMOVE"},
				"recursive":     true,
				"max_file_size": 1048576, // 1MB
				"schema_name":   "config_change",
			},
		},
	}

	// Start all sources
	var sources []adapters.FactSource
	var factChannels []<-chan *adapters.TypedFact

	for _, config := range configs {
		log.Printf("Starting source: %s (%s)", config.SourceID, config.Type)

		// Create source
		source, err := adapters.CreateSource(config)
		if err != nil {
			log.Fatalf("Failed to create source %s: %v", config.SourceID, err)
		}

		// Start source
		if err := source.Start(ctx); err != nil {
			log.Fatalf("Failed to start source %s: %v", config.SourceID, err)
		}

		sources = append(sources, source)

		// Subscribe to facts
		factChan, err := source.Subscribe(ctx, nil) // Subscribe to all fact types
		if err != nil {
			log.Fatalf("Failed to subscribe to source %s: %v", config.SourceID, err)
		}

		factChannels = append(factChannels, factChan)
		log.Printf("Source %s started successfully", config.SourceID)
	}

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Process facts from all sources
	var wg sync.WaitGroup

	// Start fact processors for each source
	for i, factChan := range factChannels {
		wg.Add(1)
		go func(sourceIndex int, ch <-chan *adapters.TypedFact) {
			defer wg.Done()

			sourceID := configs[sourceIndex].SourceID
			log.Printf("Started fact processor for source: %s", sourceID)

			for {
				select {
				case fact, ok := <-ch:
					if !ok {
						log.Printf("Fact channel closed for source: %s", sourceID)
						return
					}

					processFact(fact)

				case <-ctx.Done():
					log.Printf("Context cancelled, stopping processor for source: %s", sourceID)
					return
				}
			}
		}(i, factChan)
	}

	// Wait for shutdown signal
	log.Printf("All sources started. Processing facts... (Press Ctrl+C to stop)")
	<-sigChan
	log.Printf("Shutdown signal received. Stopping sources...")

	// Cancel context to stop all sources
	cancel()

	// Stop all sources gracefully
	for i, source := range sources {
		log.Printf("Stopping source: %s", configs[i].SourceID)
		if err := source.Stop(ctx); err != nil {
			log.Printf("Error stopping source %s: %v", configs[i].SourceID, err)
		}
	}

	// Wait for all processors to finish
	wg.Wait()
	log.Printf("All sources stopped successfully")
}

func processFact(fact *adapters.TypedFact) {
	log.Printf("Processing fact: %s from %s at %v",
		fact.SchemaName,
		fact.SourceID,
		fact.Timestamp.Format("15:04:05"))

	// Example processing based on source type
	switch fact.SourceID {
	case "postgres_users":
		processUserProfileUpdate(fact)
	case "redis_events":
		processRealtimeEvent(fact)
	case "config_files":
		processConfigurationChange(fact)
	default:
		log.Printf("Unknown source: %s", fact.SourceID)
	}

	// Log metadata for debugging
	if len(fact.Metadata) > 0 {
		log.Printf("  Metadata: %v", fact.Metadata)
	}
}

func processUserProfileUpdate(fact *adapters.TypedFact) {
	log.Printf("  -> User profile update detected")

	// Example: Parse the database row data
	var userData map[string]interface{}
	if err := json.Unmarshal(fact.RawData, &userData); err == nil {
		if userID, ok := userData["id"]; ok {
			log.Printf("  -> User ID: %v", userID)
		}
		if email, ok := userData["email"]; ok {
			log.Printf("  -> Email: %v", email)
		}
	}

	// Here you would:
	// 1. Transform to Effectus fact format
	// 2. Apply business rules
	// 3. Trigger effects if conditions are met
}

func processRealtimeEvent(fact *adapters.TypedFact) {
	log.Printf("  -> Real-time Redis event processed")

	// Example: Handle different stream types
	if streamName, exists := fact.Metadata["redis.stream"]; exists {
		switch streamName {
		case "user:events":
			log.Printf("  -> User event from Redis")
		case "order:events":
			log.Printf("  -> Order event from Redis")
		case "system:logs":
			log.Printf("  -> System log from Redis")
		}
	}

	// Here you would:
	// 1. Parse Redis stream data
	// 2. Route based on event type
	// 3. Apply real-time business logic
}

func processConfigurationChange(fact *adapters.TypedFact) {
	log.Printf("  -> Configuration file change detected")

	// Example: Handle different file operations
	if operation, exists := fact.Metadata["file.operation"]; exists {
		filename := fact.Metadata["file.name"]

		switch operation {
		case "CREATE":
			log.Printf("  -> New config file created: %s", filename)
		case "WRITE":
			log.Printf("  -> Config file modified: %s", filename)
		case "REMOVE":
			log.Printf("  -> Config file removed: %s", filename)
		}
	}

	// Here you would:
	// 1. Parse configuration changes
	// 2. Validate new configuration
	// 3. Trigger application reloads if needed
}

// Example health check routine
func healthCheckSources(sources []adapters.FactSource, configs []adapters.SourceConfig) {
	for i, source := range sources {
		if err := source.HealthCheck(); err != nil {
			log.Printf("Health check failed for source %s: %v", configs[i].SourceID, err)
		} else {
			log.Printf("Source %s is healthy", configs[i].SourceID)
		}
	}
}
