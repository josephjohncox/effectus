package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/lib/pq"

	"github.com/josephjohncox/effectus/internal/adapters"
)

var pollerIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// PostgresPollerSource polls PostgreSQL database at regular intervals.
type PostgresPollerSource struct {
	sourceID         string
	sourceType       string
	connectionString string
	query            string
	intervalSeconds  int
	timestampColumn  string
	tieBreakColumn   string
	processedLedger  string
	schemaName       string
	maxRows          int

	db     *sql.DB
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	out    chan *adapters.TypedFact
	schema *adapters.Schema

	mu      sync.Mutex
	running bool
	cursor  pollCursor
}

type pollCursor struct {
	timestamp time.Time
	tieBreak  interface{}
	set       bool
}

// PollerConfig holds configuration for the PostgreSQL poller.
type PollerConfig struct {
	ConnectionString string      `json:"connection_string" yaml:"connection_string"`
	Query            string      `json:"query" yaml:"query"`
	IntervalSeconds  int         `json:"interval_seconds" yaml:"interval_seconds"`
	TimestampColumn  string      `json:"timestamp_column" yaml:"timestamp_column"`
	TieBreakColumn   string      `json:"tie_break_column" yaml:"tie_break_column"`
	ProcessedLedger  string      `json:"processed_ledger_table" yaml:"processed_ledger_table"`
	StartTimestamp   string      `json:"start_timestamp" yaml:"start_timestamp"`
	StartTieBreak    interface{} `json:"start_tie_break" yaml:"start_tie_break"`
	SchemaName       string      `json:"schema_name" yaml:"schema_name"`
	MaxRows          int         `json:"max_rows" yaml:"max_rows"`
}

// NewPostgresPollerSource creates a new PostgreSQL poller source.
func NewPostgresPollerSource(sourceID string, config PollerConfig) (*PostgresPollerSource, error) {
	if config.ConnectionString == "" {
		return nil, fmt.Errorf("connection_string is required")
	}
	if strings.TrimSpace(config.Query) == "" {
		return nil, fmt.Errorf("query is required")
	}
	if config.TimestampColumn != "" {
		if config.TieBreakColumn == "" {
			return nil, fmt.Errorf("tie_break_column is required for incremental polling")
		}
		if !pollerIdentifier.MatchString(config.TimestampColumn) || !pollerIdentifier.MatchString(config.TieBreakColumn) {
			return nil, fmt.Errorf("timestamp_column and tie_break_column must be simple SQL identifiers")
		}
		if !pollerIdentifier.MatchString(config.ProcessedLedger) {
			return nil, fmt.Errorf("processed_ledger_table is required for lossless incremental polling and must be a simple SQL identifier")
		}
	}
	if config.TieBreakColumn != "" && config.TimestampColumn == "" {
		return nil, fmt.Errorf("timestamp_column is required when tie_break_column is set")
	}
	if config.IntervalSeconds <= 0 {
		config.IntervalSeconds = 60
	}
	if config.MaxRows <= 0 {
		config.MaxRows = 1000
	}
	if config.SchemaName == "" {
		config.SchemaName = "database_row"
	}

	var cursor pollCursor
	if config.StartTimestamp != "" {
		parsed, err := time.Parse(time.RFC3339Nano, config.StartTimestamp)
		if err != nil {
			return nil, fmt.Errorf("start_timestamp must be RFC3339: %w", err)
		}
		if config.StartTieBreak == nil {
			return nil, fmt.Errorf("start_tie_break is required with start_timestamp")
		}
		cursor = pollCursor{timestamp: parsed, tieBreak: config.StartTieBreak, set: true}
	}

	return &PostgresPollerSource{
		sourceID:         sourceID,
		sourceType:       "postgres_poller",
		connectionString: config.ConnectionString,
		query:            strings.TrimSuffix(strings.TrimSpace(config.Query), ";"),
		intervalSeconds:  config.IntervalSeconds,
		timestampColumn:  config.TimestampColumn,
		tieBreakColumn:   config.TieBreakColumn,
		processedLedger:  config.ProcessedLedger,
		schemaName:       config.SchemaName,
		maxRows:          config.MaxRows,
		cursor:           cursor,
		schema: &adapters.Schema{
			Name:    config.SchemaName,
			Version: "v1.0.0",
			Fields: map[string]interface{}{
				"query":       config.Query,
				"timestamp":   config.TimestampColumn,
				"tie_breaker": config.TieBreakColumn,
			},
		},
	}, nil
}

func (p *PostgresPollerSource) Subscribe(ctx context.Context, factTypes []string) (<-chan *adapters.TypedFact, error) {
	p.mu.Lock()
	if p.running {
		ch := p.out
		p.mu.Unlock()
		return ch, nil
	}
	needStart := p.db == nil
	p.mu.Unlock()
	if needStart {
		if err := p.Start(ctx); err != nil {
			return nil, err
		}
	}

	p.mu.Lock()
	if p.running {
		ch := p.out
		p.mu.Unlock()
		return ch, nil
	}
	p.ctx, p.cancel = context.WithCancel(ctx)
	p.out = make(chan *adapters.TypedFact, 100)
	p.done = make(chan struct{})
	p.running = true
	ch, done := p.out, p.done
	p.mu.Unlock()

	go p.pollLoop(ch, done)
	return ch, nil
}

func (p *PostgresPollerSource) pollLoop(factChan chan *adapters.TypedFact, done chan struct{}) {
	defer close(done)
	defer close(factChan)
	defer func() {
		p.mu.Lock()
		p.running = false
		p.mu.Unlock()
	}()

	ticker := time.NewTicker(time.Duration(p.intervalSeconds) * time.Second)
	defer ticker.Stop()

	if err := p.executePoll(p.ctx, factChan); err != nil && p.ctx.Err() == nil {
		log.Printf("Initial poll failed: %v", err)
	}
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			if err := p.executePoll(p.ctx, factChan); err != nil && p.ctx.Err() == nil {
				log.Printf("Poll failed: %v", err)
			}
		}
	}
}

func (p *PostgresPollerSource) Start(ctx context.Context) error {
	p.mu.Lock()
	if p.db != nil {
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()

	db, err := sql.Open("postgres", p.connectionString)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}
	p.mu.Lock()
	if p.db != nil {
		p.mu.Unlock()
		db.Close()
		return nil
	}
	p.db = db
	p.mu.Unlock()
	log.Printf("PostgreSQL poller source started, interval: %ds", p.intervalSeconds)
	return nil
}

func (p *PostgresPollerSource) Stop(ctx context.Context) error {
	p.mu.Lock()
	cancel, done, db := p.cancel, p.done, p.db
	p.db = nil
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if db != nil {
		_ = db.Close()
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	log.Printf("PostgreSQL poller source stopped")
	return nil
}

func (p *PostgresPollerSource) GetSourceSchema() *adapters.Schema { return p.schema }

func (p *PostgresPollerSource) HealthCheck() error {
	p.mu.Lock()
	db := p.db
	p.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database connection not initialized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return db.PingContext(ctx)
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
			"tie_break_column": p.tieBreakColumn,
		},
		Tags: []string{"database", "postgresql"},
	}
}

// executePoll drains every page that is visible through the ordered tuple
// cursor. The durable ledger and cursor move only after caller acknowledgement.
func (p *PostgresPollerSource) executePoll(ctx context.Context, factChan chan<- *adapters.TypedFact) error {
	p.mu.Lock()
	db := p.db
	p.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database connection not initialized")
	}

	for {
		query, args := p.buildQuery()
		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("failed to execute query: %w", err)
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			return fmt.Errorf("failed to get columns: %w", err)
		}

		type preparedRow struct {
			fact *adapters.TypedFact
			next pollCursor
		}
		prepared := make([]preparedRow, 0, p.maxRows)
		for rows.Next() {
			values := make([]interface{}, len(columns))
			valuePtrs := make([]interface{}, len(columns))
			for index := range values {
				valuePtrs[index] = &values[index]
			}
			if err := rows.Scan(valuePtrs...); err != nil {
				rows.Close()
				return fmt.Errorf("failed to scan row: %w", err)
			}
			rowData := make(map[string]interface{}, len(columns))
			for index, column := range columns {
				if data, ok := values[index].([]byte); ok {
					rowData[column] = string(data)
				} else {
					rowData[column] = values[index]
				}
			}
			fact, err := p.transformRow(rowData, columns)
			if err != nil {
				rows.Close()
				return fmt.Errorf("failed to transform row: %w", err)
			}
			var next pollCursor
			if p.timestampColumn != "" {
				timestamp, timestampErr := timestampValue(rowData[p.timestampColumn])
				if timestampErr != nil {
					rows.Close()
					return fmt.Errorf("cursor column %s: %w", p.timestampColumn, timestampErr)
				}
				tie, ok := rowData[p.tieBreakColumn]
				if !ok || tie == nil {
					rows.Close()
					return fmt.Errorf("cursor tie-break column %s is missing or null", p.tieBreakColumn)
				}
				next = pollCursor{timestamp: timestamp, tieBreak: tie, set: true}
			}
			prepared = append(prepared, preparedRow{fact: fact, next: next})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("row iteration error: %w", err)
		}
		rows.Close() // release the query connection before acknowledgement commits

		for _, row := range prepared {
			if row.next.set {
				next := row.next
				barrier := adapters.NewAcknowledgementBarrier(1, func(ackCtx context.Context) error {
					if err := p.markProcessed(ackCtx, db, next.tieBreak); err != nil {
						return err
					}
					p.mu.Lock()
					p.cursor = next
					p.mu.Unlock()
					return nil
				})
				row.fact.Acknowledge = barrier.Callback(0)
				select {
				case factChan <- row.fact:
				case <-ctx.Done():
					return ctx.Err()
				}
				if err := barrier.Wait(ctx); err != nil {
					return err
				}
			} else {
				select {
				case factChan <- row.fact:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
		rowCount := len(prepared)

		// Batch mode has no durable cursor and therefore executes once per tick.
		if p.timestampColumn == "" || rowCount < p.maxRows {
			return nil
		}
	}
}

func (p *PostgresPollerSource) buildQuery() (string, []interface{}) {
	base := fmt.Sprintf("SELECT effectus_source.* FROM (%s) AS effectus_source", p.query)
	if p.timestampColumn == "" {
		return fmt.Sprintf("%s LIMIT $1", base), []interface{}{p.maxRows}
	}
	ts := pq.QuoteIdentifier(p.timestampColumn)
	tie := pq.QuoteIdentifier(p.tieBreakColumn)
	ledger := pq.QuoteIdentifier(p.processedLedger)
	notProcessed := fmt.Sprintf("NOT EXISTS (SELECT 1 FROM %s AS effectus_processed WHERE effectus_processed.source_id = $1 AND effectus_processed.record_key = effectus_source.%s::text)", ledger, tie)
	// The processed-key ledger is the durable cursor. Do not add a tuple lower
	// bound here: a transaction with an older timestamp can commit after a
	// newer row and must still be discovered on a later scan.
	return fmt.Sprintf("%s WHERE %s ORDER BY %s, %s LIMIT $2", base, notProcessed, ts, tie), []interface{}{p.sourceID, p.maxRows}
}

func (p *PostgresPollerSource) markProcessed(ctx context.Context, db *sql.DB, tieBreak interface{}) error {
	ledger := pq.QuoteIdentifier(p.processedLedger)
	query := fmt.Sprintf("INSERT INTO %s (source_id, record_key, processed_at) VALUES ($1, $2, NOW()) ON CONFLICT (source_id, record_key) DO NOTHING", ledger)
	if _, err := db.ExecContext(ctx, query, p.sourceID, fmt.Sprint(tieBreak)); err != nil {
		return fmt.Errorf("record processed key in %s: %w", p.processedLedger, err)
	}
	return nil
}

func timestampValue(value interface{}) (time.Time, error) {
	switch v := value.(type) {
	case time.Time:
		return v, nil
	case string:
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05"} {
			if parsed, err := time.Parse(layout, v); err == nil {
				return parsed, nil
			}
		}
	}
	return time.Time{}, fmt.Errorf("expected a timestamp, got %T", value)
}

func (p *PostgresPollerSource) transformRow(rowData map[string]interface{}, columns []string) (*adapters.TypedFact, error) {
	data, err := json.Marshal(rowData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal row data: %w", err)
	}
	timestamp := time.Now().UTC()
	if p.timestampColumn != "" {
		timestamp, err = timestampValue(rowData[p.timestampColumn])
		if err != nil {
			return nil, err
		}
	}
	return &adapters.TypedFact{
		SchemaName:    p.schemaName,
		SchemaVersion: "v1.0.0",
		RawData:       data,
		Timestamp:     timestamp,
		SourceID:      p.sourceID,
		Metadata: map[string]string{
			"pg.query":        p.query,
			"pg.column_count": fmt.Sprintf("%d", len(columns)),
			"pg.columns":      strings.Join(columns, ","),
			"source_type":     "postgres_poller",
		},
	}, nil
}

// PostgresPollerFactory creates PostgreSQL poller sources.
type PostgresPollerFactory struct{}

func (f *PostgresPollerFactory) Create(config adapters.SourceConfig) (adapters.FactSource, error) {
	pollerConfig := PollerConfig{}
	if v, ok := config.Config["connection_string"].(string); ok {
		pollerConfig.ConnectionString = v
	}
	if v, ok := config.Config["query"].(string); ok {
		pollerConfig.Query = v
	}
	if v, ok := config.Config["interval_seconds"].(float64); ok {
		pollerConfig.IntervalSeconds = int(v)
	}
	if v, ok := config.Config["interval_seconds"].(int); ok {
		pollerConfig.IntervalSeconds = v
	}
	if v, ok := config.Config["timestamp_column"].(string); ok {
		pollerConfig.TimestampColumn = v
	}
	if v, ok := config.Config["tie_break_column"].(string); ok {
		pollerConfig.TieBreakColumn = v
	}
	if v, ok := config.Config["processed_ledger_table"].(string); ok {
		pollerConfig.ProcessedLedger = v
	}
	if v, ok := config.Config["start_timestamp"].(string); ok {
		pollerConfig.StartTimestamp = v
	}
	if v, ok := config.Config["start_tie_break"]; ok {
		pollerConfig.StartTieBreak = v
	}
	if v, ok := config.Config["schema_name"].(string); ok {
		pollerConfig.SchemaName = v
	}
	if v, ok := config.Config["max_rows"].(float64); ok {
		pollerConfig.MaxRows = int(v)
	}
	if v, ok := config.Config["max_rows"].(int); ok {
		pollerConfig.MaxRows = v
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
	if _, incremental := config.Config["timestamp_column"]; incremental {
		if _, ok := config.Config["tie_break_column"]; !ok {
			return fmt.Errorf("tie_break_column is required for incremental postgres_poller source")
		}
		if _, ok := config.Config["processed_ledger_table"]; !ok {
			return fmt.Errorf("processed_ledger_table is required for lossless incremental postgres_poller source")
		}
	}
	return nil
}

func (f *PostgresPollerFactory) GetConfigSchema() adapters.ConfigSchema {
	return adapters.ConfigSchema{
		Properties: map[string]adapters.ConfigProperty{
			"connection_string":      {Type: "string", Description: "PostgreSQL connection string", Examples: []string{"postgres://user:pass@localhost:5432/db"}},
			"query":                  {Type: "string", Description: "Base SELECT query; incremental filtering and ordering are applied outside it", Examples: []string{"SELECT id, payload, created_at FROM events"}},
			"interval_seconds":       {Type: "int", Description: "Polling interval in seconds", Default: 60},
			"timestamp_column":       {Type: "string", Description: "Timestamp component of the incremental cursor"},
			"tie_break_column":       {Type: "string", Description: "Required globally unique record key for incremental polling"},
			"processed_ledger_table": {Type: "string", Description: "Existing durable ledger with source_id, record_key, and processed_at columns"},
			"start_timestamp":        {Type: "string", Description: "Optional RFC3339 lower bound"},
			"start_tie_break":        {Type: "string", Description: "Initial tie-break value; required with start_timestamp"},
			"schema_name":            {Type: "string", Description: "Schema name for generated facts", Default: "database_row"},
			"max_rows":               {Type: "int", Description: "Rows per deterministic page", Default: 1000},
		},
		Required: []string{"connection_string", "query"},
	}
}

func init() { adapters.RegisterSourceType("postgres_poller", &PostgresPollerFactory{}) }
