package adapters

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
)

// SchemaFormat describes how to interpret a schema definition payload.
type SchemaFormat string

const (
	SchemaFormatAuto       SchemaFormat = "auto"
	SchemaFormatJSONSchema SchemaFormat = "jsonschema"
	SchemaFormatEffectus   SchemaFormat = "effectus"
	SchemaFormatProto      SchemaFormat = "proto"
)

// SchemaDefinition represents a typed schema payload returned by a provider.
type SchemaDefinition struct {
	Name    string       `json:"name" yaml:"name"`
	Version string       `json:"version" yaml:"version"`
	Format  SchemaFormat `json:"format" yaml:"format"`
	Data    []byte       `json:"-" yaml:"-"`
	Source  string       `json:"source,omitempty" yaml:"source,omitempty"`
}

// SchemaSourceConfig represents configuration for a schema source provider.
type SchemaSourceConfig struct {
	Name      string                 `json:"name" yaml:"name"`
	Type      string                 `json:"type" yaml:"type"`
	Namespace string                 `json:"namespace" yaml:"namespace"`
	Version   string                 `json:"version" yaml:"version"`
	Config    map[string]interface{} `json:"config" yaml:"config"`
	BaseDir   string                 `json:"-" yaml:"-"`
}

// SchemaProvider loads schema definitions from an external system.
type SchemaProvider interface {
	LoadSchemas(ctx context.Context) ([]SchemaDefinition, error)
	Close() error
}

// SchemaProviderFactory constructs schema providers.
type SchemaProviderFactory interface {
	Create(config SchemaSourceConfig) (SchemaProvider, error)
	ValidateConfig(config SchemaSourceConfig) error
	GetConfigSchema() ConfigSchema
}

// SchemaProviderRegistry manages available schema providers.
type SchemaProviderRegistry interface {
	RegisterSchemaProvider(providerType string, factory SchemaProviderFactory) error
	CreateProvider(config SchemaSourceConfig) (SchemaProvider, error)
	GetAvailableTypes() []string
	GetTypeInfo(providerType string) SchemaProviderTypeInfo
}

// SchemaProviderTypeInfo provides information about a schema provider type.
type SchemaProviderTypeInfo struct {
	Type         string       `json:"type"`
	Description  string       `json:"description"`
	Capabilities []string     `json:"capabilities"`
	ConfigSchema ConfigSchema `json:"config_schema"`
	Examples     []string     `json:"examples"`
}

// DefaultSchemaProviderRegistry provides a simple in-memory registry.
type DefaultSchemaProviderRegistry struct {
	factories map[string]SchemaProviderFactory
	mu        sync.RWMutex
}

// NewDefaultSchemaProviderRegistry creates a new registry.
func NewDefaultSchemaProviderRegistry() *DefaultSchemaProviderRegistry {
	return &DefaultSchemaProviderRegistry{
		factories: make(map[string]SchemaProviderFactory),
	}
}

func (r *DefaultSchemaProviderRegistry) RegisterSchemaProvider(providerType string, factory SchemaProviderFactory) error {
	if providerType == "" {
		return fmt.Errorf("schema provider type cannot be empty")
	}
	if factory == nil {
		return fmt.Errorf("schema provider factory cannot be nil")
	}
	if r.factories == nil {
		r.factories = make(map[string]SchemaProviderFactory)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[providerType] = factory
	return nil
}

func (r *DefaultSchemaProviderRegistry) CreateProvider(config SchemaSourceConfig) (SchemaProvider, error) {
	r.mu.RLock()
	factory := r.factories[config.Type]
	r.mu.RUnlock()
	if factory == nil {
		return nil, fmt.Errorf("unknown schema provider type: %s", config.Type)
	}
	if err := factory.ValidateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid config for %s: %w", config.Type, err)
	}
	return factory.Create(config)
}

func (r *DefaultSchemaProviderRegistry) GetAvailableTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	types := make([]string, 0, len(r.factories))
	for t := range r.factories {
		types = append(types, t)
	}
	return types
}

func (r *DefaultSchemaProviderRegistry) GetTypeInfo(providerType string) SchemaProviderTypeInfo {
	r.mu.RLock()
	factory := r.factories[providerType]
	r.mu.RUnlock()
	if factory == nil {
		return SchemaProviderTypeInfo{}
	}
	return SchemaProviderTypeInfo{
		Type:         providerType,
		ConfigSchema: factory.GetConfigSchema(),
	}
}

var defaultSchemaRegistry = NewDefaultSchemaProviderRegistry()

// RegisterSchemaProvider registers a schema provider type globally.
func RegisterSchemaProvider(providerType string, factory SchemaProviderFactory) error {
	return defaultSchemaRegistry.RegisterSchemaProvider(providerType, factory)
}

// CreateSchemaProvider creates a provider from configuration.
func CreateSchemaProvider(config SchemaSourceConfig) (SchemaProvider, error) {
	return defaultSchemaRegistry.CreateProvider(config)
}

// GetAvailableSchemaProviderTypes returns all registered schema provider types.
func GetAvailableSchemaProviderTypes() []string {
	return defaultSchemaRegistry.GetAvailableTypes()
}

// ResolveSchemaPath resolves a path relative to the schema source config's base directory.
func ResolveSchemaPath(config SchemaSourceConfig, path string) string {
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) || config.BaseDir == "" {
		return path
	}
	return filepath.Join(config.BaseDir, path)
}
