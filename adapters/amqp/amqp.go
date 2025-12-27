package amqp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/effectus/effectus-go/adapters"
)

// Config holds AMQP adapter configuration.
type Config struct {
	SourceID        string            `json:"source_id" yaml:"source_id"`
	URL             string            `json:"url" yaml:"url"`
	Queue           string            `json:"queue" yaml:"queue"`
	Exchange        string            `json:"exchange" yaml:"exchange"`
	RoutingKey      string            `json:"routing_key" yaml:"routing_key"`
	ConsumerTag     string            `json:"consumer_tag" yaml:"consumer_tag"`
	AutoAck         bool              `json:"auto_ack" yaml:"auto_ack"`
	Prefetch        int               `json:"prefetch" yaml:"prefetch"`
	Format          string            `json:"format" yaml:"format"` // json or raw
	SchemaName      string            `json:"schema_name" yaml:"schema_name"`
	SchemaVersion   string            `json:"schema_version" yaml:"schema_version"`
	SchemaHeader    string            `json:"schema_header" yaml:"schema_header"`
	QueueDeclare    bool              `json:"queue_declare" yaml:"queue_declare"`
	QueueDurable    bool              `json:"queue_durable" yaml:"queue_durable"`
	QueueExclusive  bool              `json:"queue_exclusive" yaml:"queue_exclusive"`
	QueueAutoDel    bool              `json:"queue_auto_delete" yaml:"queue_auto_delete"`
	ExchangeType    string            `json:"exchange_type" yaml:"exchange_type"`
	ExchangeDeclare bool              `json:"exchange_declare" yaml:"exchange_declare"`
	Headers         map[string]string `json:"headers" yaml:"headers"`
	BufferSize      int               `json:"buffer_size" yaml:"buffer_size"`
}

// Source implements AMQP consumption as a fact source.
type Source struct {
	config   *Config
	conn     *amqp.Connection
	channel  *amqp.Channel
	factChan chan *adapters.TypedFact
	metrics  adapters.SourceMetrics
	ctx      context.Context
	cancel   context.CancelFunc
	running  bool
	mappings map[string]adapters.FactMapping
	mu       sync.Mutex
}

// NewSource creates a new AMQP source.
func NewSource(config *Config, mappings []adapters.FactMapping) (*Source, error) {
	if config == nil {
		return nil, fmt.Errorf("config is nil")
	}
	if config.URL == "" {
		return nil, fmt.Errorf("url is required")
	}
	if config.Queue == "" {
		return nil, fmt.Errorf("queue is required")
	}
	if config.SchemaName == "" {
		config.SchemaName = "amqp_message"
	}
	if config.SchemaVersion == "" {
		config.SchemaVersion = "v1"
	}
	if config.BufferSize == 0 {
		config.BufferSize = 1000
	}
	if config.Format == "" {
		config.Format = "json"
	}
	if config.ConsumerTag == "" {
		config.ConsumerTag = fmt.Sprintf("effectus-%s", config.SourceID)
	}
	if config.ExchangeType == "" {
		config.ExchangeType = "topic"
	}

	ctx, cancel := context.WithCancel(context.Background())
	mappingMap := make(map[string]adapters.FactMapping)
	for _, m := range mappings {
		mappingMap[m.SourceKey] = m
	}

	return &Source{
		config:   config,
		metrics:  adapters.GetGlobalMetrics(),
		ctx:      ctx,
		cancel:   cancel,
		mappings: mappingMap,
	}, nil
}

// Subscribe starts consuming messages.
func (s *Source) Subscribe(ctx context.Context, factTypes []string) (<-chan *adapters.TypedFact, error) {
	s.factChan = make(chan *adapters.TypedFact, s.config.BufferSize)
	if err := s.Start(ctx); err != nil {
		close(s.factChan)
		return nil, err
	}
	return s.factChan, nil
}

// Start connects and begins consumption.
func (s *Source) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return fmt.Errorf("source already running")
	}

	conn, err := amqp.Dial(s.config.URL)
	if err != nil {
		return err
	}
	channel, err := conn.Channel()
	if err != nil {
		conn.Close()
		return err
	}

	if s.config.Prefetch > 0 {
		if err := channel.Qos(s.config.Prefetch, 0, false); err != nil {
			channel.Close()
			conn.Close()
			return err
		}
	}

	if s.config.ExchangeDeclare {
		if err := channel.ExchangeDeclare(
			s.config.Exchange,
			s.config.ExchangeType,
			true,
			false,
			false,
			false,
			nil,
		); err != nil {
			channel.Close()
			conn.Close()
			return err
		}
	}

	if s.config.QueueDeclare {
		if _, err := channel.QueueDeclare(
			s.config.Queue,
			s.config.QueueDurable,
			s.config.QueueAutoDel,
			s.config.QueueExclusive,
			false,
			nil,
		); err != nil {
			channel.Close()
			conn.Close()
			return err
		}
	}

	if s.config.Exchange != "" {
		if err := channel.QueueBind(
			s.config.Queue,
			s.config.RoutingKey,
			s.config.Exchange,
			false,
			nil,
		); err != nil {
			channel.Close()
			conn.Close()
			return err
		}
	}

	msgs, err := channel.Consume(
		s.config.Queue,
		s.config.ConsumerTag,
		s.config.AutoAck,
		s.config.QueueExclusive,
		false,
		false,
		nil,
	)
	if err != nil {
		channel.Close()
		conn.Close()
		return err
	}

	s.conn = conn
	s.channel = channel
	s.running = true

	go s.processMessages(msgs)

	log.Printf("AMQP source %s started (queue=%s)", s.config.SourceID, s.config.Queue)
	return nil
}

// Stop stops consumption and closes connection.
func (s *Source) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return nil
	}
	s.cancel()
	s.running = false
	if s.channel != nil {
		s.channel.Close()
	}
	if s.conn != nil {
		s.conn.Close()
	}
	if s.factChan != nil {
		close(s.factChan)
	}
	log.Printf("AMQP source %s stopped", s.config.SourceID)
	return nil
}

// GetSourceSchema returns schema metadata.
func (s *Source) GetSourceSchema() *adapters.Schema {
	return &adapters.Schema{
		Name:    s.config.SchemaName,
		Version: s.config.SchemaVersion,
		Fields: map[string]interface{}{
			"queue":       s.config.Queue,
			"exchange":    s.config.Exchange,
			"routing_key": s.config.RoutingKey,
			"format":      s.config.Format,
		},
	}
}

// HealthCheck checks connection status.
func (s *Source) HealthCheck() error {
	if s.conn == nil || s.conn.IsClosed() {
		return fmt.Errorf("connection not initialized")
	}
	return nil
}

// GetMetadata returns source metadata.
func (s *Source) GetMetadata() adapters.SourceMetadata {
	return adapters.SourceMetadata{
		SourceID:      s.config.SourceID,
		SourceType:    "amqp",
		Version:       "1.0.0",
		Capabilities:  []string{"streaming"},
		SchemaFormats: []string{"json"},
		Config: map[string]string{
			"queue":       s.config.Queue,
			"exchange":    s.config.Exchange,
			"routing_key": s.config.RoutingKey,
		},
		Tags: []string{"amqp", "rabbitmq"},
	}
}

func (s *Source) processMessages(msgs <-chan amqp.Delivery) {
	for {
		select {
		case <-s.ctx.Done():
			return
		case msg, ok := <-msgs:
			if !ok {
				return
			}
			fact, err := s.messageToFact(msg)
			if err != nil {
				s.metrics.RecordError(s.config.SourceID, "decode", err)
				if !s.config.AutoAck {
					msg.Nack(false, false)
				}
				continue
			}
			select {
			case s.factChan <- fact:
				s.metrics.RecordFactProcessed(s.config.SourceID, fact.SchemaName)
				if !s.config.AutoAck {
					msg.Ack(false)
				}
			case <-s.ctx.Done():
				return
			default:
				s.metrics.RecordError(s.config.SourceID, "channel_full", fmt.Errorf("fact channel full"))
				if !s.config.AutoAck {
					msg.Nack(false, true)
				}
			}
		}
	}
}

func (s *Source) messageToFact(msg amqp.Delivery) (*adapters.TypedFact, error) {
	schemaName, schemaVersion := s.resolveSchema(msg)

	payload := msg.Body
	var data *structpb.Struct
	if shouldDecodeJSON(s.config.Format, msg.ContentType) {
		var parsed interface{}
		if err := json.Unmarshal(payload, &parsed); err != nil {
			return nil, err
		}
		if obj, ok := parsed.(map[string]interface{}); ok {
			structData, err := structpb.NewStruct(obj)
			if err != nil {
				return nil, err
			}
			data = structData
		} else {
			structData, err := structpb.NewStruct(map[string]interface{}{"value": parsed})
			if err != nil {
				return nil, err
			}
			data = structData
		}
	}

	metadata := map[string]string{
		"amqp.routing_key": msg.RoutingKey,
		"amqp.exchange":    msg.Exchange,
		"amqp.message_id":  msg.MessageId,
	}

	for key, value := range msg.Headers {
		if str, ok := value.(string); ok {
			metadata["amqp.header."+key] = str
		}
	}

	return &adapters.TypedFact{
		SchemaName:    schemaName,
		SchemaVersion: schemaVersion,
		Data:          data,
		RawData:       payload,
		Timestamp:     time.Now().UTC(),
		SourceID:      s.config.SourceID,
		Metadata:      metadata,
	}, nil
}

func (s *Source) resolveSchema(msg amqp.Delivery) (string, string) {
	key := msg.RoutingKey
	if s.config.SchemaHeader != "" {
		if header, ok := msg.Headers[s.config.SchemaHeader]; ok {
			if str, ok := header.(string); ok {
				key = str
			}
		}
	}
	if mapping, ok := s.mappings[key]; ok {
		if mapping.SchemaVersion != "" {
			return mapping.EffectusType, mapping.SchemaVersion
		}
		return mapping.EffectusType, s.config.SchemaVersion
	}
	return s.config.SchemaName, s.config.SchemaVersion
}

func shouldDecodeJSON(format, contentType string) bool {
	if strings.EqualFold(format, "json") {
		return true
	}
	if strings.Contains(strings.ToLower(contentType), "json") {
		return true
	}
	return false
}

// Factory creates AMQP sources from generic config.
type Factory struct{}

func (f *Factory) Create(config adapters.SourceConfig) (adapters.FactSource, error) {
	amqpConfig := &Config{
		SourceID:      config.SourceID,
		SchemaName:    "amqp_message",
		SchemaVersion: "v1",
	}

	if v, ok := config.Config["url"].(string); ok {
		amqpConfig.URL = v
	}
	if v, ok := config.Config["queue"].(string); ok {
		amqpConfig.Queue = v
	}
	if v, ok := config.Config["exchange"].(string); ok {
		amqpConfig.Exchange = v
	}
	if v, ok := config.Config["routing_key"].(string); ok {
		amqpConfig.RoutingKey = v
	}
	if v, ok := config.Config["consumer_tag"].(string); ok {
		amqpConfig.ConsumerTag = v
	}
	if v, ok := config.Config["auto_ack"].(bool); ok {
		amqpConfig.AutoAck = v
	}
	if v, ok := config.Config["prefetch"].(float64); ok {
		amqpConfig.Prefetch = int(v)
	}
	if v, ok := config.Config["format"].(string); ok {
		amqpConfig.Format = v
	}
	if v, ok := config.Config["schema_name"].(string); ok {
		amqpConfig.SchemaName = v
	}
	if v, ok := config.Config["schema_version"].(string); ok {
		amqpConfig.SchemaVersion = v
	}
	if v, ok := config.Config["schema_header"].(string); ok {
		amqpConfig.SchemaHeader = v
	}
	if v, ok := config.Config["queue_declare"].(bool); ok {
		amqpConfig.QueueDeclare = v
	}
	if v, ok := config.Config["queue_durable"].(bool); ok {
		amqpConfig.QueueDurable = v
	}
	if v, ok := config.Config["queue_exclusive"].(bool); ok {
		amqpConfig.QueueExclusive = v
	}
	if v, ok := config.Config["queue_auto_delete"].(bool); ok {
		amqpConfig.QueueAutoDel = v
	}
	if v, ok := config.Config["exchange_declare"].(bool); ok {
		amqpConfig.ExchangeDeclare = v
	}
	if v, ok := config.Config["exchange_type"].(string); ok {
		amqpConfig.ExchangeType = v
	}
	if v, ok := config.Config["buffer_size"].(float64); ok {
		amqpConfig.BufferSize = int(v)
	}

	return NewSource(amqpConfig, config.Mappings)
}

func (f *Factory) ValidateConfig(config adapters.SourceConfig) error {
	if _, ok := config.Config["url"]; !ok {
		return fmt.Errorf("url is required for amqp source")
	}
	if _, ok := config.Config["queue"]; !ok {
		return fmt.Errorf("queue is required for amqp source")
	}
	return nil
}

func (f *Factory) GetConfigSchema() adapters.ConfigSchema {
	return adapters.ConfigSchema{
		Properties: map[string]adapters.ConfigProperty{
			"url": {
				Type:        "string",
				Description: "AMQP connection URL",
			},
			"queue": {
				Type:        "string",
				Description: "Queue to consume",
			},
			"exchange": {
				Type:        "string",
				Description: "Exchange to bind (optional)",
			},
			"routing_key": {
				Type:        "string",
				Description: "Routing key for binding",
			},
			"consumer_tag": {
				Type:        "string",
				Description: "Consumer tag",
			},
			"auto_ack": {
				Type:        "bool",
				Description: "Auto-acknowledge messages",
			},
			"prefetch": {
				Type:        "int",
				Description: "Prefetch count",
			},
			"format": {
				Type:        "string",
				Description: "json or raw",
				Default:     "json",
			},
			"schema_name": {
				Type:        "string",
				Description: "Effectus schema name for emitted facts",
			},
			"schema_version": {
				Type:        "string",
				Description: "Schema version tag",
				Default:     "v1",
			},
			"schema_header": {
				Type:        "string",
				Description: "Header key to resolve schema mapping",
			},
			"queue_declare": {
				Type:        "bool",
				Description: "Declare queue if missing",
			},
			"queue_durable": {
				Type:        "bool",
				Description: "Queue durability",
			},
			"queue_exclusive": {
				Type:        "bool",
				Description: "Exclusive queue",
			},
			"queue_auto_delete": {
				Type:        "bool",
				Description: "Auto-delete queue",
			},
			"exchange_declare": {
				Type:        "bool",
				Description: "Declare exchange if missing",
			},
			"exchange_type": {
				Type:        "string",
				Description: "Exchange type (topic, fanout, direct)",
				Default:     "topic",
			},
			"buffer_size": {
				Type:        "int",
				Description: "Channel buffer size for facts",
				Default:     1000,
			},
		},
		Required: []string{"url", "queue"},
	}
}

func init() {
	adapters.RegisterSourceType("amqp", &Factory{})
}
