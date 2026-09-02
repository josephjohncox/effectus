package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/josephjohncox/effectus/internal/adapters"
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
	stopping    bool
	currentLSN  string
	done        chan struct{}
	mu          sync.Mutex
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

	source := &CDCSource{
		config:      config,
		transformer: NewChangeTransformer(config),
		metrics:     adapters.GetGlobalMetrics(),
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
	c.mu.Lock()
	if c.running {
		ch := c.factChan
		c.mu.Unlock()
		return ch, nil
	}
	c.mu.Unlock()

	if err := c.Start(ctx); err != nil {
		return nil, err
	}
	c.mu.Lock()
	ch := c.factChan
	c.mu.Unlock()
	return ch, nil
}

func (c *CDCSource) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("source already running")
	}

	poolConfig, err := pgxpool.ParseConfig(c.config.ConnectionString)
	if err != nil {
		c.mu.Unlock()
		return fmt.Errorf("failed to parse connection string: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		c.mu.Unlock()
		return fmt.Errorf("failed to create connection pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		c.mu.Unlock()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	workerCtx, cancel := context.WithCancel(ctx)
	c.pool = pool
	c.ctx = workerCtx
	c.cancel = cancel
	c.factChan = make(chan *adapters.TypedFact, c.config.BufferSize)
	c.done = make(chan struct{})
	c.running = true
	c.stopping = false

	if c.config.CreateSlot {
		if err := c.ensureSlot(ctx); err != nil {
			c.running = false
			cancel()
			pool.Close()
			close(c.factChan)
			close(c.done)
			c.mu.Unlock()
			return err
		}
	}
	if c.config.StartLSN != "" {
		if _, err := pool.Exec(ctx, "SELECT pg_replication_slot_advance($1, $2::pg_lsn)", c.config.SlotName, c.config.StartLSN); err != nil {
			c.running = false
			cancel()
			pool.Close()
			close(c.factChan)
			close(c.done)
			c.mu.Unlock()
			return fmt.Errorf("advance replication slot to start_lsn: %w", err)
		}
		c.currentLSN = c.config.StartLSN
		// StartLSN is an initialization instruction, never a polling bound.
		c.config.StartLSN = ""
	}

	factChan := c.factChan
	done := c.done
	c.mu.Unlock()

	go c.pollChanges(workerCtx, pool, factChan, done)
	log.Printf("PostgreSQL CDC source started for slot: %s", c.config.SlotName)
	return nil
}

func (c *CDCSource) Stop(ctx context.Context) error {
	c.mu.Lock()
	done := c.done
	if !c.running {
		c.mu.Unlock()
		return waitForCDCStop(ctx, done)
	}
	if c.stopping {
		c.mu.Unlock()
		if err := waitForCDCStop(ctx, done); err != nil {
			return err
		}
		c.finishStop(done)
		return nil
	}
	c.stopping = true
	cancel, pool := c.cancel, c.pool
	c.mu.Unlock()

	cancel()
	if pool != nil {
		pool.Close()
	}
	if err := waitForCDCStop(ctx, done); err != nil {
		go func() {
			<-done
			c.finishStop(done)
		}()
		return err
	}
	c.finishStop(done)
	log.Printf("PostgreSQL CDC source stopped")
	return nil
}

func waitForCDCStop(ctx context.Context, done <-chan struct{}) error {
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *CDCSource) finishStop(done <-chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done != done {
		return
	}
	c.running = false
	c.stopping = false
	c.pool = nil
	c.ctx = nil
	c.cancel = nil
}

func (c *CDCSource) GetSourceSchema() *adapters.Schema {
	return c.schema
}

func (c *CDCSource) HealthCheck() error {
	c.mu.Lock()
	pool, workerCtx := c.pool, c.ctx
	c.mu.Unlock()
	if pool == nil || workerCtx == nil {
		return fmt.Errorf("connection pool not initialized")
	}
	return pool.Ping(workerCtx)
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

func (c *CDCSource) pollChanges(ctx context.Context, pool *pgxpool.Pool, factChan chan *adapters.TypedFact, done chan struct{}) {
	defer close(done)
	defer close(factChan)

	ticker := time.NewTicker(c.config.PollInterval)
	defer ticker.Stop()

	if err := c.fetchChanges(ctx, pool, factChan); err != nil && ctx.Err() == nil {
		log.Printf("PostgreSQL CDC initial fetch failed: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.fetchChanges(ctx, pool, factChan); err != nil && ctx.Err() == nil {
				log.Printf("PostgreSQL CDC fetch failed: %v", err)
			}
		}
	}
}

type preparedWALRecord struct {
	lsn   string
	facts []*adapters.TypedFact
}

func (c *CDCSource) fetchChanges(ctx context.Context, pool *pgxpool.Pool, factChan chan<- *adapters.TypedFact) error {
	if pool == nil {
		return fmt.Errorf("connection pool not initialized")
	}

	// Peeking is deliberately non-advancing. The slot is advanced only after
	// every fact from a WAL record reaches the caller's durable boundary.
	rows, err := pool.Query(ctx,
		"SELECT lsn, xid, data FROM pg_logical_slot_peek_changes($1, NULL, $2)",
		c.config.SlotName,
		c.config.MaxChanges,
	)
	if err != nil {
		return err
	}

	var batch []preparedWALRecord
	for rows.Next() {
		var lsn string
		var xid uint32
		var data string
		if err := rows.Scan(&lsn, &xid, &data); err != nil {
			rows.Close()
			return err
		}

		events, err := parseWal2JSON(data, xid, lsn)
		if err != nil {
			rows.Close()
			return fmt.Errorf("parse WAL record at %s: %w", lsn, err)
		}
		record := preparedWALRecord{lsn: lsn}
		for _, event := range events {
			fact, err := c.prepareChangeEvent(event)
			if err != nil {
				rows.Close()
				return err
			}
			if fact != nil {
				record.facts = append(record.facts, fact)
			}
		}
		batch = append(batch, record)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, record := range batch {
		if len(record.facts) == 0 {
			if err := c.advanceSlot(ctx, pool, record.lsn); err != nil {
				return err
			}
			continue
		}
		barrier := adapters.NewAcknowledgementBarrier(len(record.facts), func(ackCtx context.Context) error {
			return c.advanceSlot(ackCtx, pool, record.lsn)
		})
		for index, fact := range record.facts {
			fact.Acknowledge = barrier.Callback(index)
			select {
			case factChan <- fact:
				c.metrics.RecordFactProcessed(c.config.SourceID, fact.SchemaName)
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if err := barrier.Wait(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (c *CDCSource) advanceSlot(ctx context.Context, pool *pgxpool.Pool, lsn string) error {
	if _, err := pool.Exec(ctx,
		"SELECT pg_replication_slot_advance($1, $2::pg_lsn)",
		c.config.SlotName, lsn,
	); err != nil {
		return fmt.Errorf("advance replication slot through %s: %w", lsn, err)
	}
	c.mu.Lock()
	c.currentLSN = lsn
	c.mu.Unlock()
	return nil
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

func (c *CDCSource) prepareChangeEvent(change *ChangeEvent) (*adapters.TypedFact, error) {
	if !c.isOperationEnabled(change.Operation) {
		return nil, nil
	}
	if len(c.config.Tables) > 0 && !c.isTableEnabled(change.Table) {
		return nil, nil
	}
	fact, err := c.transformer.TransformChange(change)
	if err != nil {
		return nil, fmt.Errorf("failed to transform change at %s: %w", change.LSN, err)
	}
	return fact, nil
}

func (c *CDCSource) processChangeEvent(change *ChangeEvent) error {
	fact, err := c.prepareChangeEvent(change)
	if err != nil || fact == nil {
		return err
	}
	select {
	case c.factChan <- fact:
		c.metrics.RecordFactProcessed(c.config.SourceID, fact.SchemaName)
		return nil
	case <-c.ctx.Done():
		return c.ctx.Err()
	}
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
