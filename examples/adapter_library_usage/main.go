package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/effectus/effectus-go/adapters"
	// Auto-register adapters
	_ "github.com/effectus/effectus-go/adapters/http"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	log.Println("🚀 Starting Effectus Adapter Library Example")

	// Create and start fact sources
	sources, err := setupFactSources(ctx)
	if err != nil {
		log.Fatalf("Failed to setup fact sources: %v", err)
	}

	// Start fact processing
	factProcessor := NewFactProcessor()
	go factProcessor.ProcessFacts(ctx, sources)

	// Display available source types
	log.Printf("📋 Available source types: %v", adapters.GetAvailableSourceTypes())

	// Show example webhook calls
	showExampleUsage()

	// Wait for shutdown signal
	<-sigChan
	log.Println("🛑 Shutting down gracefully...")

	// Stop all sources
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	for _, source := range sources {
		if err := source.Stop(shutdownCtx); err != nil {
			log.Printf("Error stopping source: %v", err)
		}
	}

	log.Println("✅ Shutdown complete")
}

func setupFactSources(ctx context.Context) ([]adapters.FactSource, error) {
	var sources []adapters.FactSource

	// HTTP Webhook Source
	httpConfig := adapters.SourceConfig{
		SourceID: "api_webhooks",
		Type:     "http",
		Config: map[string]interface{}{
			"listen_port": 8080,
			"path":        "/webhook/events",
			"auth_method": "api_key",
			"auth_config": map[string]interface{}{
				"token_header":   "X-API-Key",
				"expected_token": "demo-api-key-123",
			},
		},
		Mappings: []adapters.FactMapping{
			{
				SourceKey:     "user.created",
				EffectusType:  "acme.v1.facts.UserProfile",
				SchemaVersion: "v1.0.0",
			},
			{
				SourceKey:     "order.completed",
				EffectusType:  "acme.v1.facts.OrderEvent",
				SchemaVersion: "v1.0.0",
			},
		},
		Tags: []string{"webhook", "api", "realtime"},
	}

	httpSource, err := adapters.CreateSource(httpConfig)
	if err != nil {
		return nil, err
	}

	if err := httpSource.Start(ctx); err != nil {
		return nil, err
	}

	sources = append(sources, httpSource)
	log.Printf("✅ Started HTTP webhook source on port 8080")

	return sources, nil
}

// FactProcessor handles incoming facts from all sources
type FactProcessor struct {
	metrics *ProcessorMetrics
}

func NewFactProcessor() *FactProcessor {
	return &FactProcessor{
		metrics: &ProcessorMetrics{},
	}
}

func (fp *FactProcessor) ProcessFacts(ctx context.Context, sources []adapters.FactSource) {
	for _, source := range sources {
		go fp.processSourceFacts(ctx, source)
	}
}

func (fp *FactProcessor) processSourceFacts(ctx context.Context, source adapters.FactSource) {
	metadata := source.GetMetadata()
	log.Printf("📡 Subscribing to facts from %s (%s)", metadata.SourceID, metadata.SourceType)

	factChan, err := source.Subscribe(ctx, []string{})
	if err != nil {
		log.Printf("❌ Failed to subscribe to %s: %v", metadata.SourceID, err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case fact, ok := <-factChan:
			if !ok {
				log.Printf("📪 Fact channel closed for %s", metadata.SourceID)
				return
			}

			if err := fp.processSingleFact(fact); err != nil {
				log.Printf("❌ Error processing fact from %s: %v", fact.SourceID, err)
				fp.metrics.RecordError(fact.SourceID, err)
			} else {
				fp.metrics.RecordFactProcessed(fact.SourceID, fact.SchemaName)
			}
		}
	}
}

func (fp *FactProcessor) processSingleFact(fact *adapters.TypedFact) error {
	log.Printf("📦 Processing fact: %s from source: %s", fact.SchemaName, fact.SourceID)
	log.Printf("   📊 Metadata: %v", fact.Metadata)
	log.Printf("   🕐 Timestamp: %s", fact.Timestamp.Format(time.RFC3339))

	if fact.TraceID != "" {
		log.Printf("   🔍 Trace ID: %s", fact.TraceID)
	}

	return fp.sendToEffectusEngine(fact)
}

func (fp *FactProcessor) sendToEffectusEngine(fact *adapters.TypedFact) error {
	log.Printf("🎯 Sending fact %s to Effectus rule engine", fact.SchemaName)

	// Simulate processing time
	time.Sleep(10 * time.Millisecond)

	// Simulate rule execution results
	effectsGenerated := simulateRuleExecution(fact)
	if len(effectsGenerated) > 0 {
		log.Printf("⚡ Generated %d effects from fact %s", len(effectsGenerated), fact.SchemaName)
		for _, effect := range effectsGenerated {
			log.Printf("   📤 Effect: %s", effect)
		}
	}

	return nil
}

func simulateRuleExecution(fact *adapters.TypedFact) []string {
	switch fact.SchemaName {
	case "acme.v1.facts.UserProfile":
		return []string{"send_welcome_email", "create_user_dashboard"}
	case "acme.v1.facts.OrderEvent":
		return []string{"send_order_confirmation", "update_inventory"}
	case "http.webhook.event":
		return []string{"log_webhook_received"}
	default:
		return []string{}
	}
}

type ProcessorMetrics struct {
	factsProcessed map[string]int
	errors         map[string]int
}

func (pm *ProcessorMetrics) RecordFactProcessed(sourceID, factType string) {
	if pm.factsProcessed == nil {
		pm.factsProcessed = make(map[string]int)
	}
	key := sourceID + ":" + factType
	pm.factsProcessed[key]++

	total := 0
	for _, count := range pm.factsProcessed {
		total += count
	}

	if total%5 == 0 {
		log.Printf("📈 Processed %d total facts", total)
	}
}

func (pm *ProcessorMetrics) RecordError(sourceID string, err error) {
	if pm.errors == nil {
		pm.errors = make(map[string]int)
	}
	pm.errors[sourceID]++
	log.Printf("📉 Total errors for %s: %d", sourceID, pm.errors[sourceID])
}

func showExampleUsage() {
	go func() {
		time.Sleep(2 * time.Second)

		log.Println("💡 Test the adapter with these webhook calls:")
		log.Println("")
		log.Println("# User created event:")
		log.Println("curl -X POST http://localhost:8080/webhook/events \\")
		log.Println("  -H 'Content-Type: application/json' \\")
		log.Println("  -H 'X-API-Key: demo-api-key-123' \\")
		log.Println("  -d '{\"event_type\": \"user.created\", \"user\": {\"id\": \"123\", \"email\": \"test@example.com\"}}'")
		log.Println("")
		log.Println("# Order completed event:")
		log.Println("curl -X POST http://localhost:8080/webhook/events \\")
		log.Println("  -H 'Content-Type: application/json' \\")
		log.Println("  -H 'X-API-Key: demo-api-key-123' \\")
		log.Println("  -d '{\"event_type\": \"order.completed\", \"order\": {\"id\": \"456\", \"amount\": 99.99}}'")
		log.Println("")
	}()
}
