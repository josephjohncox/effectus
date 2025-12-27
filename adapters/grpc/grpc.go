package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/effectus/effectus-go/adapters"
)

// Config holds gRPC adapter configuration.
type Config struct {
	SourceID      string                 `json:"source_id" yaml:"source_id"`
	Address       string                 `json:"address" yaml:"address"`
	Method        string                 `json:"method" yaml:"method"` // full method: /package.Service/Method
	TLS           bool                   `json:"tls" yaml:"tls"`
	ServerName    string                 `json:"server_name" yaml:"server_name"`
	Headers       map[string]string      `json:"headers" yaml:"headers"`
	Request       map[string]interface{} `json:"request" yaml:"request"`
	SchemaName    string                 `json:"schema_name" yaml:"schema_name"`
	SchemaVersion string                 `json:"schema_version" yaml:"schema_version"`
	FactTypeField string                 `json:"fact_type_field" yaml:"fact_type_field"`
	Timeout       time.Duration          `json:"timeout" yaml:"timeout"`
	BufferSize    int                    `json:"buffer_size" yaml:"buffer_size"`
}

// Source implements gRPC streaming as a fact source.
type Source struct {
	config   *Config
	conn     *grpc.ClientConn
	factChan chan *adapters.TypedFact
	metrics  adapters.SourceMetrics
	ctx      context.Context
	cancel   context.CancelFunc
	running  bool
	mappings map[string]adapters.FactMapping
	mu       sync.Mutex
}

// NewSource creates a new gRPC source.
func NewSource(config *Config, mappings []adapters.FactMapping) (*Source, error) {
	if config == nil {
		return nil, fmt.Errorf("config is nil")
	}
	if config.Address == "" {
		return nil, fmt.Errorf("address is required")
	}
	if config.Method == "" {
		return nil, fmt.Errorf("method is required")
	}
	if config.SchemaName == "" {
		config.SchemaName = "grpc_message"
	}
	if config.SchemaVersion == "" {
		config.SchemaVersion = "v1"
	}
	if config.BufferSize == 0 {
		config.BufferSize = 1000
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

// Subscribe starts the gRPC stream.
func (s *Source) Subscribe(ctx context.Context, factTypes []string) (<-chan *adapters.TypedFact, error) {
	s.factChan = make(chan *adapters.TypedFact, s.config.BufferSize)
	if err := s.Start(ctx); err != nil {
		close(s.factChan)
		return nil, err
	}
	return s.factChan, nil
}

// Start connects to the gRPC server.
func (s *Source) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return fmt.Errorf("source already running")
	}

	creds := insecure.NewCredentials()
	if s.config.TLS {
		creds = credentials.NewClientTLSFromCert(nil, s.config.ServerName)
	}

	dialCtx := ctx
	var cancel context.CancelFunc
	if s.config.Timeout > 0 {
		dialCtx, cancel = context.WithTimeout(ctx, s.config.Timeout)
		defer cancel()
	}

	conn, err := grpc.DialContext(dialCtx, s.config.Address, grpc.WithTransportCredentials(creds))
	if err != nil {
		return err
	}

	s.conn = conn
	s.running = true

	go s.consumeStream()

	log.Printf("gRPC source %s started (%s)", s.config.SourceID, s.config.Method)
	return nil
}

// Stop stops the stream and closes the connection.
func (s *Source) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return nil
	}
	s.cancel()
	s.running = false
	if s.conn != nil {
		s.conn.Close()
	}
	if s.factChan != nil {
		close(s.factChan)
	}
	log.Printf("gRPC source %s stopped", s.config.SourceID)
	return nil
}

// GetSourceSchema returns schema metadata.
func (s *Source) GetSourceSchema() *adapters.Schema {
	return &adapters.Schema{
		Name:    s.config.SchemaName,
		Version: s.config.SchemaVersion,
		Fields: map[string]interface{}{
			"address": s.config.Address,
			"method":  s.config.Method,
		},
	}
}

// HealthCheck checks connection status.
func (s *Source) HealthCheck() error {
	if s.conn == nil {
		return fmt.Errorf("connection not initialized")
	}
	return nil
}

// GetMetadata returns source metadata.
func (s *Source) GetMetadata() adapters.SourceMetadata {
	return adapters.SourceMetadata{
		SourceID:      s.config.SourceID,
		SourceType:    "grpc",
		Version:       "1.0.0",
		Capabilities:  []string{"streaming"},
		SchemaFormats: []string{"json"},
		Config: map[string]string{
			"address": s.config.Address,
			"method":  s.config.Method,
		},
		Tags: []string{"grpc", "streaming"},
	}
}

func (s *Source) consumeStream() {
	streamDesc := &grpc.StreamDesc{ServerStreams: true, ClientStreams: false}
	requestStruct := &structpb.Struct{}
	if len(s.config.Request) > 0 {
		if reqStruct, err := structpb.NewStruct(s.config.Request); err == nil {
			requestStruct = reqStruct
		}
	}

	headers := metadata.New(nil)
	for key, value := range s.config.Headers {
		headers.Append(key, value)
	}

	ctx := metadata.NewOutgoingContext(s.ctx, headers)
	if s.config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.config.Timeout)
		defer cancel()
	}

	stream, err := s.conn.NewStream(ctx, streamDesc, s.config.Method)
	if err != nil {
		s.metrics.RecordError(s.config.SourceID, "stream_open", err)
		return
	}

	if err := stream.SendMsg(requestStruct); err != nil {
		s.metrics.RecordError(s.config.SourceID, "stream_send", err)
		return
	}
	if err := stream.CloseSend(); err != nil {
		s.metrics.RecordError(s.config.SourceID, "stream_close", err)
	}

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		response := &structpb.Struct{}
		if err := stream.RecvMsg(response); err != nil {
			if strings.Contains(err.Error(), "EOF") {
				return
			}
			s.metrics.RecordError(s.config.SourceID, "stream_recv", err)
			return
		}

		fact := s.structToFact(response)
		select {
		case s.factChan <- fact:
			s.metrics.RecordFactProcessed(s.config.SourceID, fact.SchemaName)
		case <-s.ctx.Done():
			return
		default:
			s.metrics.RecordError(s.config.SourceID, "channel_full", fmt.Errorf("fact channel full"))
		}
	}
}

func (s *Source) structToFact(payload *structpb.Struct) *adapters.TypedFact {
	schemaName, schemaVersion := s.resolveSchema(payload)

	raw, _ := json.Marshal(payload.AsMap())

	return &adapters.TypedFact{
		SchemaName:    schemaName,
		SchemaVersion: schemaVersion,
		Data:          payload,
		RawData:       raw,
		Timestamp:     time.Now().UTC(),
		SourceID:      s.config.SourceID,
	}
}

func (s *Source) resolveSchema(payload *structpb.Struct) (string, string) {
	if s.config.FactTypeField != "" {
		if value, ok := payload.AsMap()[s.config.FactTypeField]; ok {
			if key, ok := value.(string); ok {
				if mapping, exists := s.mappings[key]; exists {
					if mapping.SchemaVersion != "" {
						return mapping.EffectusType, mapping.SchemaVersion
					}
					return mapping.EffectusType, s.config.SchemaVersion
				}
				return key, s.config.SchemaVersion
			}
		}
	}

	return s.config.SchemaName, s.config.SchemaVersion
}

// Factory creates gRPC sources from generic config.
type Factory struct{}

func (f *Factory) Create(config adapters.SourceConfig) (adapters.FactSource, error) {
	grpcConfig := &Config{
		SourceID:      config.SourceID,
		SchemaName:    "grpc_message",
		SchemaVersion: "v1",
	}

	if v, ok := config.Config["address"].(string); ok {
		grpcConfig.Address = v
	}
	if v, ok := config.Config["method"].(string); ok {
		grpcConfig.Method = v
	}
	if v, ok := config.Config["tls"].(bool); ok {
		grpcConfig.TLS = v
	}
	if v, ok := config.Config["server_name"].(string); ok {
		grpcConfig.ServerName = v
	}
	if v, ok := config.Config["schema_name"].(string); ok {
		grpcConfig.SchemaName = v
	}
	if v, ok := config.Config["schema_version"].(string); ok {
		grpcConfig.SchemaVersion = v
	}
	if v, ok := config.Config["fact_type_field"].(string); ok {
		grpcConfig.FactTypeField = v
	}
	if v, ok := config.Config["buffer_size"].(float64); ok {
		grpcConfig.BufferSize = int(v)
	}
	if v, ok := config.Config["timeout"].(string); ok {
		if parsed, err := time.ParseDuration(v); err == nil {
			grpcConfig.Timeout = parsed
		}
	}
	if v, ok := config.Config["headers"].(map[string]interface{}); ok {
		headers := make(map[string]string)
		for key, value := range v {
			if str, ok := value.(string); ok {
				headers[key] = str
			}
		}
		grpcConfig.Headers = headers
	}
	if v, ok := config.Config["request"].(map[string]interface{}); ok {
		grpcConfig.Request = v
	}

	return NewSource(grpcConfig, config.Mappings)
}

func (f *Factory) ValidateConfig(config adapters.SourceConfig) error {
	if _, ok := config.Config["address"]; !ok {
		return fmt.Errorf("address is required for grpc source")
	}
	if _, ok := config.Config["method"]; !ok {
		return fmt.Errorf("method is required for grpc source")
	}
	return nil
}

func (f *Factory) GetConfigSchema() adapters.ConfigSchema {
	return adapters.ConfigSchema{
		Properties: map[string]adapters.ConfigProperty{
			"address": {
				Type:        "string",
				Description: "gRPC server address",
			},
			"method": {
				Type:        "string",
				Description: "Full method name (e.g., /pkg.Service/StreamFacts)",
			},
			"tls": {
				Type:        "bool",
				Description: "Enable TLS",
				Default:     false,
			},
			"server_name": {
				Type:        "string",
				Description: "TLS server name override",
			},
			"headers": {
				Type:        "object",
				Description: "Request metadata headers",
			},
			"request": {
				Type:        "object",
				Description: "Request payload sent as google.protobuf.Struct",
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
			"fact_type_field": {
				Type:        "string",
				Description: "Field in response struct used for schema mapping",
			},
			"timeout": {
				Type:        "string",
				Description: "Optional stream timeout (e.g., 30s)",
			},
			"buffer_size": {
				Type:        "int",
				Description: "Channel buffer size for facts",
				Default:     1000,
			},
		},
		Required: []string{"address", "method"},
	}
}

func init() {
	adapters.RegisterSourceType("grpc", &Factory{})
}
