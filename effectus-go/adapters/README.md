# Effectus Source Adapters Library

A pluggable library for ingesting facts from multiple data sources into Effectus with type safety and schema validation.

## **Overview**

The Effectus adapters library provides a unified interface for connecting various data sources to Effectus:

- **Kafka Streams** - High-throughput message streaming
- **HTTP Webhooks** - Real-time API integration  
- **Database Polling** - Poll database tables at regular intervals
- **Redis Streams** - Real-time event streaming from Redis
- **File System Watcher** - Monitor directories for file changes
- **Message Queues** - AMQP, Redis, etc.

All adapters convert heterogeneous data formats into strongly-typed protobuf facts for Effectus processing.

## **Quick Start**

### Installation

```bash
go get github.com/effectus/effectus-go/adapters
```

### Basic Usage

```go
package main

import (
    "context"
    "log"
    
    "github.com/effectus/effectus-go/adapters"
    _ "github.com/effectus/effectus-go/adapters/http"  // Auto-registers HTTP adapter
    _ "github.com/effectus/effectus-go/adapters/kafka" // Auto-registers Kafka adapter
)

func main() {
    ctx := context.Background()
    
    // Create HTTP webhook source
    httpConfig := adapters.SourceConfig{
        SourceID: "api_webhooks",
        Type:     "http",
        Config: map[string]interface{}{
            "listen_port": 8080,
            "path":        "/webhook/events",
            "auth_method": "bearer_token",
        },
        Mappings: []adapters.FactMapping{
            {
                SourceKey:     "user.created",
                EffectusType:  "acme.v1.facts.UserProfile",
                SchemaVersion: "v1.0.0",
            },
        },
    }
    
    source, err := adapters.CreateSource(httpConfig)
    if err != nil {
        log.Fatal(err)
    }
    
    // Start the source
    if err := source.Start(ctx); err != nil {
        log.Fatal(err)
    }
    defer source.Stop(ctx)
    
    // Subscribe to facts
    factChan, err := source.Subscribe(ctx, []string{"acme.v1.facts.UserProfile"})
    if err != nil {
        log.Fatal(err)
    }
    
    // Process facts
    for fact := range factChan {
        log.Printf("Received fact: %s from %s", fact.SchemaName, fact.SourceID)
        // Send to Effectus rule engine
        processFactWithEffectus(fact)
    }
}
```

## **Supported Sources**

### HTTP Webhooks

Listen for HTTP POST requests and convert them to typed facts.

```yaml
source_id: "api_webhooks"
type: "http"
config:
  listen_port: 8080
  path: "/webhook/events"
  auth_method: "bearer_token"
  auth_config:
    token: "your-secret-token"
transforms:
  - source_path: "$.user"
    target_type: "acme.v1.facts.UserProfile"
    mapping:
      user_id: "$.id"
      email: "$.email_address"
```

**Features:**
- Multiple authentication methods (Bearer token, API key, none)
- CORS support
- JSON/XML/Form data support
- Request transformation and validation

### PostgreSQL Database Poller

Poll PostgreSQL database tables at regular intervals with incremental support.

```yaml
source_id: "user_database"
type: "postgres_poller"
config:
  connection_string: "postgres://user:pass@localhost:5432/db"
  query: "SELECT id, name, email, created_at FROM users"
  interval_seconds: 60
  timestamp_column: "created_at"
  max_rows: 1000
  schema_name: "user_profile"
```

**Features:**
- Incremental polling using timestamp columns
- Configurable polling intervals
- Row limit controls
- Automatic connection management
- JSON serialization of database rows

### Redis Streams

Consume real-time events from Redis Streams with consumer group support.

```yaml
source_id: "redis_events"
type: "redis_streams"
config:
  redis_addr: "localhost:6379"
  redis_db: 0
  streams: ["events", "notifications", "logs"]
  consumer_group: "effectus_group"
  consumer_name: "consumer_1"
  batch_size: 100
  block_time: "1s"
```

**Features:**
- Consumer group coordination
- Automatic message acknowledgment
- Configurable batch processing
- Persistent stream consumption
- Connection pooling and retry logic

### File System Watcher

Monitor directories for file changes with pattern matching and content reading.

```yaml
source_id: "config_watcher"
type: "file_watcher"
config:
  paths: ["/etc/config", "./data"]
  patterns: ["*.json", "*.yaml"]
  events: ["CREATE", "WRITE", "REMOVE"]
  recursive: true
  max_file_size: 10485760  # 10MB
```

**Features:**
- Real-time file system monitoring
- Pattern-based file filtering
- Recursive directory watching
- Content extraction for small files
- Cross-platform compatibility

## **Configuration**

### YAML Configuration

```yaml
# config.yaml
sources:
  - source_id: "kafka_events"
    type: "kafka"
    config:
      brokers: ["localhost:9092"]
      topic: "events"
      consumer_group: "effectus"
    mappings:
      - source_key: "user.created"
        effectus_type: "acme.v1.facts.UserProfile"
        schema_version: "v1.0.0"
        
  - source_id: "api_webhooks"
    type: "http"
    config:
      listen_port: 8081
      path: "/hooks"
      auth_method: "api_key"
      auth_config:
        token_header: "X-API-Key"
        expected_token: "${API_KEY}"
```

### Programmatic Configuration

```go
// Create multiple sources
configs := []adapters.SourceConfig{
    {
        SourceID: "kafka_events",
        Type:     "kafka",
        Config: map[string]interface{}{
            "brokers":        []string{"localhost:9092"},
            "topic":          "events",
            "consumer_group": "effectus",
        },
    },
    {
        SourceID: "webhooks",
        Type:     "http",
        Config: map[string]interface{}{
            "listen_port": 8081,
            "path":        "/hooks",
        },
    },
}

// Start all sources
var sources []adapters.FactSource
for _, config := range configs {
    source, err := adapters.CreateSource(config)
    if err != nil {
        return err
    }
    
    if err := source.Start(ctx); err != nil {
        return err
    }
    
    sources = append(sources, source)
}
```

## **Schema Validation**

All sources validate incoming data against registered protobuf schemas:

```go
// Schema validation happens automatically
fact, err := source.Subscribe(ctx, []string{"acme.v1.facts.UserProfile"})
if err != nil {
    // Schema validation failed
    log.Printf("Schema error: %v", err)
}

// Facts are guaranteed to be valid proto messages
userProfile := fact.Data.(*acme_v1.UserProfile)
```

## **Custom Adapters**

Create custom adapters by implementing the `FactSource` interface:

```go
package custom

import (
    "context"
    "github.com/effectus/effectus-go/adapters"
)

type CustomSource struct {
    config *Config
    // ... other fields
}

func (c *CustomSource) Start(ctx context.Context) error {
    // Implementation
}

func (c *CustomSource) Subscribe(ctx context.Context, factTypes []string) (<-chan *adapters.TypedFact, error) {
    // Implementation
}

// ... implement other FactSource methods

// Register the custom adapter
func init() {
    adapters.RegisterSourceType("custom", &CustomFactory{})
}
```

## **Observability**

The library provides comprehensive metrics and logging:

```go
// Custom metrics implementation
type PrometheusMetrics struct {
    factsProcessed *prometheus.CounterVec
    processingTime *prometheus.HistogramVec
    errors         *prometheus.CounterVec
}

func (p *PrometheusMetrics) RecordFactProcessed(sourceID, factType string) {
    p.factsProcessed.WithLabelValues(sourceID, factType).Inc()
}

// Set global metrics
adapters.SetGlobalMetrics(&PrometheusMetrics{
    // ... initialize metrics
})
```

## **Error Handling**

All sources provide structured error handling:

```go
for fact := range factChan {
    if err := processFactWithEffectus(fact); err != nil {
        // Log error with source context
        log.Printf("Processing failed for fact from %s: %v", fact.SourceID, err)
        
        // Optionally implement retry logic
        retryFact(fact)
    }
}
```

## **Available Source Types**

| Type | Description | Status |
|------|-------------|--------|
| `http` | HTTP webhooks and REST APIs | ✅ Stable |
| `kafka` | Kafka message streaming | ✅ Stable |
| `postgres_poller` | PostgreSQL database polling | ✅ Stable |
| `redis_streams` | Redis streams and consumer groups | ✅ Stable |
| `file_watcher` | File system change monitoring | ✅ Stable |
| `postgres_cdc` | PostgreSQL change data capture | 📋 Planned |
| `mysql_cdc` | MySQL binlog streaming | 📋 Planned |
| `amqp` | RabbitMQ and AMQP | 📋 Planned |
| `grpc` | gRPC streaming | 📋 Planned |

## **Best Practices**

### Performance
- Use buffered channels with appropriate buffer sizes
- Implement backpressure handling for high-throughput sources
- Monitor memory usage for long-running sources

### Reliability
- Always implement proper error handling and retry logic
- Use structured logging with source identifiers
- Monitor source health with regular health checks

### Security
- Use strong authentication for webhook endpoints
- Validate all incoming data against schemas
- Implement rate limiting for public endpoints

### Observability
- Export metrics for all sources
- Include tracing headers for distributed systems
- Log important events with structured data

This library transforms Effectus into a **universal fact ingestion platform** while maintaining type safety and operational excellence. 