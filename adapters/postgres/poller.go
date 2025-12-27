package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver

	"github.com/effectus/effectus-go/adapters"
)

// PostgresPollerSource polls PostgreSQL database at regular intervals
type PostgresPollerSource struct {
	sourceID         string
	sourceType       string
	connectionString string
	query            string
	intervalSeconds  int
	timestampColumn  string
	schemaName       string
	maxRows          int

	db       *sql.DB
	lastPoll time.Time
	ctx      context.Context
	cancel   context.CancelFunc
	schema   *adapters.Schema
}

// PollerConfig holds configuration for the PostgreSQL poller
type PollerConfig struct {
	ConnectionString string `json:"connection_string" yaml:"connection_string"`
	Query            string `json:"query" yaml:"query"`
	IntervalSeconds  int    `json:"interval_seconds" yaml:"interval_seconds"`
	TimestampColumn  string `json:"timestamp_column" yaml:"timestamp_column"`
	SchemaName       string `json:"schema_name" yaml:"schema_name"`
	MaxRows          int    `json:"max_rows" yaml:"max_rows"`
}

// NewPostgresPollerSource creates a new PostgreSQL poller source
func NewPostgresPollerSource(sourceID string, config PollerConfig) (*PostgresPollerSource, error) {
	if config.ConnectionString == "" {
		return nil, fmt.Errorf("connection_string is required")
	}
	if config.Query == "" {
		return nil, fmt.Errorf("query is required")
	}

	// Set defaults
	if config.IntervalSeconds == 0 {
		config.IntervalSeconds = 60
	}
	if config.MaxRows == 0 {
		config.MaxRows = 1000
	}
	if config.SchemaName == "" {
		config.SchemaName = "database_row"
	}

	ctx, cancel := context.WithCancel(context.Background())

	source := &PostgresPollerSource{
		sourceID:         sourceID,
		sourceType:       "postgres_poller",
		connectionString: config.ConnectionString,
		query:            config.Query,
		intervalSeconds:  config.IntervalSeconds,
		timestampColumn:  config.TimestampColumn,
		schemaName:       config.SchemaName,
		maxRows:          config.MaxRows,
		ctx:              ctx,
		cancel:           cancel,
		lastPoll:         time.Now(),
		schema: &adapters.Schema{
			Name:    config.SchemaName,
			Version: "v1.0.0",
			Fields: map[string]interface{}{
				"query":     config.Query,
				"timestamp": "datetime",
				"data":      "object",
			},
		},
	}

	return source, nil
}

func (p *PostgresPollerSource) Subscribe(ctx context.Context, factTypes []string) (<-chan *adapters.TypedFact, error) {
	factChan := make(chan *adapters.TypedFact, 100)

	// Start polling in background
	go func() {
		defer close(factChan)

		ticker := time.NewTicker(time.Duration(p.intervalSeconds) * time.Second)
		defer ticker.Stop()

		// Initial poll
		if err := p.executePoll(factChan); err != nil {
			log.Printf("Initial poll failed: %v", err)
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-p.ctx.Done():
				return
			case <-ticker.C:
				if err := p.executePoll(factChan); err != nil {
					log.Printf("Poll failed: %v", err)
				}
			}
		}
	}()

	return factChan, nil
}

func (p *PostgresPollerSource) Start(ctx context.Context) error {
	// Open database connection
	db, err := sql.Open("postgres", p.connectionString)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	p.db = db
	log.Printf("PostgreSQL poller source started, interval: %ds", p.intervalSeconds)
	return nil
}

func (p *PostgresPollerSource) Stop(ctx context.Context) error {
	p.cancel()

	if p.db != nil {
		if err := p.db.Close(); err != nil {
			return fmt.Errorf("failed to close database: %w", err)
		}
	}

	log.Printf("PostgreSQL poller source stopped")
	return nil
}

func (p *PostgresPollerSource) GetSourceSchema() *adapters.Schema {
	return p.schema
}

func (p *PostgresPollerSource) HealthCheck() error {
	if p.db == nil {
		return fmt.Errorf("database connection not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return p.db.PingContext(ctx)
}

func (p *PostgresPollerSource) GetMetadata() adapters.SourceMetadata {
	return adapters.SourceMetadata{
		SourceID:      p.sourceID,
		SourceType:    p.sourceType,
		Version:       "1.0.0",
		Capabilities:  []string{"batch", "polling", "incremental"},
		SchemaFormats: []string{"json"},
		Config: map[string]string{
			"query":            p.query,
			"interval_seconds": fmt.Sprintf("%d", p.intervalSeconds),
			"timestamp_column": p.timestampColumn,
		},
		Tags: []string{"database", "postgresql"},
	}
}

func (p *PostgresPollerSource) executePoll(factChan chan<- *adapters.TypedFact) error {
	if p.db == nil {
		return fmt.Errorf("database connection not initialized")
	}

	start := time.Now()
	defer func() {
		duration := time.Since(start)
		log.Printf("Poll completed in %v", duration)
	}()

	// Build query with timestamp filter if configured
	query := p.buildQuery()

	rows, err := p.db.QueryContext(p.ctx, query)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("failed to get columns: %w", err)
	}

	rowCount := 0
	for rows.Next() {
		if rowCount >= p.maxRows {
			log.Printf("Max rows limit reached: %d", p.maxRows)
			break
		}

		// Create slice to hold row values
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		// Scan row
		if err := rows.Scan(valuePtrs...); err != nil {
			log.Printf("Failed to scan row: %v", err)
			continue
		}

		// Create row data map
		rowData := make(map[string]interface{})
		for i, col := range columns {
			// Handle NULL values
			if values[i] == nil {
				rowData[col] = nil
			} else {
				// Convert []byte to string for text fields
				if b, ok := values[i].([]byte); ok {
					rowData[col] = string(b)
				} else {
					rowData[col] = values[i]
				}
			}
		}

		// Transform to TypedFact
		fact, err := p.transformRow(rowData, columns)
		if err != nil {
			log.Printf("Failed to transform row: %v", err)
			continue
		}

		// Send fact
		select {
		case factChan <- fact:
			rowCount++
		case <-p.ctx.Done():
			return nil
		default:
			log.Printf("Fact channel full, dropping row")
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("row iteration error: %w", err)
	}

	p.lastPoll = time.Now()
	log.Printf("Polled %d rows", rowCount)
	return nil
}

func (p *PostgresPollerSource) buildQuery() string {
	query := p.query

	// Add timestamp filter if configured
	if p.timestampColumn != "" {
		separator := " WHERE "
		if strings.Contains(strings.ToUpper(query), " WHERE ") {
			separator = " AND "
		}

		query += fmt.Sprintf("%s %s > '%s'",
			separator,
			p.timestampColumn,
			p.lastPoll.Format("2006-01-02 15:04:05"))
	}

	// Add limit
	if p.maxRows > 0 {
		query += fmt.Sprintf(" LIMIT %d", p.maxRows)
	}

	return query
}

func (p *PostgresPollerSource) transformRow(rowData map[string]interface{}, columns []string) (*adapters.TypedFact, error) {
	// Serialize row data
	data, err := json.Marshal(rowData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal row data: %w", err)
	}

	// Extract timestamp if available
	timestamp := time.Now()
	if p.timestampColumn != "" {
		if ts, exists := rowData[p.timestampColumn]; exists {
			switch v := ts.(type) {
			case time.Time:
				timestamp = v
			case string:
				if parsed, err := time.Parse("2006-01-02 15:04:05", v); err == nil {
					timestamp = parsed
				} else if parsed, err := time.Parse(time.RFC3339, v); err == nil {
					timestamp = parsed
				}
			}
		}
	}

	return &adapters.TypedFact{
		SchemaName:    p.schemaName,
		SchemaVersion: "v1.0.0",
		Data:          nil, // Would contain proto message in real implementation
		RawData:       data,
		Timestamp:     timestamp,
		SourceID:      p.sourceID,
		TraceID:       "",
		Metadata: map[string]string{
			"pg.query":        p.query,
			"pg.column_count": fmt.Sprintf("%d", len(columns)),
			"pg.columns":      strings.Join(columns, ","),
			"source_type":     "postgres_poller",
		},
	}, nil
}

// PostgresPollerFactory creates PostgreSQL poller sources
type PostgresPollerFactory struct{}

func (f *PostgresPollerFactory) Create(config adapters.SourceConfig) (adapters.FactSource, error) {
	pollerConfig := PollerConfig{}

	// Extract configuration
	if connStr, ok := config.Config["connection_string"].(string); ok {
		pollerConfig.ConnectionString = connStr
	}
	if query, ok := config.Config["query"].(string); ok {
		pollerConfig.Query = query
	}
	if interval, ok := config.Config["interval_seconds"].(float64); ok {
		pollerConfig.IntervalSeconds = int(interval)
	}
	if tsCol, ok := config.Config["timestamp_column"].(string); ok {
		pollerConfig.TimestampColumn = tsCol
	}
	if schema, ok := config.Config["schema_name"].(string); ok {
		pollerConfig.SchemaName = schema
	}
	if maxRows, ok := config.Config["max_rows"].(float64); ok {
		pollerConfig.MaxRows = int(maxRows)
	}

	return NewPostgresPollerSource(config.SourceID, pollerConfig)
}

func (f *PostgresPollerFactory) ValidateConfig(config adapters.SourceConfig) error {
	if _, ok := config.Config["connection_string"]; !ok {
		return fmt.Errorf("connection_string is required for postgres_poller source")
	}
	if _, ok := config.Config["query"]; !ok {
		return fmt.Errorf("query is required for postgres_poller source")
	}
	return nil
}

func (f *PostgresPollerFactory) GetConfigSchema() adapters.ConfigSchema {
	return adapters.ConfigSchema{
		Properties: map[string]adapters.ConfigProperty{
			"connection_string": {
				Type:        "string",
				Description: "PostgreSQL connection string",
				Examples:    []string{"postgres://user:pass@localhost:5432/db"},
			},
			"query": {
				Type:        "string",
				Description: "SQL query to execute",
				Examples:    []string{"SELECT * FROM events", "SELECT id, name, created_at FROM users"},
			},
			"interval_seconds": {
				Type:        "int",
				Description: "Polling interval in seconds",
				Default:     60,
				Examples:    []string{"30", "300"},
			},
			"timestamp_column": {
				Type:        "string",
				Description: "Column to use for incremental polling",
				Examples:    []string{"created_at", "updated_at"},
			},
			"schema_name": {
				Type:        "string",
				Description: "Schema name for generated facts",
				Default:     "database_row",
			},
			"max_rows": {
				Type:        "int",
				Description: "Maximum rows to fetch per poll",
				Default:     1000,
			},
		},
		Required: []string{"connection_string", "query"},
	}
}

func init() {
	adapters.RegisterSourceType("postgres_poller", &PostgresPollerFactory{})
}
