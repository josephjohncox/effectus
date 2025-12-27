package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/effectus/effectus-go/adapters"
)

// CDCConfig holds PostgreSQL CDC configuration
type CDCConfig struct {
	SourceID             string                    `json:"source_id" yaml:"source_id"`
	SourceType           string                    `json:"source_type" yaml:"source_type"`
	ConnectionString     string                    `json:"connection_string" yaml:"connection_string"`
	SlotName             string                    `json:"slot_name" yaml:"slot_name"`
	PublicationName      string                    `json:"publication_name" yaml:"publication_name"`
	Plugin               string                    `json:"plugin" yaml:"plugin"`
	CreateSlot           bool                      `json:"create_slot" yaml:"create_slot"`
	Tables               []string                  `json:"tables" yaml:"tables"`
	Operations           []string                  `json:"operations" yaml:"operations"` // INSERT, UPDATE, DELETE
	SchemaMapping        map[string]string         `json:"schema_mapping" yaml:"schema_mapping"`
	StartLSN             string                    `json:"start_lsn" yaml:"start_lsn"`
	BatchSize            int                       `json:"batch_size" yaml:"batch_size"`
	HeartbeatIntervalSec int                       `json:"heartbeat_interval_sec" yaml:"heartbeat_interval_sec"`
	BufferSize           int                       `json:"buffer_size" yaml:"buffer_size"`
	PollInterval         time.Duration             `json:"poll_interval" yaml:"poll_interval"`
	MaxChanges           int                       `json:"max_changes" yaml:"max_changes"`
	Transforms           []adapters.Transformation `json:"transforms" yaml:"transforms"`
}

// CDCSource implements Change Data Capture for PostgreSQL
type CDCSource struct {
	config      *CDCConfig
	pool        *pgxpool.Pool
	factChan    chan *adapters.TypedFact
	transformer *ChangeTransformer
	metrics     adapters.SourceMetrics
	ctx         context.Context
	cancel      context.CancelFunc
	schema      *adapters.Schema
	running     bool
	currentLSN  string
}

// ChangeEvent represents a database change event
type ChangeEvent struct {
	Operation string                 `json:"operation"`
	Schema    string                 `json:"schema"`
	Table     string                 `json:"table"`
	Before    map[string]interface{} `json:"before,omitempty"`
	After     map[string]interface{} `json:"after,omitempty"`
	LSN       string                 `json:"lsn"`
	Timestamp time.Time              `json:"timestamp"`
	TxID      uint32                 `json:"tx_id"`
}

// NewCDCSource creates a new PostgreSQL CDC source
func NewCDCSource(config *CDCConfig) (*CDCSource, error) {
	if config.ConnectionString == "" {
		return nil, fmt.Errorf("connection_string is required")
	}

	// Set defaults
	if config.SlotName == "" {
		config.SlotName = fmt.Sprintf("effectus_slot_%s", config.SourceID)
	}
	if config.PublicationName == "" {
		config.PublicationName = fmt.Sprintf("effectus_pub_%s", config.SourceID)
	}
	if config.Plugin == "" {
		config.Plugin = "wal2json"
	}
	if config.PollInterval == 0 {
		config.PollInterval = 2 * time.Second
	}
	if config.MaxChanges == 0 {
		config.MaxChanges = 100
	}
	if config.BatchSize == 0 {
		config.BatchSize = 100
	}
	if config.BufferSize == 0 {
		config.BufferSize = 1000
	}
	if len(config.Operations) == 0 {
		config.Operations = []string{"INSERT", "UPDATE", "DELETE"}
	}
	if config.HeartbeatIntervalSec == 0 {
		config.HeartbeatIntervalSec = 30
	}

	ctx, cancel := context.WithCancel(context.Background())

	source := &CDCSource{
		config:      config,
		transformer: NewChangeTransformer(config),
		metrics:     adapters.GetGlobalMetrics(),
		ctx:         ctx,
		cancel:      cancel,
		schema: &adapters.Schema{
			Name:    "postgres_cdc",
			Version: "v1.0.0",
			Fields: map[string]interface{}{
				"operation": "string",
				"schema":    "string",
				"table":     "string",
				"before":    "object",
				"after":     "object",
				"lsn":       "string",
				"timestamp": "timestamp",
				"tx_id":     "uint32",
			},
		},
	}

	return source, nil
}

// FactSource interface implementation

func (c *CDCSource) Subscribe(ctx context.Context, factTypes []string) (<-chan *adapters.TypedFact, error) {
	c.factChan = make(chan *adapters.TypedFact, c.config.BufferSize)

	if err := c.Start(ctx); err != nil {
		close(c.factChan)
		return nil, err
	}

	return c.factChan, nil
}

func (c *CDCSource) Start(ctx context.Context) error {
	if c.running {
		return fmt.Errorf("source already running")
	}

	// Create connection pool
	poolConfig, err := pgxpool.ParseConfig(c.config.ConnectionString)
	if err != nil {
		return fmt.Errorf("failed to parse connection string: %w", err)
	}

	c.pool, err = pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Test connection
	if err := c.pool.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	log.Printf("PostgreSQL CDC source started for slot: %s", c.config.SlotName)

	c.running = true

	if c.config.CreateSlot {
		if err := c.ensureSlot(ctx); err != nil {
			return err
		}
	}

	if c.config.StartLSN != "" {
		c.currentLSN = c.config.StartLSN
	}

	go c.pollChanges()

	return nil
}

func (c *CDCSource) Stop(ctx context.Context) error {
	if !c.running {
		return nil
	}

	c.cancel()
	c.running = false

	if c.pool != nil {
		c.pool.Close()
	}

	if c.factChan != nil {
		close(c.factChan)
	}

	log.Printf("PostgreSQL CDC source stopped")
	return nil
}

func (c *CDCSource) GetSourceSchema() *adapters.Schema {
	return c.schema
}

func (c *CDCSource) HealthCheck() error {
	if c.pool == nil {
		return fmt.Errorf("connection pool not initialized")
	}

	return c.pool.Ping(c.ctx)
}

func (c *CDCSource) GetMetadata() adapters.SourceMetadata {
	return adapters.SourceMetadata{
		SourceID:      c.config.SourceID,
		SourceType:    "postgres_cdc",
		Version:       "1.0.0",
		Capabilities:  []string{"streaming", "realtime"},
		SchemaFormats: []string{"json"},
		Config: map[string]string{
			"slot_name":   c.config.SlotName,
			"publication": c.config.PublicationName,
			"tables":      strings.Join(c.config.Tables, ","),
			"operations":  strings.Join(c.config.Operations, ","),
		},
		Tags: []string{"database", "postgres", "cdc"},
	}
}

func (c *CDCSource) pollChanges() {
	ticker := time.NewTicker(c.config.PollInterval)
	defer ticker.Stop()

	if err := c.fetchChanges(); err != nil {
		log.Printf("PostgreSQL CDC initial fetch failed: %v", err)
	}

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if err := c.fetchChanges(); err != nil {
				log.Printf("PostgreSQL CDC fetch failed: %v", err)
			}
		}
	}
}

func (c *CDCSource) fetchChanges() error {
	if c.pool == nil {
		return fmt.Errorf("connection pool not initialized")
	}

	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()

	var startLSN interface{}
	if c.currentLSN != "" {
		startLSN = c.currentLSN
	}

	rows, err := c.pool.Query(ctx,
		"SELECT lsn, xid, data FROM pg_logical_slot_get_changes($1, $2, $3)",
		c.config.SlotName,
		startLSN,
		c.config.MaxChanges,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var lsn string
		var xid uint32
		var data string

		if err := rows.Scan(&lsn, &xid, &data); err != nil {
			return err
		}
		c.currentLSN = lsn

		events, err := parseWal2JSON(data, xid, lsn)
		if err != nil {
			log.Printf("PostgreSQL CDC parse error: %v", err)
			continue
		}

		for _, event := range events {
			if err := c.processChangeEvent(event); err != nil {
				log.Printf("PostgreSQL CDC process error: %v", err)
			}
		}
	}

	return rows.Err()
}

func (c *CDCSource) ensureSlot(ctx context.Context) error {
	_, err := c.pool.Exec(ctx, "SELECT * FROM pg_create_logical_replication_slot($1, $2)", c.config.SlotName, c.config.Plugin)
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "already exists") {
		return nil
	}
	return err
}

type wal2jsonMessage struct {
	Xid       uint32           `json:"xid"`
	Timestamp string           `json:"timestamp"`
	Change    []wal2jsonChange `json:"change"`
}

type wal2jsonChange struct {
	Kind         string        `json:"kind"`
	Schema       string        `json:"schema"`
	Table        string        `json:"table"`
	ColumnNames  []string      `json:"columnnames"`
	ColumnValues []interface{} `json:"columnvalues"`
	OldKeys      *wal2jsonKeys `json:"oldkeys"`
}

type wal2jsonKeys struct {
	KeyNames  []string      `json:"keynames"`
	KeyValues []interface{} `json:"keyvalues"`
}

func parseWal2JSON(payload string, xid uint32, lsn string) ([]*ChangeEvent, error) {
	var msg wal2jsonMessage
	if err := json.Unmarshal([]byte(payload), &msg); err != nil {
		return nil, err
	}

	var ts time.Time
	if msg.Timestamp != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, msg.Timestamp); err == nil {
			ts = parsed
		}
	}
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	var events []*ChangeEvent
	for _, change := range msg.Change {
		after := columnsToMap(change.ColumnNames, change.ColumnValues)
		before := map[string]interface{}{}
		if change.OldKeys != nil {
			before = columnsToMap(change.OldKeys.KeyNames, change.OldKeys.KeyValues)
		}

		operation := strings.ToUpper(change.Kind)
		events = append(events, &ChangeEvent{
			Operation: operation,
			Schema:    change.Schema,
			Table:     change.Table,
			Before:    before,
			After:     after,
			LSN:       lsn,
			Timestamp: ts,
			TxID:      xid,
		})
	}

	return events, nil
}

func columnsToMap(names []string, values []interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for i, name := range names {
		if i < len(values) {
			result[name] = values[i]
		}
	}
	return result
}

func (c *CDCSource) processChangeEvent(change *ChangeEvent) error {
	// Filter by operations
	if !c.isOperationEnabled(change.Operation) {
		return nil
	}

	// Filter by tables if configured
	if len(c.config.Tables) > 0 && !c.isTableEnabled(change.Table) {
		return nil
	}

	// Transform to TypedFact
	fact, err := c.transformer.TransformChange(change)
	if err != nil {
		return fmt.Errorf("failed to transform change: %w", err)
	}

	if fact != nil && c.factChan != nil {
		select {
		case c.factChan <- fact:
			c.metrics.RecordFactProcessed(c.config.SourceID, fact.SchemaName)
		case <-c.ctx.Done():
			return nil
		default:
			c.metrics.RecordError(c.config.SourceID, "channel_full", fmt.Errorf("fact channel full"))
		}
	}

	return nil
}

func (c *CDCSource) isOperationEnabled(operation string) bool {
	for _, op := range c.config.Operations {
		if strings.EqualFold(op, operation) {
			return true
		}
	}
	return false
}

func (c *CDCSource) isTableEnabled(table string) bool {
	for _, t := range c.config.Tables {
		if t == table {
			return true
		}
	}
	return false
}

// ChangeTransformer transforms database changes to TypedFacts
type ChangeTransformer struct {
	config *CDCConfig
}

func NewChangeTransformer(config *CDCConfig) *ChangeTransformer {
	return &ChangeTransformer{config: config}
}

func (t *ChangeTransformer) TransformChange(change *ChangeEvent) (*adapters.TypedFact, error) {
	// Map table to schema if configured
	schemaKey := fmt.Sprintf("%s.%s", change.Schema, change.Table)
	schemaName := schemaKey
	if mappedSchema, exists := t.config.SchemaMapping[schemaKey]; exists {
		schemaName = mappedSchema
	} else if mappedSchema, exists := t.config.SchemaMapping[change.Table]; exists {
		schemaName = mappedSchema
	}

	payload := map[string]interface{}{
		"operation": change.Operation,
		"schema":    change.Schema,
		"table":     change.Table,
		"before":    change.Before,
		"after":     change.After,
		"lsn":       change.LSN,
		"timestamp": change.Timestamp.Format(time.RFC3339Nano),
		"tx_id":     change.TxID,
	}

	structData, err := structpb.NewStruct(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to build struct payload: %w", err)
	}

	rawData, err := json.Marshal(change)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal change event: %w", err)
	}

	return &adapters.TypedFact{
		SchemaName:    schemaName,
		SchemaVersion: "v1.0.0",
		Data:          structData,
		RawData:       rawData,
		Timestamp:     change.Timestamp,
		SourceID:      t.config.SourceID,
		TraceID:       "",
		Metadata: map[string]string{
			"pg.operation": change.Operation,
			"pg.schema":    change.Schema,
			"pg.table":     change.Table,
			"pg.lsn":       change.LSN,
			"pg.tx_id":     fmt.Sprintf("%d", change.TxID),
			"source_type":  "postgres_cdc",
		},
	}, nil
}

// Factory for PostgreSQL CDC sources
type CDCFactory struct{}

func (f *CDCFactory) Create(config adapters.SourceConfig) (adapters.FactSource, error) {
	cdcConfig := &CDCConfig{
		SourceID:   config.SourceID,
		SourceType: config.Type,
		Transforms: config.Transforms,
	}

	// Extract PostgreSQL CDC-specific configuration
	if connStr, ok := config.Config["connection_string"].(string); ok {
		cdcConfig.ConnectionString = connStr
	}
	if slotName, ok := config.Config["slot_name"].(string); ok {
		cdcConfig.SlotName = slotName
	}
	if pubName, ok := config.Config["publication_name"].(string); ok {
		cdcConfig.PublicationName = pubName
	}
	if tables, ok := config.Config["tables"].([]interface{}); ok {
		cdcConfig.Tables = make([]string, len(tables))
		for i, table := range tables {
			if tableStr, ok := table.(string); ok {
				cdcConfig.Tables[i] = tableStr
			}
		}
	}
	if ops, ok := config.Config["operations"].([]interface{}); ok {
		cdcConfig.Operations = make([]string, len(ops))
		for i, op := range ops {
			if opStr, ok := op.(string); ok {
				cdcConfig.Operations[i] = opStr
			}
		}
	}
	if mapping, ok := config.Config["schema_mapping"].(map[string]interface{}); ok {
		cdcConfig.SchemaMapping = make(map[string]string)
		for k, v := range mapping {
			if vStr, ok := v.(string); ok {
				cdcConfig.SchemaMapping[k] = vStr
			}
		}
	}
	if startLSN, ok := config.Config["start_lsn"].(string); ok {
		cdcConfig.StartLSN = startLSN
	}
	if batchSize, ok := config.Config["batch_size"].(float64); ok {
		cdcConfig.BatchSize = int(batchSize)
	}
	if heartbeat, ok := config.Config["heartbeat_interval_sec"].(float64); ok {
		cdcConfig.HeartbeatIntervalSec = int(heartbeat)
	}
	if bufferSize, ok := config.Config["buffer_size"].(float64); ok {
		cdcConfig.BufferSize = int(bufferSize)
	}
	if createSlot, ok := config.Config["create_slot"].(bool); ok {
		cdcConfig.CreateSlot = createSlot
	} else {
		cdcConfig.CreateSlot = true
	}
	if plugin, ok := config.Config["plugin"].(string); ok {
		cdcConfig.Plugin = plugin
	}
	if pollInterval, ok := config.Config["poll_interval"].(string); ok {
		if parsed, err := time.ParseDuration(pollInterval); err == nil {
			cdcConfig.PollInterval = parsed
		}
	}
	if maxChanges, ok := config.Config["max_changes"].(float64); ok {
		cdcConfig.MaxChanges = int(maxChanges)
	}
	if maxChanges, ok := config.Config["max_changes"].(int); ok {
		cdcConfig.MaxChanges = maxChanges
	}

	return NewCDCSource(cdcConfig)
}

func (f *CDCFactory) ValidateConfig(config adapters.SourceConfig) error {
	if _, ok := config.Config["connection_string"]; !ok {
		return fmt.Errorf("connection_string is required for postgres_cdc source")
	}
	return nil
}

func (f *CDCFactory) GetConfigSchema() adapters.ConfigSchema {
	return adapters.ConfigSchema{
		Properties: map[string]adapters.ConfigProperty{
			"connection_string": {
				Type:        "string",
				Description: "PostgreSQL connection string",
				Examples:    []string{"postgres://user:pass@localhost:5432/db"},
			},
			"slot_name": {
				Type:        "string",
				Description: "Logical replication slot name (auto-generated if not provided)",
			},
			"plugin": {
				Type:        "string",
				Description: "Logical decoding plugin (default: wal2json)",
			},
			"create_slot": {
				Type:        "bool",
				Description: "Create replication slot if missing",
				Default:     true,
			},
			"publication_name": {
				Type:        "string",
				Description: "Publication name for logical replication (auto-generated if not provided)",
			},
			"tables": {
				Type:        "array",
				Description: "List of tables to monitor (all tables if not specified)",
				Examples:    []string{`["users", "orders", "products"]`},
			},
			"operations": {
				Type:        "array",
				Description: "Database operations to capture",
				Default:     []string{"INSERT", "UPDATE", "DELETE"},
				Examples:    []string{`["INSERT", "UPDATE"]`},
			},
			"schema_mapping": {
				Type:        "object",
				Description: "Map database tables to schema names",
				Examples:    []string{`{"users": "user_profile", "orders": "order_event"}`},
			},
			"start_lsn": {
				Type:        "string",
				Description: "Starting LSN for replication (optional)",
				Examples:    []string{"0/1234567"},
			},
			"batch_size": {
				Type:        "integer",
				Description: "Number of events to batch together",
				Default:     100,
			},
			"heartbeat_interval_sec": {
				Type:        "integer",
				Description: "Heartbeat interval in seconds",
				Default:     30,
			},
			"poll_interval": {
				Type:        "string",
				Description: "Polling interval for fetching changes (e.g., 2s, 5s)",
			},
			"max_changes": {
				Type:        "integer",
				Description: "Maximum changes to fetch per poll",
				Default:     100,
			},
			"buffer_size": {
				Type:        "integer",
				Description: "Channel buffer size for facts",
				Default:     1000,
			},
		},
		Required: []string{"connection_string"},
	}
}

func init() {
	adapters.RegisterSourceType("postgres_cdc", &CDCFactory{})
}
