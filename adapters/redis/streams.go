package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"

	"github.com/effectus/effectus-go/adapters"
)

// RedisStreamsSource consumes events from Redis Streams.
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
	claimMinIdle  time.Duration
	claimInterval time.Duration
	schemaName    string

	client  redis.UniversalClient
	xack    func(context.Context, string, string, string) (int64, error)
	pending func(context.Context, string, string, string) (bool, error)
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	out     chan *adapters.TypedFact
	schema  *adapters.Schema

	mu      sync.Mutex
	running bool
}

// StreamsConfig holds configuration for Redis Streams.
type StreamsConfig struct {
	RedisAddr     string        `json:"redis_addr" yaml:"redis_addr"`
	RedisDB       int           `json:"redis_db" yaml:"redis_db"`
	Password      string        `json:"password" yaml:"password"`
	Streams       []string      `json:"streams" yaml:"streams"`
	ConsumerGroup string        `json:"consumer_group" yaml:"consumer_group"`
	ConsumerName  string        `json:"consumer_name" yaml:"consumer_name"`
	BatchSize     int64         `json:"batch_size" yaml:"batch_size"`
	BlockTime     time.Duration `json:"block_time" yaml:"block_time"`
	ClaimMinIdle  time.Duration `json:"claim_min_idle" yaml:"claim_min_idle"`
	ClaimInterval time.Duration `json:"claim_interval" yaml:"claim_interval"`
	SchemaName    string        `json:"schema_name" yaml:"schema_name"`
}

// NewRedisStreamsSource creates a new Redis Streams source.
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
	if config.BatchSize <= 0 {
		config.BatchSize = 100
	}
	if config.BlockTime <= 0 {
		config.BlockTime = time.Second
	}
	if config.ClaimMinIdle <= 0 {
		config.ClaimMinIdle = 30 * time.Second
	}
	if config.ClaimInterval <= 0 {
		config.ClaimInterval = config.ClaimMinIdle / 2
	}
	if config.ClaimInterval < 10*time.Millisecond {
		config.ClaimInterval = 10 * time.Millisecond
	}
	if config.SchemaName == "" {
		config.SchemaName = "redis_stream_event"
	}

	return &RedisStreamsSource{
		sourceID: sourceID, sourceType: "redis_streams", redisAddr: config.RedisAddr,
		redisDB: config.RedisDB, password: config.Password, streams: config.Streams,
		consumerGroup: config.ConsumerGroup, consumerName: config.ConsumerName,
		batchSize: config.BatchSize, blockTime: config.BlockTime,
		claimMinIdle: config.ClaimMinIdle, claimInterval: config.ClaimInterval,
		schemaName: config.SchemaName,
		schema: &adapters.Schema{Name: config.SchemaName, Version: "v1.0.0", Fields: map[string]interface{}{
			"stream": "string", "id": "string", "timestamp": "datetime", "fields": "object",
		}},
	}, nil
}

func (r *RedisStreamsSource) Subscribe(ctx context.Context, factTypes []string) (<-chan *adapters.TypedFact, error) {
	r.mu.Lock()
	if r.running {
		ch := r.out
		r.mu.Unlock()
		return ch, nil
	}
	needStart := r.client == nil
	r.mu.Unlock()
	if needStart {
		if err := r.Start(ctx); err != nil {
			return nil, err
		}
	}

	r.mu.Lock()
	if r.running {
		ch := r.out
		r.mu.Unlock()
		return ch, nil
	}
	r.ctx, r.cancel = context.WithCancel(ctx)
	r.out = make(chan *adapters.TypedFact, 100)
	r.done = make(chan struct{})
	r.running = true
	ch, done := r.out, r.done
	r.mu.Unlock()
	go r.consumeStreams(ch, done)
	return ch, nil
}

func (r *RedisStreamsSource) Start(ctx context.Context) error {
	r.mu.Lock()
	if r.client != nil {
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()

	client := redis.NewClient(&redis.Options{Addr: r.redisAddr, Password: r.password, DB: r.redisDB})
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}
	for _, stream := range r.streams {
		if err := createConsumerGroup(ctx, client, stream, r.consumerGroup); err != nil {
			client.Close()
			return err
		}
	}
	r.mu.Lock()
	if r.client != nil {
		r.mu.Unlock()
		client.Close()
		return nil
	}
	r.client = client
	r.mu.Unlock()
	log.Printf("Redis Streams source started, streams: %v", r.streams)
	return nil
}

func (r *RedisStreamsSource) Stop(ctx context.Context) error {
	r.mu.Lock()
	cancel, done, client := r.cancel, r.done, r.client
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if client != nil {
		_ = client.Close()
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	r.mu.Lock()
	if r.client == client {
		r.client = nil
	}
	r.mu.Unlock()
	log.Printf("Redis Streams source stopped")
	return nil
}

func (r *RedisStreamsSource) GetSourceSchema() *adapters.Schema { return r.schema }

func (r *RedisStreamsSource) HealthCheck() error {
	r.mu.Lock()
	client := r.client
	r.mu.Unlock()
	if client == nil {
		return fmt.Errorf("Redis client not initialized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return client.Ping(ctx).Err()
}

func (r *RedisStreamsSource) GetMetadata() adapters.SourceMetadata {
	return adapters.SourceMetadata{
		SourceID: r.sourceID, SourceType: r.sourceType, Version: "1.0.0",
		Capabilities:  []string{"streaming", "realtime", "consumer_groups", "explicit_ack"},
		SchemaFormats: []string{"json"},
		Config:        map[string]string{"redis_addr": r.redisAddr, "streams": strings.Join(r.streams, ","), "consumer_group": r.consumerGroup},
		Tags:          []string{"redis", "streaming"},
	}
}

func createConsumerGroup(ctx context.Context, client redis.UniversalClient, stream, group string) error {
	err := client.XGroupCreateMkStream(ctx, stream, group, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("create consumer group for %s: %w", stream, err)
	}
	return nil
}

func (r *RedisStreamsSource) consumeStreams(factChan chan *adapters.TypedFact, done chan struct{}) {
	defer close(done)
	defer close(factChan)
	defer func() { r.mu.Lock(); r.running = false; r.mu.Unlock() }()

	streams := make([]string, len(r.streams)*2)
	for i, stream := range r.streams {
		streams[i] = stream
		streams[len(r.streams)+i] = ">"
	}

	nextClaim := time.Time{} // Recover the PEL before reading new messages.
	for {
		if r.ctx.Err() != nil {
			return
		}
		if !time.Now().Before(nextClaim) {
			if err := r.recoverPending(r.ctx, factChan); err != nil {
				if r.ctx.Err() != nil {
					return
				}
				log.Printf("Redis pending recovery failed: %v", err)
			}
			nextClaim = time.Now().Add(r.claimInterval)
		}

		block := r.blockTime
		if untilClaim := time.Until(nextClaim); untilClaim > 0 && untilClaim < block {
			block = untilClaim
		}
		result, err := r.client.XReadGroup(r.ctx, &redis.XReadGroupArgs{
			Group: r.consumerGroup, Consumer: r.consumerName, Streams: streams,
			Count: r.batchSize, Block: block,
		}).Result()
		if err != nil {
			if err == redis.Nil {
				continue
			}
			if r.ctx.Err() != nil {
				return
			}
			log.Printf("Redis stream read failed: %v", err)
			select {
			case <-time.After(100 * time.Millisecond):
			case <-r.ctx.Done():
				return
			}
			continue
		}
		for _, stream := range result {
			for _, message := range stream.Messages {
				if err := r.deliver(r.ctx, stream.Stream, message, factChan); err != nil {
					if r.ctx.Err() != nil {
						return
					}
					log.Printf("Redis message %s delivery failed: %v", message.ID, err)
				}
			}
		}
	}
}

func (r *RedisStreamsSource) recoverPending(ctx context.Context, factChan chan<- *adapters.TypedFact) error {
	for _, stream := range r.streams {
		start := "0-0"
		for {
			messages, next, err := xAutoClaim(ctx, r.client, stream, r.consumerGroup, r.consumerName, r.claimMinIdle, start, r.batchSize)
			if err != nil {
				return fmt.Errorf("XAUTOCLAIM %s: %w", stream, err)
			}
			for _, message := range messages {
				if err := r.deliver(ctx, stream, message, factChan); err != nil {
					return err
				}
			}
			if next == "0-0" || next == start {
				break
			}
			start = next
		}
	}
	return nil
}

// xAutoClaim decodes both the Redis 6 two-element reply and the Redis 7
// three-element reply. go-redis/v8 only accepts the Redis 6 shape.
func xAutoClaim(ctx context.Context, client redis.UniversalClient, stream, group, consumer string, minIdle time.Duration, start string, count int64) ([]redis.XMessage, string, error) {
	reply, err := client.Do(ctx, "XAUTOCLAIM", stream, group, consumer, minIdle.Milliseconds(), start, "COUNT", count).Result()
	if err != nil {
		return nil, "", err
	}
	parts, ok := reply.([]interface{})
	if !ok || len(parts) < 2 {
		return nil, "", fmt.Errorf("unexpected reply type %T", reply)
	}
	next, err := redisReplyString(parts[0])
	if err != nil {
		return nil, "", err
	}
	rawMessages, ok := parts[1].([]interface{})
	if !ok {
		return nil, "", fmt.Errorf("unexpected messages type %T", parts[1])
	}
	messages := make([]redis.XMessage, 0, len(rawMessages))
	for _, raw := range rawMessages {
		messageParts, ok := raw.([]interface{})
		if !ok || len(messageParts) != 2 {
			return nil, "", fmt.Errorf("unexpected message reply %T", raw)
		}
		id, err := redisReplyString(messageParts[0])
		if err != nil {
			return nil, "", err
		}
		rawFields, ok := messageParts[1].([]interface{})
		if !ok || len(rawFields)%2 != 0 {
			return nil, "", fmt.Errorf("unexpected fields reply %T", messageParts[1])
		}
		values := make(map[string]interface{}, len(rawFields)/2)
		for i := 0; i < len(rawFields); i += 2 {
			key, err := redisReplyString(rawFields[i])
			if err != nil {
				return nil, "", err
			}
			value, err := redisReplyString(rawFields[i+1])
			if err != nil {
				return nil, "", err
			}
			values[key] = value
		}
		messages = append(messages, redis.XMessage{ID: id, Values: values})
	}
	return messages, next, nil
}

func redisReplyString(value interface{}) (string, error) {
	switch value := value.(type) {
	case string:
		return value, nil
	case []byte:
		return string(value), nil
	default:
		return "", fmt.Errorf("unexpected Redis string type %T", value)
	}
}

func (r *RedisStreamsSource) deliver(ctx context.Context, stream string, message redis.XMessage, factChan chan<- *adapters.TypedFact) error {
	fact, err := r.transformMessage(stream, message)
	if err != nil {
		return fmt.Errorf("transform %s: %w", message.ID, err)
	}
	fact.Acknowledge = r.acknowledger(stream, message.ID)
	select {
	case factChan <- fact:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *RedisStreamsSource) acknowledger(stream, messageID string) func(context.Context) error {
	var mu sync.Mutex
	acknowledged := false
	return func(ctx context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		if acknowledged {
			return nil
		}
		var lastErr error
		for attempt := 0; attempt < 3; attempt++ {
			count, err := r.ackMessage(ctx, stream, messageID)
			if err == nil && count == 1 {
				acknowledged = true
				return nil
			}
			if err == nil && count == 0 {
				isPending, pendingErr := r.messagePending(ctx, stream, messageID)
				if pendingErr == nil && !isPending {
					acknowledged = true
					return nil
				}
				if pendingErr != nil {
					lastErr = pendingErr
				} else {
					lastErr = fmt.Errorf("message remains pending after XACK returned zero")
				}
			} else if err != nil {
				lastErr = err
			} else {
				lastErr = fmt.Errorf("XACK acknowledged %d messages", count)
			}
			if attempt < 2 {
				select {
				case <-time.After(time.Duration(attempt+1) * 10 * time.Millisecond):
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
		return fmt.Errorf("acknowledge Redis message %s: %w", messageID, lastErr)
	}
}

func (r *RedisStreamsSource) ackMessage(ctx context.Context, stream, messageID string) (int64, error) {
	if r.xack != nil {
		return r.xack(ctx, stream, r.consumerGroup, messageID)
	}
	r.mu.Lock()
	client := r.client
	r.mu.Unlock()
	if client == nil {
		return 0, fmt.Errorf("Redis client is stopped")
	}
	return client.XAck(ctx, stream, r.consumerGroup, messageID).Result()
}

func (r *RedisStreamsSource) messagePending(ctx context.Context, stream, messageID string) (bool, error) {
	if r.pending != nil {
		return r.pending(ctx, stream, r.consumerGroup, messageID)
	}
	r.mu.Lock()
	client := r.client
	r.mu.Unlock()
	if client == nil {
		return false, fmt.Errorf("Redis client is stopped")
	}
	messages, err := client.XPendingExt(ctx, &redis.XPendingExtArgs{Stream: stream, Group: r.consumerGroup, Start: messageID, End: messageID, Count: 1}).Result()
	if err != nil {
		return false, err
	}
	return len(messages) == 1 && messages[0].ID == messageID, nil
}

func (r *RedisStreamsSource) transformMessage(streamName string, message redis.XMessage) (*adapters.TypedFact, error) {
	now := time.Now().UTC()
	eventData := map[string]interface{}{"stream": streamName, "id": message.ID, "timestamp": now, "fields": message.Values}
	data, err := json.Marshal(eventData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event data: %w", err)
	}
	return &adapters.TypedFact{
		SchemaName: r.schemaName, SchemaVersion: "v1.0.0", RawData: data,
		Timestamp: now, SourceID: r.sourceID,
		Metadata: map[string]string{
			"redis.stream": streamName, "redis.message_id": message.ID,
			"redis.consumer_group": r.consumerGroup, "redis.consumer_name": r.consumerName,
			"source_type": "redis_streams",
		},
	}, nil
}

// RedisStreamsFactory creates Redis Streams sources.
type RedisStreamsFactory struct{}

func (f *RedisStreamsFactory) Create(config adapters.SourceConfig) (adapters.FactSource, error) {
	streamsConfig := StreamsConfig{}
	if v, ok := config.Config["redis_addr"].(string); ok {
		streamsConfig.RedisAddr = v
	}
	if v, ok := config.Config["redis_db"].(float64); ok {
		streamsConfig.RedisDB = int(v)
	}
	if v, ok := config.Config["redis_db"].(int); ok {
		streamsConfig.RedisDB = v
	}
	if v, ok := config.Config["password"].(string); ok {
		streamsConfig.Password = v
	}
	if values, ok := config.Config["streams"].([]interface{}); ok {
		for _, value := range values {
			if v, ok := value.(string); ok {
				streamsConfig.Streams = append(streamsConfig.Streams, v)
			}
		}
	}
	if values, ok := config.Config["streams"].([]string); ok {
		streamsConfig.Streams = values
	}
	if v, ok := config.Config["consumer_group"].(string); ok {
		streamsConfig.ConsumerGroup = v
	}
	if v, ok := config.Config["consumer_name"].(string); ok {
		streamsConfig.ConsumerName = v
	}
	if v, ok := config.Config["batch_size"].(float64); ok {
		streamsConfig.BatchSize = int64(v)
	}
	if v, ok := config.Config["batch_size"].(int); ok {
		streamsConfig.BatchSize = int64(v)
	}
	if v, ok := config.Config["block_time"].(string); ok {
		streamsConfig.BlockTime, _ = time.ParseDuration(v)
	}
	if v, ok := config.Config["claim_min_idle"].(string); ok {
		streamsConfig.ClaimMinIdle, _ = time.ParseDuration(v)
	}
	if v, ok := config.Config["claim_interval"].(string); ok {
		streamsConfig.ClaimInterval, _ = time.ParseDuration(v)
	}
	if v, ok := config.Config["schema_name"].(string); ok {
		streamsConfig.SchemaName = v
	}
	return NewRedisStreamsSource(config.SourceID, streamsConfig)
}

func (f *RedisStreamsFactory) ValidateConfig(config adapters.SourceConfig) error {
	streams, ok := config.Config["streams"]
	if !ok {
		return fmt.Errorf("at least one stream is required for redis_streams source")
	}
	switch values := streams.(type) {
	case []interface{}:
		if len(values) == 0 {
			return fmt.Errorf("at least one stream is required for redis_streams source")
		}
	case []string:
		if len(values) == 0 {
			return fmt.Errorf("at least one stream is required for redis_streams source")
		}
	default:
		return fmt.Errorf("streams must be an array")
	}
	return nil
}

func (f *RedisStreamsFactory) GetConfigSchema() adapters.ConfigSchema {
	return adapters.ConfigSchema{
		Properties: map[string]adapters.ConfigProperty{
			"redis_addr":     {Type: "string", Description: "Redis server address", Default: "localhost:6379"},
			"redis_db":       {Type: "int", Description: "Redis database number", Default: 0},
			"password":       {Type: "string", Description: "Redis password (optional)"},
			"streams":        {Type: "array", Description: "Redis streams to consume", Examples: []string{`["events"]`}},
			"consumer_group": {Type: "string", Description: "Consumer group name"},
			"consumer_name":  {Type: "string", Description: "Consumer name"},
			"batch_size":     {Type: "int", Description: "Messages per Redis read", Default: 100},
			"block_time":     {Type: "string", Description: "Maximum blocking read time", Default: "1s"},
			"claim_min_idle": {Type: "string", Description: "Minimum PEL idle time before recovery", Default: "30s"},
			"claim_interval": {Type: "string", Description: "Pending-entry recovery interval", Default: "15s"},
			"schema_name":    {Type: "string", Description: "Schema name for generated facts", Default: "redis_stream_event"},
		},
		Required: []string{"streams"},
	}
}

func init() { adapters.RegisterSourceType("redis_streams", &RedisStreamsFactory{}) }
