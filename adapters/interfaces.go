package adapters

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"
)

// Placeholder for effectus schema types until we have proper imports
type Schema struct {
	Name    string
	Version string
	Fields  map[string]interface{}
}

// FactSource represents any source that can provide typed facts to Effectus
type FactSource interface {
	// Subscribe to facts with schema validation
	Subscribe(ctx context.Context, factTypes []string) (<-chan *TypedFact, error)

	// Start the source (for sources that need initialization)
	Start(ctx context.Context) error

	// Stop the source gracefully
	Stop(ctx context.Context) error

	// Get schema information for this source
	GetSourceSchema() *Schema

	// Health check
	HealthCheck() error

	// Source metadata
	GetMetadata() SourceMetadata
}

// TypedFact represents a fact with full schema information
type TypedFact struct {
	SchemaName    string            `json:"schema_name"`
	SchemaVersion string            `json:"schema_version"`
	Data          proto.Message     `json:"-"`                  // Proto message data
	RawData       []byte            `json:"raw_data,omitempty"` // Original raw data
	Timestamp     time.Time         `json:"timestamp"`
	SourceID      string            `json:"source_id"`
	TraceID       string            `json:"trace_id,omitempty"`
	SpanID        string            `json:"span_id,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

// SourceMetadata provides information about fact sources
type SourceMetadata struct {
	SourceID      string            `json:"source_id"`
	SourceType    string            `json:"source_type"` // "kafka", "http", "database", "file"
	Version       string            `json:"version"`
	Capabilities  []string          `json:"capabilities"`   // ["streaming", "batch", "realtime"]
	SchemaFormats []string          `json:"schema_formats"` // ["protobuf", "json", "avro"]
	Config        map[string]string `json:"config,omitempty"`
	Tags          []string          `json:"tags,omitempty"`
}

// FormatConverter handles conversion from external formats to proto
type FormatConverter interface {
	// Convert raw data to proto message using target schema
	Convert(rawData []byte, targetSchema *Schema) (proto.Message, error)

	// Check if converter can handle the source->target conversion
	CanConvert(sourceFormat, targetFormat string) bool

	// Get supported formats
	GetSupportedFormats() []string
}

// SourceRegistry manages available fact sources
type SourceRegistry interface {
	// Register a source type with factory function
	RegisterSourceType(sourceType string, factory SourceFactory) error

	// Create a source instance from configuration
	CreateSource(config SourceConfig) (FactSource, error)

	// List available source types
	GetAvailableTypes() []string

	// Get source type information
	GetTypeInfo(sourceType string) SourceTypeInfo
}

// SourceFactory creates instances of a specific source type
type SourceFactory interface {
	// Create a new source instance
	Create(config SourceConfig) (FactSource, error)

	// Validate configuration before creating
	ValidateConfig(config SourceConfig) error

	// Get configuration schema for this source type
	GetConfigSchema() ConfigSchema
}

// SourceConfig represents configuration for any source
type SourceConfig struct {
	SourceID   string                 `json:"source_id" yaml:"source_id"`
	Type       string                 `json:"type" yaml:"type"`
	Config     map[string]interface{} `json:"config" yaml:"config"`
	Mappings   []FactMapping          `json:"mappings" yaml:"mappings"`
	Transforms []Transformation       `json:"transforms" yaml:"transforms"`
	Tags       []string               `json:"tags" yaml:"tags"`
}

// FactMapping maps source-specific identifiers to Effectus fact types
type FactMapping struct {
	SourceKey     string `json:"source_key" yaml:"source_key"`         // Source-specific key (topic, table, etc.)
	EffectusType  string `json:"effectus_type" yaml:"effectus_type"`   // Target Effectus fact type
	SchemaVersion string `json:"schema_version" yaml:"schema_version"` // Target schema version
}

// Transformation defines how to transform data from source format
type Transformation struct {
	SourcePath string            `json:"source_path" yaml:"source_path"` // JSONPath or similar
	TargetType string            `json:"target_type" yaml:"target_type"` // Target fact type
	Mapping    map[string]string `json:"mapping" yaml:"mapping"`         // Field mappings
}

// SourceTypeInfo provides information about a source type
type SourceTypeInfo struct {
	Type         string       `json:"type"`
	Description  string       `json:"description"`
	Capabilities []string     `json:"capabilities"`
	ConfigSchema ConfigSchema `json:"config_schema"`
	Examples     []string     `json:"examples"`
}

// ConfigSchema describes the configuration schema for a source type
type ConfigSchema struct {
	Properties map[string]ConfigProperty `json:"properties"`
	Required   []string                  `json:"required"`
}

// ConfigProperty describes a single configuration property
type ConfigProperty struct {
	Type        string      `json:"type"` // "string", "int", "bool", "array", "object"
	Description string      `json:"description"`
	Default     interface{} `json:"default,omitempty"`
	Examples    []string    `json:"examples,omitempty"`
}

// SourceError represents errors from source operations
type SourceError struct {
	SourceID  string `json:"source_id"`
	Operation string `json:"operation"`
	Message   string `json:"message"`
	Cause     error  `json:"-"`
}

func (e *SourceError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("source %s: %s: %s: %v", e.SourceID, e.Operation, e.Message, e.Cause)
	}
	return fmt.Sprintf("source %s: %s: %s", e.SourceID, e.Operation, e.Message)
}

func (e *SourceError) Unwrap() error {
	return e.Cause
}

// NewSourceError creates a new source error
func NewSourceError(sourceID, operation, message string, cause error) *SourceError {
	return &SourceError{
		SourceID:  sourceID,
		Operation: operation,
		Message:   message,
		Cause:     cause,
	}
}

// SourceMetrics provides observability for sources
type SourceMetrics interface {
	// Record facts processed
	RecordFactProcessed(sourceID, factType string)

	// Record processing errors
	RecordError(sourceID, operation string, err error)

	// Record processing latency
	RecordLatency(sourceID string, duration time.Duration)

	// Record source health
	RecordHealthCheck(sourceID string, healthy bool)
}

// DefaultSourceMetrics provides a no-op implementation
type DefaultSourceMetrics struct{}

func (d *DefaultSourceMetrics) RecordFactProcessed(sourceID, factType string)         {}
func (d *DefaultSourceMetrics) RecordError(sourceID, operation string, err error)     {}
func (d *DefaultSourceMetrics) RecordLatency(sourceID string, duration time.Duration) {}
func (d *DefaultSourceMetrics) RecordHealthCheck(sourceID string, healthy bool)       {}

// DefaultSourceRegistry provides a simple in-memory registry
type DefaultSourceRegistry struct {
	factories map[string]SourceFactory
}

// NewDefaultSourceRegistry creates a new default registry
func NewDefaultSourceRegistry() *DefaultSourceRegistry {
	return &DefaultSourceRegistry{
		factories: make(map[string]SourceFactory),
	}
}

func (r *DefaultSourceRegistry) RegisterSourceType(sourceType string, factory SourceFactory) error {
	if sourceType == "" {
		return fmt.Errorf("source type cannot be empty")
	}
	if factory == nil {
		return fmt.Errorf("factory cannot be nil")
	}
	r.factories[sourceType] = factory
	return nil
}

func (r *DefaultSourceRegistry) CreateSource(config SourceConfig) (FactSource, error) {
	factory, exists := r.factories[config.Type]
	if !exists {
		return nil, fmt.Errorf("unknown source type: %s", config.Type)
	}

	if err := factory.ValidateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid config for %s: %w", config.Type, err)
	}

	return factory.Create(config)
}

func (r *DefaultSourceRegistry) GetAvailableTypes() []string {
	types := make([]string, 0, len(r.factories))
	for t := range r.factories {
		types = append(types, t)
	}
	return types
}

func (r *DefaultSourceRegistry) GetTypeInfo(sourceType string) SourceTypeInfo {
	factory, exists := r.factories[sourceType]
	if !exists {
		return SourceTypeInfo{}
	}

	return SourceTypeInfo{
		Type:         sourceType,
		ConfigSchema: factory.GetConfigSchema(),
	}
}

// Package-level convenience functions
var (
	defaultRegistry               = NewDefaultSourceRegistry()
	defaultMetrics  SourceMetrics = &DefaultSourceMetrics{}
)

// RegisterSourceType registers a source type globally
func RegisterSourceType(sourceType string, factory SourceFactory) error {
	return defaultRegistry.RegisterSourceType(sourceType, factory)
}

// CreateSource creates a source from configuration using the global registry
func CreateSource(config SourceConfig) (FactSource, error) {
	return defaultRegistry.CreateSource(config)
}

// GetAvailableSourceTypes returns all registered source types
func GetAvailableSourceTypes() []string {
	return defaultRegistry.GetAvailableTypes()
}

// SetGlobalMetrics sets the global metrics implementation
func SetGlobalMetrics(metrics SourceMetrics) {
	defaultMetrics = metrics
}

// GetGlobalMetrics returns the global metrics implementation
func GetGlobalMetrics() SourceMetrics {
	return defaultMetrics
}
