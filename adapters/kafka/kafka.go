package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
	"google.golang.org/protobuf/proto"

	"github.com/effectus/effectus-go/adapters"
)

// KafkaSource implements the FactSource interface for Kafka
type KafkaSource struct {
	config    *Config
	reader    *kafka.Reader
	factChan  chan *adapters.TypedFact
	converter *MessageConverter
	metrics   adapters.SourceMetrics
	stopChan  chan struct{}
	started   bool
}

// Config holds Kafka source configuration
type Config struct {
	SourceID      string            `json:"source_id" yaml:"source_id"`
	Brokers       []string          `json:"brokers" yaml:"brokers"`
	Topic         string            `json:"topic" yaml:"topic"`
	ConsumerGroup string            `json:"consumer_group" yaml:"consumer_group"`
	SchemaFormat  string            `json:"schema_format" yaml:"schema_format"` // "protobuf", "json", "avro"
	StartOffset   string            `json:"start_offset" yaml:"start_offset"`   // "earliest", "latest"
	BatchSize     int               `json:"batch_size" yaml:"batch_size"`
	BatchTimeout  time.Duration     `json:"batch_timeout" yaml:"batch_timeout"`
	FactMappings  map[string]string `json:"fact_mappings" yaml:"fact_mappings"` // kafka subject -> effectus type
	Headers       map[string]string `json:"headers" yaml:"headers"`             // expected headers
}

// MessageConverter converts Kafka messages to TypedFacts
type MessageConverter struct {
	config       *Config
	factMappings map[string]string
}

// NewKafkaSource creates a new Kafka fact source
func NewKafkaSource(config *Config) (*KafkaSource, error) {
	if err := validateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Set defaults
	if config.BatchSize == 0 {
		config.BatchSize = 100
	}
	if config.BatchTimeout == 0 {
		config.BatchTimeout = 1 * time.Second
	}
	if config.StartOffset == "" {
		config.StartOffset = "latest"
	}

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  config.Brokers,
		Topic:    config.Topic,
		GroupID:  config.ConsumerGroup,
		MinBytes: 1e3,  // 1KB
		MaxBytes: 10e6, // 10MB
	})

	converter := &MessageConverter{
		config:       config,
		factMappings: config.FactMappings,
	}

	return &KafkaSource{
		config:    config,
		reader:    reader,
		factChan:  make(chan *adapters.TypedFact, config.BatchSize*2),
		converter: converter,
		metrics:   adapters.GetGlobalMetrics(),
		stopChan:  make(chan struct{}),
	}, nil
}

func validateConfig(config *Config) error {
	if config == nil {
		return fmt.Errorf("config is nil")
	}
	if len(config.Brokers) == 0 {
		return fmt.Errorf("brokers list is empty")
	}
	if config.Topic == "" {
		return fmt.Errorf("topic is required")
	}
	if config.ConsumerGroup == "" {
		return fmt.Errorf("consumer group is required")
	}
	if config.SourceID == "" {
		return fmt.Errorf("source ID is required")
	}
	return nil
}

// Start implements FactSource.Start
func (k *KafkaSource) Start(ctx context.Context) error {
	if k.started {
		return fmt.Errorf("source already started")
	}

	// Start message consumption goroutine
	go k.consumeMessages(ctx)

	k.started = true
	log.Printf("Kafka source %s started for topic %s", k.config.SourceID, k.config.Topic)
	return nil
}

// Stop implements FactSource.Stop
func (k *KafkaSource) Stop(ctx context.Context) error {
	if !k.started {
		return nil
	}

	close(k.stopChan)

	if err := k.reader.Close(); err != nil {
		return fmt.Errorf("failed to close kafka reader: %w", err)
	}

	close(k.factChan)
	k.started = false

	log.Printf("Kafka source %s stopped", k.config.SourceID)
	return nil
}

// Subscribe implements FactSource.Subscribe
func (k *KafkaSource) Subscribe(ctx context.Context, factTypes []string) (<-chan *adapters.TypedFact, error) {
	if !k.started {
		return nil, fmt.Errorf("source not started")
	}

	// For Kafka, we return the main fact channel
	// In a more sophisticated implementation, we could filter by fact types
	return k.factChan, nil
}

// GetSourceSchema implements FactSource.GetSourceSchema
func (k *KafkaSource) GetSourceSchema() *adapters.Schema {
	return &adapters.Schema{
		Name:    fmt.Sprintf("kafka_%s", k.config.Topic),
		Version: "v1.0.0",
		Fields: map[string]interface{}{
			"topic":     "string",
			"partition": "int32",
			"offset":    "int64",
			"key":       "bytes",
			"value":     "bytes",
			"headers":   "map[string]string",
		},
	}
}

// HealthCheck implements FactSource.HealthCheck
func (k *KafkaSource) HealthCheck() error {
	// Try to get metadata from Kafka
	conn, err := kafka.Dial("tcp", k.config.Brokers[0])
	if err != nil {
		return fmt.Errorf("failed to connect to kafka: %w", err)
	}
	defer conn.Close()

	_, err = conn.ReadPartitions()
	if err != nil {
		return fmt.Errorf("failed to read partitions: %w", err)
	}

	k.metrics.RecordHealthCheck(k.config.SourceID, true)
	return nil
}

// GetMetadata implements FactSource.GetMetadata
func (k *KafkaSource) GetMetadata() adapters.SourceMetadata {
	return adapters.SourceMetadata{
		SourceID:      k.config.SourceID,
		SourceType:    "kafka",
		Version:       "v1.0.0",
		Capabilities:  []string{"streaming", "realtime", "backfill"},
		SchemaFormats: []string{"protobuf", "json", "avro"},
		Config: map[string]string{
			"topic":          k.config.Topic,
			"consumer_group": k.config.ConsumerGroup,
			"schema_format":  k.config.SchemaFormat,
		},
		Tags: []string{"messaging", "streaming"},
	}
}

// consumeMessages runs the main message consumption loop
func (k *KafkaSource) consumeMessages(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Kafka consumer panic: %v", r)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-k.stopChan:
			return
		default:
			// Read message with timeout
			msg, err := k.reader.ReadMessage(ctx)
			if err != nil {
				k.metrics.RecordError(k.config.SourceID, "read_message", err)
				log.Printf("Error reading Kafka message: %v", err)
				time.Sleep(1 * time.Second) // Back off on error
				continue
			}

			// Convert to TypedFact
			start := time.Now()
			fact, err := k.converter.ConvertMessage(msg)
			if err != nil {
				k.metrics.RecordError(k.config.SourceID, "convert_message", err)
				log.Printf("Error converting Kafka message: %v", err)
				continue
			}

			if fact != nil {
				// Send to fact channel (non-blocking)
				select {
				case k.factChan <- fact:
					k.metrics.RecordFactProcessed(k.config.SourceID, fact.SchemaName)
					k.metrics.RecordLatency(k.config.SourceID, time.Since(start))
				default:
					k.metrics.RecordError(k.config.SourceID, "channel_full", fmt.Errorf("fact channel full"))
					log.Printf("Fact channel full, dropping message")
				}
			}
		}
	}
}

// ConvertMessage converts a Kafka message to a TypedFact
func (c *MessageConverter) ConvertMessage(msg kafka.Message) (*adapters.TypedFact, error) {
	// Extract metadata from headers
	headers := make(map[string]string)
	var schemaID string
	var schemaVersion string
	var traceID string

	for _, header := range msg.Headers {
		key := string(header.Key)
		value := string(header.Value)
		headers[key] = value

		switch key {
		case "schema-id", "schema_id":
			schemaID = value
		case "schema-version", "schema_version":
			schemaVersion = value
		case "trace-id", "trace_id", "x-trace-id":
			traceID = value
		}
	}

	// Determine fact type from schema ID or topic
	var factType string
	if schemaID != "" {
		if mapped, exists := c.factMappings[schemaID]; exists {
			factType = mapped
		} else {
			return nil, fmt.Errorf("unknown schema ID: %s", schemaID)
		}
	} else {
		// Fallback to topic-based mapping
		if mapped, exists := c.factMappings[msg.Topic]; exists {
			factType = mapped
		} else {
			return nil, fmt.Errorf("no mapping for topic: %s", msg.Topic)
		}
	}

	// Convert message based on format
	var protoMsg proto.Message
	var err error

	switch c.config.SchemaFormat {
	case "json":
		protoMsg, err = c.convertJSONMessage(msg.Value, factType)
	case "protobuf":
		protoMsg, err = c.convertProtobufMessage(msg.Value, factType)
	case "avro":
		return nil, fmt.Errorf("avro format not yet supported")
	default:
		return nil, fmt.Errorf("unsupported schema format: %s", c.config.SchemaFormat)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to convert message: %w", err)
	}

	// Create TypedFact
	fact := &adapters.TypedFact{
		SchemaName:    factType,
		SchemaVersion: schemaVersion,
		Data:          protoMsg,
		RawData:       msg.Value,
		Timestamp:     msg.Time,
		SourceID:      c.config.SourceID,
		TraceID:       traceID,
		Metadata: map[string]string{
			"kafka.topic":     msg.Topic,
			"kafka.partition": fmt.Sprintf("%d", msg.Partition),
			"kafka.offset":    fmt.Sprintf("%d", msg.Offset),
			"kafka.key":       string(msg.Key),
		},
	}

	// Add custom headers to metadata
	for k, v := range headers {
		if !strings.HasPrefix(k, "kafka.") {
			fact.Metadata[k] = v
		}
	}

	return fact, nil
}

// convertJSONMessage converts JSON message to proto message
func (c *MessageConverter) convertJSONMessage(data []byte, factType string) (proto.Message, error) {
	// Parse JSON
	var jsonData map[string]interface{}
	if err := json.Unmarshal(data, &jsonData); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Convert to proto based on fact type
	// This is a simplified conversion - in practice you'd use reflection
	// or code generation based on the proto schemas
	switch factType {
	case "acme.v1.facts.UserProfile":
		return c.convertToUserProfile(jsonData)
	case "acme.v1.facts.SystemEvent":
		return c.convertToSystemEvent(jsonData)
	default:
		// Generic proto message - would need proper schema-driven conversion
		return nil, fmt.Errorf("unsupported fact type for JSON conversion: %s", factType)
	}
}

// convertProtobufMessage converts protobuf message
func (c *MessageConverter) convertProtobufMessage(data []byte, factType string) (proto.Message, error) {
	// This would use proto.Unmarshal with the appropriate message type
	// For now, return an error since we don't have the generated types
	return nil, fmt.Errorf("protobuf conversion not implemented for fact type: %s", factType)
}

// Helper conversion functions (simplified examples)
func (c *MessageConverter) convertToUserProfile(data map[string]interface{}) (proto.Message, error) {
	// This would create an actual UserProfile proto message
	// For now, return nil as we don't have the generated types
	return nil, fmt.Errorf("UserProfile conversion not implemented")
}

func (c *MessageConverter) convertToSystemEvent(data map[string]interface{}) (proto.Message, error) {
	// This would create an actual SystemEvent proto message
	// For now, return nil as we don't have the generated types
	return nil, fmt.Errorf("SystemEvent conversion not implemented")
}

// Factory for Kafka sources
type Factory struct{}

func (f *Factory) Create(config adapters.SourceConfig) (adapters.FactSource, error) {
	kafkaConfig := &Config{
		SourceID: config.SourceID,
	}

	// Extract Kafka-specific configuration
	if brokers, ok := config.Config["brokers"].([]interface{}); ok {
		for _, broker := range brokers {
			if b, ok := broker.(string); ok {
				kafkaConfig.Brokers = append(kafkaConfig.Brokers, b)
			}
		}
	}

	if topic, ok := config.Config["topic"].(string); ok {
		kafkaConfig.Topic = topic
	}

	if group, ok := config.Config["consumer_group"].(string); ok {
		kafkaConfig.ConsumerGroup = group
	}

	if format, ok := config.Config["schema_format"].(string); ok {
		kafkaConfig.SchemaFormat = format
	}

	// Convert mappings
	kafkaConfig.FactMappings = make(map[string]string)
	for _, mapping := range config.Mappings {
		kafkaConfig.FactMappings[mapping.SourceKey] = mapping.EffectusType
	}

	return NewKafkaSource(kafkaConfig)
}

func (f *Factory) ValidateConfig(config adapters.SourceConfig) error {
	// Validate required fields
	if _, ok := config.Config["brokers"]; !ok {
		return fmt.Errorf("brokers is required")
	}
	if _, ok := config.Config["topic"]; !ok {
		return fmt.Errorf("topic is required")
	}
	if _, ok := config.Config["consumer_group"]; !ok {
		return fmt.Errorf("consumer_group is required")
	}
	return nil
}

func (f *Factory) GetConfigSchema() adapters.ConfigSchema {
	return adapters.ConfigSchema{
		Properties: map[string]adapters.ConfigProperty{
			"brokers": {
				Type:        "array",
				Description: "List of Kafka broker addresses",
				Examples:    []string{"[\"localhost:9092\"]", "[\"kafka1:9092\", \"kafka2:9092\"]"},
			},
			"topic": {
				Type:        "string",
				Description: "Kafka topic to consume from",
				Examples:    []string{"user.events", "system.logs"},
			},
			"consumer_group": {
				Type:        "string",
				Description: "Kafka consumer group ID",
				Examples:    []string{"effectus_consumers", "my_app_group"},
			},
			"schema_format": {
				Type:        "string",
				Description: "Message format",
				Default:     "json",
				Examples:    []string{"json", "protobuf", "avro"},
			},
			"start_offset": {
				Type:        "string",
				Description: "Where to start consuming",
				Default:     "latest",
				Examples:    []string{"earliest", "latest"},
			},
		},
		Required: []string{"brokers", "topic", "consumer_group"},
	}
}

// Register the Kafka source factory
func init() {
	adapters.RegisterSourceType("kafka", &Factory{})
}
