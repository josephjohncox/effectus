package sql

import (
	"context"
	sqldb "database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/effectus/effectus-go/adapters"
	"google.golang.org/protobuf/types/known/structpb"
)

// Source implements a generic SQL fact source with batch or streaming modes.
type Source struct {
	config        *Config
	db            *sqldb.DB
	ctx           context.Context
	cancel        context.CancelFunc
	lastWatermark interface{}
	mu            sync.Mutex
}

// Config holds SQL source configuration.
type Config struct {
	SourceID        string            `json:"source_id" yaml:"source_id"`
	Driver          string            `json:"driver" yaml:"driver"`
	DSN             string            `json:"dsn" yaml:"dsn"`
	Mode            string            `json:"mode" yaml:"mode"` // "batch" or "stream"
	Query           string            `json:"query" yaml:"query"`
	StreamingQuery  string            `json:"stream_query" yaml:"stream_query"`
	Table           string            `json:"table" yaml:"table"`
	WatermarkColumn string            `json:"watermark_column" yaml:"watermark_column"`
	StartWatermark  string            `json:"start_watermark" yaml:"start_watermark"`
	WatermarkType   string            `json:"watermark_type" yaml:"watermark_type"` // "string", "int", "float", "time"
	PollInterval    time.Duration     `json:"poll_interval" yaml:"poll_interval"`
	MaxRows         int               `json:"max_rows" yaml:"max_rows"`
	SchemaName      string            `json:"schema_name" yaml:"schema_name"`
	SchemaVersion   string            `json:"schema_version" yaml:"schema_version"`
	SourceKey       string            `json:"source_key" yaml:"source_key"`
	Timeout         time.Duration     `json:"timeout" yaml:"timeout"`
	ColumnMapping   map[string]string `json:"column_mapping" yaml:"column_mapping"`
}

// NewSource creates a new SQL source.
func NewSource(config *Config) (*Source, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	source := &Source{
		config: config,
		ctx:    ctx,
		cancel: cancel,
	}

	if config.StartWatermark != "" {
		if parsed, err := parseWatermark(config.StartWatermark, config.WatermarkType); err == nil {
			source.lastWatermark = parsed
		} else {
			source.lastWatermark = config.StartWatermark
		}
	}

	return source, nil
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
	if config.Mode == "" {
		config.Mode = "batch"
	}
	if config.Mode != "batch" && config.Mode != "stream" {
		return fmt.Errorf("mode must be batch or stream")
	}
	if config.SchemaName == "" {
		config.SchemaName = "sql_row"
	}
	if config.SchemaVersion == "" {
		config.SchemaVersion = "v1"
	}
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}
	if config.MaxRows == 0 {
		config.MaxRows = 1000
	}
	if config.PollInterval == 0 {
		if config.Mode == "stream" {
			config.PollInterval = 5 * time.Second
		} else {
			config.PollInterval = 60 * time.Second
		}
	}

	switch config.Mode {
	case "batch":
		if config.Query == "" && config.Table == "" {
			return fmt.Errorf("batch mode requires query or table")
		}
	case "stream":
		if config.StreamingQuery == "" && config.Table == "" {
			return fmt.Errorf("stream mode requires stream_query or table")
		}
		if config.StreamingQuery == "" && config.WatermarkColumn == "" {
			return fmt.Errorf("stream mode requires watermark_column when using table")
		}
	}
	return nil
}

// Start opens the SQL connection.
func (s *Source) Start(ctx context.Context) error {
	db, err := sqldb.Open(s.config.Driver, s.config.DSN)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	ctxPing, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctxPing); err != nil {
		db.Close()
		return fmt.Errorf("ping database: %w", err)
	}
	s.db = db
	log.Printf("SQL source %s started (mode=%s)", s.config.SourceID, s.config.Mode)
	return nil
}

// Stop stops the source.
func (s *Source) Stop(ctx context.Context) error {
	s.cancel()
	if s.db != nil {
		if err := s.db.Close(); err != nil {
			return err
		}
	}
	log.Printf("SQL source %s stopped", s.config.SourceID)
	return nil
}

// Subscribe starts polling rows from the SQL source.
func (s *Source) Subscribe(ctx context.Context, factTypes []string) (<-chan *adapters.TypedFact, error) {
	factChan := make(chan *adapters.TypedFact, 100)

	go func() {
		defer close(factChan)

		ticker := time.NewTicker(s.config.PollInterval)
		defer ticker.Stop()

		if err := s.pollOnce(factChan); err != nil {
			log.Printf("SQL poll error: %v", err)
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-s.ctx.Done():
				return
			case <-ticker.C:
				if err := s.pollOnce(factChan); err != nil {
					log.Printf("SQL poll error: %v", err)
				}
			}
		}
	}()

	return factChan, nil
}

// GetSourceSchema returns schema metadata.
func (s *Source) GetSourceSchema() *adapters.Schema {
	return &adapters.Schema{
		Name:    s.config.SchemaName,
		Version: s.config.SchemaVersion,
		Fields: map[string]interface{}{
			"mode":             s.config.Mode,
			"query":            s.config.Query,
			"stream_query":     s.config.StreamingQuery,
			"table":            s.config.Table,
			"watermark_column": s.config.WatermarkColumn,
			"poll_interval":    s.config.PollInterval.String(),
			"max_rows":         s.config.MaxRows,
		},
	}
}

// HealthCheck checks DB connectivity.
func (s *Source) HealthCheck() error {
	if s.db == nil {
		return fmt.Errorf("database connection not initialized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.db.PingContext(ctx)
}

// GetMetadata returns source metadata.
func (s *Source) GetMetadata() adapters.SourceMetadata {
	return adapters.SourceMetadata{
		SourceID:      s.config.SourceID,
		SourceType:    "sql",
		Version:       "1.0.0",
		Capabilities:  []string{"batch", "stream"},
		SchemaFormats: []string{"json"},
		Config: map[string]string{
			"mode":          s.config.Mode,
			"poll_interval": s.config.PollInterval.String(),
		},
		Tags: []string{"sql", "warehouse"},
	}
}

func (s *Source) pollOnce(factChan chan<- *adapters.TypedFact) error {
	if s.db == nil {
		return fmt.Errorf("database connection not initialized")
	}

	ctx, cancel := context.WithTimeout(s.ctx, s.config.Timeout)
	defer cancel()

	var query string
	var args []interface{}

	if s.config.Mode == "stream" {
		query, args = s.buildStreamingQuery()
	} else {
		query = s.buildBatchQuery()
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("columns: %w", err)
	}

	for rows.Next() {
		rowMap, err := scanRow(columns, rows, s.config.ColumnMapping)
		if err != nil {
			return err
		}

		if s.config.Mode == "stream" {
			s.updateWatermark(rowMap)
		}

		fact, err := s.rowToFact(rowMap)
		if err != nil {
			return err
		}

		select {
		case factChan <- fact:
		default:
			log.Printf("SQL source %s channel full, dropping fact", s.config.SourceID)
		}
	}

	return rows.Err()
}

func (s *Source) buildBatchQuery() string {
	if s.config.Query != "" {
		return s.config.Query
	}
	return fmt.Sprintf("SELECT * FROM %s", s.config.Table)
}

func (s *Source) buildStreamingQuery() (string, []interface{}) {
	if s.config.StreamingQuery != "" {
		return s.config.StreamingQuery, []interface{}{s.currentWatermark()}
	}

	query := fmt.Sprintf("SELECT * FROM %s WHERE %s > ? ORDER BY %s ASC LIMIT %d", s.config.Table, s.config.WatermarkColumn, s.config.WatermarkColumn, s.config.MaxRows)
	return query, []interface{}{s.currentWatermark()}
}

func (s *Source) currentWatermark() interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastWatermark == nil {
		return s.config.StartWatermark
	}
	return s.lastWatermark
}

func (s *Source) updateWatermark(row map[string]interface{}) {
	if s.config.WatermarkColumn == "" {
		return
	}

	value, ok := row[s.config.WatermarkColumn]
	if !ok {
		return
	}
	value = normalizeValue(value)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastWatermark = value
}

func (s *Source) rowToFact(row map[string]interface{}) (*adapters.TypedFact, error) {
	payload, err := structpb.NewStruct(row)
	if err != nil {
		return nil, fmt.Errorf("struct payload: %w", err)
	}

	raw, _ := json.Marshal(row)

	return &adapters.TypedFact{
		SchemaName:    s.config.SchemaName,
		SchemaVersion: s.config.SchemaVersion,
		Data:          payload,
		RawData:       raw,
		Timestamp:     time.Now(),
		SourceID:      s.config.SourceID,
		Metadata: map[string]string{
			"source_key": s.config.SourceKey,
			"mode":       s.config.Mode,
		},
	}, nil
}

func scanRow(columns []string, rows *sqldb.Rows, mapping map[string]string) (map[string]interface{}, error) {
	values := make([]interface{}, len(columns))
	ptrs := make([]interface{}, len(columns))
	for i := range values {
		ptrs[i] = &values[i]
	}

	if err := rows.Scan(ptrs...); err != nil {
		return nil, err
	}

	row := make(map[string]interface{}, len(columns))
	for i, col := range columns {
		key := col
		if mapping != nil {
			if mapped, ok := mapping[col]; ok {
				key = mapped
			}
		}
		row[key] = normalizeValue(values[i])
	}
	return row, nil
}

func normalizeValue(value interface{}) interface{} {
	switch v := value.(type) {
	case []byte:
		return string(v)
	default:
		return v
	}
}

func parseWatermark(raw string, kind string) (interface{}, error) {
	switch strings.ToLower(kind) {
	case "int":
		var v int64
		if err := json.Unmarshal([]byte(raw), &v); err == nil {
			return v, nil
		}
		return nil, fmt.Errorf("invalid int watermark")
	case "float":
		var v float64
		if err := json.Unmarshal([]byte(raw), &v); err == nil {
			return v, nil
		}
		return nil, fmt.Errorf("invalid float watermark")
	case "time":
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return nil, err
		}
		return parsed, nil
	default:
		return raw, nil
	}
}

// Factory for SQL sources.
type Factory struct{}

func (f *Factory) Create(config adapters.SourceConfig) (adapters.FactSource, error) {
	sqlConfig := &Config{SourceID: config.SourceID}

	if v, ok := config.Config["driver"].(string); ok {
		sqlConfig.Driver = v
	}
	if v, ok := config.Config["dsn"].(string); ok {
		sqlConfig.DSN = v
	}
	if v, ok := config.Config["mode"].(string); ok {
		sqlConfig.Mode = v
	}
	if v, ok := config.Config["query"].(string); ok {
		sqlConfig.Query = v
	}
	if v, ok := config.Config["stream_query"].(string); ok {
		sqlConfig.StreamingQuery = v
	}
	if v, ok := config.Config["table"].(string); ok {
		sqlConfig.Table = v
	}
	if v, ok := config.Config["watermark_column"].(string); ok {
		sqlConfig.WatermarkColumn = v
	}
	if v, ok := config.Config["start_watermark"].(string); ok {
		sqlConfig.StartWatermark = v
	}
	if v, ok := config.Config["watermark_type"].(string); ok {
		sqlConfig.WatermarkType = v
	}
	if v, ok := config.Config["schema_name"].(string); ok {
		sqlConfig.SchemaName = v
	}
	if v, ok := config.Config["schema_version"].(string); ok {
		sqlConfig.SchemaVersion = v
	}
	if v, ok := config.Config["source_key"].(string); ok {
		sqlConfig.SourceKey = v
	}
	if v, ok := config.Config["timeout"].(string); ok {
		if parsed, err := time.ParseDuration(v); err == nil {
			sqlConfig.Timeout = parsed
		}
	}
	if v, ok := config.Config["poll_interval"].(string); ok {
		if parsed, err := time.ParseDuration(v); err == nil {
			sqlConfig.PollInterval = parsed
		}
	}
	if v, ok := config.Config["max_rows"].(float64); ok {
		sqlConfig.MaxRows = int(v)
	}
	if v, ok := config.Config["max_rows"].(int); ok {
		sqlConfig.MaxRows = v
	}

	if mappings, ok := config.Config["column_mapping"].(map[string]interface{}); ok {
		result := make(map[string]string)
		for key, value := range mappings {
			if str, ok := value.(string); ok {
				result[key] = str
			}
		}
		sqlConfig.ColumnMapping = result
	}

	if sqlConfig.SchemaName == "" && len(config.Mappings) == 1 {
		sqlConfig.SchemaName = config.Mappings[0].EffectusType
		sqlConfig.SchemaVersion = config.Mappings[0].SchemaVersion
		sqlConfig.SourceKey = config.Mappings[0].SourceKey
	}

	return NewSource(sqlConfig)
}

func (f *Factory) ValidateConfig(config adapters.SourceConfig) error {
	if _, ok := config.Config["driver"]; !ok {
		return fmt.Errorf("driver is required")
	}
	if _, ok := config.Config["dsn"]; !ok {
		return fmt.Errorf("dsn is required")
	}
	if _, ok := config.Config["query"]; !ok {
		if _, ok := config.Config["stream_query"]; !ok {
			if _, ok := config.Config["table"]; !ok {
				return fmt.Errorf("query, stream_query, or table is required")
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
				Description: "database/sql driver name (e.g., postgres, snowflake, mysql)",
				Examples:    []string{"postgres", "snowflake", "mysql"},
			},
			"dsn": {
				Type:        "string",
				Description: "database connection string",
				Examples:    []string{"postgres://user:pass@host/db"},
			},
			"mode": {
				Type:        "string",
				Description: "batch or stream",
				Default:     "batch",
				Examples:    []string{"batch", "stream"},
			},
			"query": {
				Type:        "string",
				Description: "SQL query to execute (batch mode)",
			},
			"stream_query": {
				Type:        "string",
				Description: "SQL query with a single placeholder for watermark (stream mode)",
			},
			"table": {
				Type:        "string",
				Description: "Table name (used to build a simple query)",
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
			"column_mapping": {
				Type:        "object",
				Description: "Optional column name remapping",
			},
		},
		Required: []string{"driver", "dsn"},
	}
}

func init() {
	adapters.RegisterSourceType("sql", &Factory{})
}
