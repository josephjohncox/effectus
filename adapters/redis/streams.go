package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"

	"github.com/effectus/effectus-go/adapters"
)

// RedisStreamsSource consumes events from Redis Streams
type RedisStreamsSource struct {
	sourceID      string
	sourceType    string
	redisAddr     string
	redisDB       int
	password      string
	streams       []string
	consumerGroup string
	consumerName  string
	batchSize     int64
	blockTime     time.Duration
	schemaName    string

	client *redis.Client
	ctx    context.Context
	cancel context.CancelFunc
	schema *adapters.Schema
}

// StreamsConfig holds configuration for Redis Streams
type StreamsConfig struct {
	RedisAddr     string        `json:"redis_addr" yaml:"redis_addr"`
	RedisDB       int           `json:"redis_db" yaml:"redis_db"`
	Password      string        `json:"password" yaml:"password"`
	Streams       []string      `json:"streams" yaml:"streams"`
	ConsumerGroup string        `json:"consumer_group" yaml:"consumer_group"`
	ConsumerName  string        `json:"consumer_name" yaml:"consumer_name"`
	BatchSize     int64         `json:"batch_size" yaml:"batch_size"`
	BlockTime     time.Duration `json:"block_time" yaml:"block_time"`
	SchemaName    string        `json:"schema_name" yaml:"schema_name"`
}

// NewRedisStreamsSource creates a new Redis Streams source
func NewRedisStreamsSource(sourceID string, config StreamsConfig) (*RedisStreamsSource, error) {
	if config.RedisAddr == "" {
		config.RedisAddr = "localhost:6379"
	}
	if len(config.Streams) == 0 {
		return nil, fmt.Errorf("at least one stream is required")
	}
	if config.ConsumerGroup == "" {
		config.ConsumerGroup = fmt.Sprintf("effectus_%s", sourceID)
	}
	if config.ConsumerName == "" {
		config.ConsumerName = fmt.Sprintf("consumer_%s", sourceID)
	}
	if config.BatchSize == 0 {
		config.BatchSize = 100
	}
	if config.BlockTime == 0 {
		config.BlockTime = 1 * time.Second
	}
	if config.SchemaName == "" {
		config.SchemaName = "redis_stream_event"
	}

	ctx, cancel := context.WithCancel(context.Background())

	source := &RedisStreamsSource{
		sourceID:      sourceID,
		sourceType:    "redis_streams",
		redisAddr:     config.RedisAddr,
		redisDB:       config.RedisDB,
		password:      config.Password,
		streams:       config.Streams,
		consumerGroup: config.ConsumerGroup,
		consumerName:  config.ConsumerName,
		batchSize:     config.BatchSize,
		blockTime:     config.BlockTime,
		schemaName:    config.SchemaName,
		ctx:           ctx,
		cancel:        cancel,
		schema: &adapters.Schema{
			Name:    config.SchemaName,
			Version: "v1.0.0",
			Fields: map[string]interface{}{
				"stream":    "string",
				"id":        "string",
				"timestamp": "datetime",
				"fields":    "object",
			},
		},
	}

	return source, nil
}

func (r *RedisStreamsSource) Subscribe(ctx context.Context, factTypes []string) (<-chan *adapters.TypedFact, error) {
	factChan := make(chan *adapters.TypedFact, 100)

	// Start consuming in background
	go func() {
		defer close(factChan)

		if err := r.consumeStreams(factChan); err != nil {
			log.Printf("Stream consumption failed: %v", err)
		}
	}()

	return factChan, nil
}

func (r *RedisStreamsSource) Start(ctx context.Context) error {
	// Create Redis client
	r.client = redis.NewClient(&redis.Options{
		Addr:     r.redisAddr,
		Password: r.password,
		DB:       r.redisDB,
	})

	// Test connection
	if err := r.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}

	// Create consumer groups
	for _, stream := range r.streams {
		if err := r.createConsumerGroup(stream); err != nil {
			log.Printf("Warning: failed to create consumer group for stream %s: %v", stream, err)
		}
	}

	log.Printf("Redis Streams source started, streams: %v", r.streams)
	return nil
}

func (r *RedisStreamsSource) Stop(ctx context.Context) error {
	r.cancel()

	if r.client != nil {
		if err := r.client.Close(); err != nil {
			return fmt.Errorf("failed to close Redis client: %w", err)
		}
	}

	log.Printf("Redis Streams source stopped")
	return nil
}

func (r *RedisStreamsSource) GetSourceSchema() *adapters.Schema {
	return r.schema
}

func (r *RedisStreamsSource) HealthCheck() error {
	if r.client == nil {
		return fmt.Errorf("Redis client not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return r.client.Ping(ctx).Err()
}

func (r *RedisStreamsSource) GetMetadata() adapters.SourceMetadata {
	return adapters.SourceMetadata{
		SourceID:      r.sourceID,
		SourceType:    r.sourceType,
		Version:       "1.0.0",
		Capabilities:  []string{"streaming", "realtime", "consumer_groups"},
		SchemaFormats: []string{"json"},
		Config: map[string]string{
			"redis_addr":     r.redisAddr,
			"streams":        strings.Join(r.streams, ","),
			"consumer_group": r.consumerGroup,
		},
		Tags: []string{"redis", "streaming"},
	}
}

func (r *RedisStreamsSource) createConsumerGroup(stream string) error {
	// Try to create consumer group (ignore if already exists)
	_, err := r.client.XGroupCreate(r.ctx, stream, r.consumerGroup, "0").Result()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("failed to create consumer group: %w", err)
	}
	return nil
}

func (r *RedisStreamsSource) consumeStreams(factChan chan<- *adapters.TypedFact) error {
	// Build stream arguments for XREADGROUP
	streams := make([]string, len(r.streams)*2)
	for i, stream := range r.streams {
		streams[i] = stream
		streams[len(r.streams)+i] = ">"
	}

	for {
		select {
		case <-r.ctx.Done():
			return nil
		default:
			// Read from streams
			result, err := r.client.XReadGroup(r.ctx, &redis.XReadGroupArgs{
				Group:    r.consumerGroup,
				Consumer: r.consumerName,
				Streams:  streams,
				Count:    r.batchSize,
				Block:    r.blockTime,
			}).Result()

			if err != nil {
				if err == redis.Nil {
					// No messages, continue
					continue
				}
				log.Printf("Failed to read from streams: %v", err)
				time.Sleep(1 * time.Second)
				continue
			}

			// Process messages
			for _, stream := range result {
				for _, message := range stream.Messages {
					fact, err := r.transformMessage(stream.Stream, message)
					if err != nil {
						log.Printf("Failed to transform message: %v", err)
						continue
					}

					select {
					case factChan <- fact:
						// Acknowledge message
						r.client.XAck(r.ctx, stream.Stream, r.consumerGroup, message.ID)
					case <-r.ctx.Done():
						return nil
					default:
						log.Printf("Fact channel full, dropping message")
					}
				}
			}
		}
	}
}

func (r *RedisStreamsSource) transformMessage(streamName string, message redis.XMessage) (*adapters.TypedFact, error) {
	// Create event data
	eventData := map[string]interface{}{
		"stream":    streamName,
		"id":        message.ID,
		"timestamp": time.Now(),
		"fields":    message.Values,
	}

	// Serialize event data
	data, err := json.Marshal(eventData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event data: %w", err)
	}

	return &adapters.TypedFact{
		SchemaName:    r.schemaName,
		SchemaVersion: "v1.0.0",
		Data:          nil, // Would contain proto message in real implementation
		RawData:       data,
		Timestamp:     time.Now(),
		SourceID:      r.sourceID,
		TraceID:       "",
		Metadata: map[string]string{
			"redis.stream":         streamName,
			"redis.message_id":     message.ID,
			"redis.consumer_group": r.consumerGroup,
			"redis.consumer_name":  r.consumerName,
			"source_type":          "redis_streams",
		},
	}, nil
}

// RedisStreamsFactory creates Redis Streams sources
type RedisStreamsFactory struct{}

func (f *RedisStreamsFactory) Create(config adapters.SourceConfig) (adapters.FactSource, error) {
	streamsConfig := StreamsConfig{}

	// Extract configuration
	if redisAddr, ok := config.Config["redis_addr"].(string); ok {
		streamsConfig.RedisAddr = redisAddr
	}
	if redisDB, ok := config.Config["redis_db"].(float64); ok {
		streamsConfig.RedisDB = int(redisDB)
	}
	if password, ok := config.Config["password"].(string); ok {
		streamsConfig.Password = password
	}
	if streams, ok := config.Config["streams"].([]interface{}); ok {
		streamsConfig.Streams = make([]string, len(streams))
		for i, stream := range streams {
			if streamStr, ok := stream.(string); ok {
				streamsConfig.Streams[i] = streamStr
			}
		}
	}
	if consumerGroup, ok := config.Config["consumer_group"].(string); ok {
		streamsConfig.ConsumerGroup = consumerGroup
	}
	if consumerName, ok := config.Config["consumer_name"].(string); ok {
		streamsConfig.ConsumerName = consumerName
	}
	if batchSize, ok := config.Config["batch_size"].(float64); ok {
		streamsConfig.BatchSize = int64(batchSize)
	}
	if blockTime, ok := config.Config["block_time"].(string); ok {
		if duration, err := time.ParseDuration(blockTime); err == nil {
			streamsConfig.BlockTime = duration
		}
	}
	if schemaName, ok := config.Config["schema_name"].(string); ok {
		streamsConfig.SchemaName = schemaName
	}

	return NewRedisStreamsSource(config.SourceID, streamsConfig)
}

func (f *RedisStreamsFactory) ValidateConfig(config adapters.SourceConfig) error {
	if streams, ok := config.Config["streams"].([]interface{}); !ok || len(streams) == 0 {
		return fmt.Errorf("at least one stream is required for redis_streams source")
	}
	return nil
}

func (f *RedisStreamsFactory) GetConfigSchema() adapters.ConfigSchema {
	return adapters.ConfigSchema{
		Properties: map[string]adapters.ConfigProperty{
			"redis_addr": {
				Type:        "string",
				Description: "Redis server address",
				Default:     "localhost:6379",
				Examples:    []string{"localhost:6379", "redis.example.com:6379"},
			},
			"redis_db": {
				Type:        "int",
				Description: "Redis database number",
				Default:     0,
			},
			"password": {
				Type:        "string",
				Description: "Redis password (optional)",
			},
			"streams": {
				Type:        "array",
				Description: "List of Redis streams to consume",
				Examples:    []string{`["events", "notifications", "logs"]`},
			},
			"consumer_group": {
				Type:        "string",
				Description: "Consumer group name (auto-generated if not provided)",
			},
			"consumer_name": {
				Type:        "string",
				Description: "Consumer name (auto-generated if not provided)",
			},
			"batch_size": {
				Type:        "int",
				Description: "Number of messages to read per batch",
				Default:     100,
			},
			"block_time": {
				Type:        "string",
				Description: "Time to block waiting for messages",
				Default:     "1s",
				Examples:    []string{"1s", "5s", "100ms"},
			},
			"schema_name": {
				Type:        "string",
				Description: "Schema name for generated facts",
				Default:     "redis_stream_event",
			},
		},
		Required: []string{"streams"},
	}
}

func init() {
	adapters.RegisterSourceType("redis_streams", &RedisStreamsFactory{})
}
