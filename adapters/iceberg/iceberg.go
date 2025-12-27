package iceberg

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/effectus/effectus-go/adapters"
	sqladapter "github.com/effectus/effectus-go/adapters/sql"
)

// Source implements an Iceberg fact source by delegating to the SQL adapter.
type Source struct {
	config   *Config
	delegate *sqladapter.Source
}

// Config holds Iceberg source configuration.
type Config struct {
	SourceID        string        `json:"source_id" yaml:"source_id"`
	Driver          string        `json:"driver" yaml:"driver"`
	DSN             string        `json:"dsn" yaml:"dsn"`
	Catalog         string        `json:"catalog" yaml:"catalog"`
	Namespace       string        `json:"namespace" yaml:"namespace"`
	Table           string        `json:"table" yaml:"table"`
	Mode            string        `json:"mode" yaml:"mode"` // "batch" or "stream"
	Columns         []string      `json:"columns" yaml:"columns"`
	Filter          string        `json:"filter" yaml:"filter"`
	Query           string        `json:"query" yaml:"query"`
	StreamQuery     string        `json:"stream_query" yaml:"stream_query"`
	WatermarkColumn string        `json:"watermark_column" yaml:"watermark_column"`
	StartWatermark  string        `json:"start_watermark" yaml:"start_watermark"`
	WatermarkType   string        `json:"watermark_type" yaml:"watermark_type"`
	PollInterval    time.Duration `json:"poll_interval" yaml:"poll_interval"`
	MaxRows         int           `json:"max_rows" yaml:"max_rows"`
	SchemaName      string        `json:"schema_name" yaml:"schema_name"`
	SchemaVersion   string        `json:"schema_version" yaml:"schema_version"`
	Timeout         time.Duration `json:"timeout" yaml:"timeout"`
}

// NewSource creates a new Iceberg source.
func NewSource(config *Config) (*Source, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}

	sqlConfig := &sqladapter.Config{
		SourceID:        config.SourceID,
		Driver:          config.Driver,
		DSN:             config.DSN,
		Mode:            config.Mode,
		Query:           buildBatchQuery(config),
		StreamingQuery:  buildStreamQuery(config),
		WatermarkColumn: config.WatermarkColumn,
		StartWatermark:  config.StartWatermark,
		WatermarkType:   config.WatermarkType,
		PollInterval:    config.PollInterval,
		MaxRows:         config.MaxRows,
		SchemaName:      config.SchemaName,
		SchemaVersion:   config.SchemaVersion,
		Timeout:         config.Timeout,
	}

	delegate, err := sqladapter.NewSource(sqlConfig)
	if err != nil {
		return nil, err
	}

	return &Source{
		config:   config,
		delegate: delegate,
	}, nil
}

func validateConfig(config *Config) error {
	if config == nil {
		return fmt.Errorf("config is nil")
	}
	if config.Driver == "" {
		return fmt.Errorf("driver is required")
	}
	if config.DSN == "" {
		return fmt.Errorf("dsn is required")
	}
	if config.Table == "" && config.Query == "" && config.StreamQuery == "" {
		return fmt.Errorf("table or query is required")
	}
	if config.Mode == "" {
		config.Mode = "batch"
	}
	if config.Mode != "batch" && config.Mode != "stream" {
		return fmt.Errorf("mode must be batch or stream")
	}
	if config.SchemaName == "" {
		config.SchemaName = "iceberg_row"
	}
	if config.SchemaVersion == "" {
		config.SchemaVersion = "v1"
	}
	if config.PollInterval == 0 {
		if config.Mode == "stream" {
			config.PollInterval = 5 * time.Second
		} else {
			config.PollInterval = 5 * time.Minute
		}
	}
	if config.MaxRows == 0 {
		config.MaxRows = 1000
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.Mode == "stream" && config.StreamQuery == "" && config.WatermarkColumn == "" {
		return fmt.Errorf("stream mode requires watermark_column or stream_query")
	}
	return nil
}

// Start delegates to the SQL source.
func (s *Source) Start(ctx context.Context) error {
	return s.delegate.Start(ctx)
}

// Stop delegates to the SQL source.
func (s *Source) Stop(ctx context.Context) error {
	return s.delegate.Stop(ctx)
}

// Subscribe delegates to the SQL source.
func (s *Source) Subscribe(ctx context.Context, factTypes []string) (<-chan *adapters.TypedFact, error) {
	return s.delegate.Subscribe(ctx, factTypes)
}

// GetSourceSchema returns schema metadata.
func (s *Source) GetSourceSchema() *adapters.Schema {
	return &adapters.Schema{
		Name:    s.config.SchemaName,
		Version: s.config.SchemaVersion,
		Fields: map[string]interface{}{
			"catalog":        s.config.Catalog,
			"namespace":      s.config.Namespace,
			"table":          s.config.Table,
			"mode":           s.config.Mode,
			"poll_interval":  s.config.PollInterval.String(),
			"watermark":      s.config.WatermarkColumn,
			"stream_query":   s.config.StreamQuery,
			"batch_query":    s.config.Query,
			"query_columns":  strings.Join(s.config.Columns, ","),
			"query_filter":   s.config.Filter,
			"watermark_type": s.config.WatermarkType,
		},
	}
}

// HealthCheck delegates to the SQL source.
func (s *Source) HealthCheck() error {
	return s.delegate.HealthCheck()
}

// GetMetadata returns source metadata.
func (s *Source) GetMetadata() adapters.SourceMetadata {
	return adapters.SourceMetadata{
		SourceID:      s.config.SourceID,
		SourceType:    "iceberg",
		Version:       "1.0.0",
		Capabilities:  []string{"batch", "stream"},
		SchemaFormats: []string{"json"},
		Config: map[string]string{
			"catalog":        s.config.Catalog,
			"namespace":      s.config.Namespace,
			"table":          s.config.Table,
			"mode":           s.config.Mode,
			"poll_interval":  s.config.PollInterval.String(),
			"watermark":      s.config.WatermarkColumn,
			"watermark_type": s.config.WatermarkType,
		},
		Tags: []string{"iceberg", "lakehouse"},
	}
}

func buildBatchQuery(config *Config) string {
	if config.Query != "" {
		return config.Query
	}
	columns := selectColumns(config.Columns)
	table := qualifiedTable(config)
	if config.Filter == "" {
		return fmt.Sprintf("SELECT %s FROM %s", columns, table)
	}
	return fmt.Sprintf("SELECT %s FROM %s WHERE %s", columns, table, config.Filter)
}

func buildStreamQuery(config *Config) string {
	if config.Mode != "stream" {
		return ""
	}
	if config.StreamQuery != "" {
		return config.StreamQuery
	}
	columns := selectColumns(config.Columns)
	table := qualifiedTable(config)
	filter := strings.TrimSpace(config.Filter)
	if filter != "" {
		filter = fmt.Sprintf("(%s) AND ", filter)
	}
	return fmt.Sprintf("SELECT %s FROM %s WHERE %s%s > ? ORDER BY %s ASC LIMIT %d",
		columns,
		table,
		filter,
		config.WatermarkColumn,
		config.WatermarkColumn,
		config.MaxRows,
	)
}

func selectColumns(columns []string) string {
	if len(columns) == 0 {
		return "*"
	}
	return strings.Join(columns, ", ")
}

func qualifiedTable(config *Config) string {
	if config.Catalog == "" && config.Namespace == "" {
		return config.Table
	}
	parts := []string{}
	if config.Catalog != "" {
		parts = append(parts, config.Catalog)
	}
	if config.Namespace != "" {
		parts = append(parts, config.Namespace)
	}
	if config.Table != "" {
		parts = append(parts, config.Table)
	}
	return strings.Join(parts, ".")
}

// Factory creates Iceberg sources from generic config.
type Factory struct{}

func (f *Factory) Create(config adapters.SourceConfig) (adapters.FactSource, error) {
	icebergConfig := &Config{
		SourceID: config.SourceID,
	}

	if v, ok := config.Config["driver"].(string); ok {
		icebergConfig.Driver = v
	}
	if v, ok := config.Config["dsn"].(string); ok {
		icebergConfig.DSN = v
	}
	if v, ok := config.Config["catalog"].(string); ok {
		icebergConfig.Catalog = v
	}
	if v, ok := config.Config["namespace"].(string); ok {
		icebergConfig.Namespace = v
	}
	if v, ok := config.Config["table"].(string); ok {
		icebergConfig.Table = v
	}
	if v, ok := config.Config["mode"].(string); ok {
		icebergConfig.Mode = v
	}
	if v, ok := config.Config["query"].(string); ok {
		icebergConfig.Query = v
	}
	if v, ok := config.Config["stream_query"].(string); ok {
		icebergConfig.StreamQuery = v
	}
	if v, ok := config.Config["filter"].(string); ok {
		icebergConfig.Filter = v
	}
	if v, ok := config.Config["watermark_column"].(string); ok {
		icebergConfig.WatermarkColumn = v
	}
	if v, ok := config.Config["start_watermark"].(string); ok {
		icebergConfig.StartWatermark = v
	}
	if v, ok := config.Config["watermark_type"].(string); ok {
		icebergConfig.WatermarkType = v
	}
	if v, ok := config.Config["schema_name"].(string); ok {
		icebergConfig.SchemaName = v
	}
	if v, ok := config.Config["schema_version"].(string); ok {
		icebergConfig.SchemaVersion = v
	}
	if v, ok := config.Config["poll_interval"].(string); ok {
		if parsed, err := time.ParseDuration(v); err == nil {
			icebergConfig.PollInterval = parsed
		}
	}
	if v, ok := config.Config["timeout"].(string); ok {
		if parsed, err := time.ParseDuration(v); err == nil {
			icebergConfig.Timeout = parsed
		}
	}
	if v, ok := config.Config["max_rows"].(float64); ok {
		icebergConfig.MaxRows = int(v)
	}
	if v, ok := config.Config["max_rows"].(int); ok {
		icebergConfig.MaxRows = v
	}
	if v, ok := config.Config["columns"].([]interface{}); ok {
		for _, col := range v {
			if s, ok := col.(string); ok {
				icebergConfig.Columns = append(icebergConfig.Columns, s)
			}
		}
	}
	if v, ok := config.Config["columns"].([]string); ok {
		icebergConfig.Columns = append(icebergConfig.Columns, v...)
	}

	if icebergConfig.SchemaName == "" && len(config.Mappings) == 1 {
		icebergConfig.SchemaName = config.Mappings[0].EffectusType
		icebergConfig.SchemaVersion = config.Mappings[0].SchemaVersion
	}

	return NewSource(icebergConfig)
}

func (f *Factory) ValidateConfig(config adapters.SourceConfig) error {
	if _, ok := config.Config["driver"]; !ok {
		return fmt.Errorf("driver is required")
	}
	if _, ok := config.Config["dsn"]; !ok {
		return fmt.Errorf("dsn is required")
	}
	if _, ok := config.Config["table"]; !ok {
		if _, ok := config.Config["query"]; !ok {
			if _, ok := config.Config["stream_query"]; !ok {
				return fmt.Errorf("table, query, or stream_query is required")
			}
		}
	}
	return nil
}

func (f *Factory) GetConfigSchema() adapters.ConfigSchema {
	return adapters.ConfigSchema{
		Properties: map[string]adapters.ConfigProperty{
			"driver": {
				Type:        "string",
				Description: "SQL engine driver (trino, spark, athena, snowflake)",
			},
			"dsn": {
				Type:        "string",
				Description: "Connection string for the SQL engine",
			},
			"catalog": {
				Type:        "string",
				Description: "Iceberg catalog name (for Trino/Athena)",
			},
			"namespace": {
				Type:        "string",
				Description: "Namespace or schema containing the table",
			},
			"table": {
				Type:        "string",
				Description: "Iceberg table name",
			},
			"mode": {
				Type:        "string",
				Description: "batch or stream",
				Default:     "batch",
				Examples:    []string{"batch", "stream"},
			},
			"columns": {
				Type:        "array",
				Description: "Columns to select (defaults to *)",
			},
			"filter": {
				Type:        "string",
				Description: "Optional WHERE filter",
			},
			"query": {
				Type:        "string",
				Description: "Explicit batch query override",
			},
			"stream_query": {
				Type:        "string",
				Description: "Explicit stream query override (watermark placeholder)",
			},
			"watermark_column": {
				Type:        "string",
				Description: "Column used for incremental streaming",
			},
			"start_watermark": {
				Type:        "string",
				Description: "Initial watermark value",
			},
			"watermark_type": {
				Type:        "string",
				Description: "Type for watermark parsing (string, int, float, time)",
				Default:     "string",
			},
			"poll_interval": {
				Type:        "string",
				Description: "Polling interval (e.g., 5s, 1m)",
			},
			"max_rows": {
				Type:        "int",
				Description: "Maximum rows per poll",
				Default:     1000,
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
		},
		Required: []string{"driver", "dsn"},
	}
}

func init() {
	adapters.RegisterSourceType("iceberg", &Factory{})
}
