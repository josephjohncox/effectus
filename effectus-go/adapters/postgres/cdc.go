package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/effectus/effectus-go/adapters"
)

// CDCConfig holds PostgreSQL CDC configuration
type CDCConfig struct {
	adapters.BaseConfig  `yaml:",inline"`
	ConnectionString     string            `json:"connection_string" yaml:"connection_string"`
	SlotName             string            `json:"slot_name" yaml:"slot_name"`
	PublicationName      string            `json:"publication_name" yaml:"publication_name"`
	Tables               []string          `json:"tables" yaml:"tables"`
	Operations           []string          `json:"operations" yaml:"operations"` // INSERT, UPDATE, DELETE
	SchemaMapping        map[string]string `json:"schema_mapping" yaml:"schema_mapping"`
	StartLSN             string            `json:"start_lsn" yaml:"start_lsn"`
	BatchSize            int               `json:"batch_size" yaml:"batch_size"`
	HeartbeatIntervalSec int               `json:"heartbeat_interval_sec" yaml:"heartbeat_interval_sec"`
}

// CDCSource implements Change Data Capture for PostgreSQL
type CDCSource struct {
	config      *CDCConfig
	pool        *pgxpool.Pool
	replConn    *pgconn.PgConn
	factChan    chan<- *adapters.TypedFact
	transformer *ChangeTransformer
	metrics     adapters.SourceMetrics
	ctx         context.Context
	cancel      context.CancelFunc
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
	if config.BatchSize == 0 {
		config.BatchSize = 100
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
		metrics:     adapters.NewSourceMetrics(config.SourceID),
		ctx:         ctx,
		cancel:      cancel,
	}

	return source, nil
}

func (c *CDCSource) Start(factChan chan<- *adapters.TypedFact) error {
	c.factChan = factChan

	// Create connection pool
	poolConfig, err := pgxpool.ParseConfig(c.config.ConnectionString)
	if err != nil {
		return fmt.Errorf("failed to parse connection string: %w", err)
	}

	c.pool, err = pgxpool.NewWithConfig(c.ctx, poolConfig)
	if err != nil {
		return fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Test connection
	if err := c.pool.Ping(c.ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	log.Printf("PostgreSQL CDC source started for slot: %s", c.config.SlotName)

	// Setup replication
	if err := c.setupReplication(); err != nil {
		return fmt.Errorf("failed to setup replication: %w", err)
	}

	// Start consuming changes
	go c.consumeChanges()

	return nil
}

func (c *CDCSource) Stop() error {
	c.cancel()

	if c.replConn != nil {
		c.replConn.Close(c.ctx)
	}

	if c.pool != nil {
		c.pool.Close()
	}

	log.Printf("PostgreSQL CDC source stopped")
	return nil
}

func (c *CDCSource) GetMetrics() adapters.SourceMetrics {
	return c.metrics
}

func (c *CDCSource) setupReplication() error {
	// Connect with replication mode
	replConfig, err := pgconn.ParseConfig(c.config.ConnectionString + " replication=database")
	if err != nil {
		return fmt.Errorf("failed to parse replication config: %w", err)
	}

	c.replConn, err = pgconn.ConnectConfig(c.ctx, replConfig)
	if err != nil {
		return fmt.Errorf("failed to create replication connection: %w", err)
	}

	// Create publication if specified tables
	if len(c.config.Tables) > 0 {
		if err := c.createPublication(); err != nil {
			log.Printf("Warning: failed to create publication: %v", err)
		}
	}

	// Create replication slot
	if err := c.createReplicationSlot(); err != nil {
		log.Printf("Warning: failed to create replication slot: %v", err)
	}

	return nil
}

func (c *CDCSource) createPublication() error {
	conn, err := c.pool.Acquire(c.ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	// Drop existing publication
	_, err = conn.Exec(c.ctx, fmt.Sprintf("DROP PUBLICATION IF EXISTS %s", c.config.PublicationName))
	if err != nil {
		log.Printf("Warning: failed to drop existing publication: %v", err)
	}

	// Create publication for specified tables
	tableList := strings.Join(c.config.Tables, ", ")
	query := fmt.Sprintf("CREATE PUBLICATION %s FOR TABLE %s", c.config.PublicationName, tableList)

	_, err = conn.Exec(c.ctx, query)
	if err != nil {
		return fmt.Errorf("failed to create publication: %w", err)
	}

	log.Printf("Created publication: %s for tables: %s", c.config.PublicationName, tableList)
	return nil
}

func (c *CDCSource) createReplicationSlot() error {
	query := fmt.Sprintf("SELECT pg_create_logical_replication_slot('%s', 'pgoutput')", c.config.SlotName)

	result := c.replConn.Exec(c.ctx, query)
	_, err := result.ReadAll()
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return fmt.Errorf("failed to create replication slot: %w", err)
	}

	log.Printf("Replication slot ready: %s", c.config.SlotName)
	return nil
}

func (c *CDCSource) consumeChanges() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CDC consumer panic: %v", r)
			c.metrics.RecordError(c.config.SourceID, "panic", fmt.Errorf("consumer panic: %v", r))
		}
	}()

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			if err := c.startReplication(); err != nil {
				c.metrics.RecordError(c.config.SourceID, "replication", err)
				log.Printf("Replication error: %v", err)

				// Backoff before reconnecting
				select {
				case <-c.ctx.Done():
					return
				case <-time.After(5 * time.Second):
					continue
				}
			}
		}
	}
}

func (c *CDCSource) startReplication() error {
	options := fmt.Sprintf("proto_version '1', publication_names '%s'", c.config.PublicationName)
	if c.config.StartLSN != "" {
		options += fmt.Sprintf(", start_lsn '%s'", c.config.StartLSN)
	}

	query := fmt.Sprintf("START_REPLICATION SLOT %s LOGICAL 0/0 (%s)", c.config.SlotName, options)
	result := c.replConn.Exec(c.ctx, query)

	heartbeatTicker := time.NewTicker(time.Duration(c.config.HeartbeatIntervalSec) * time.Second)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return nil
		case <-heartbeatTicker.C:
			// Send keepalive
			if err := c.sendHeartbeat(); err != nil {
				log.Printf("Heartbeat failed: %v", err)
			}
		default:
			msg, err := result.NextMsg()
			if err != nil {
				return fmt.Errorf("failed to get next message: %w", err)
			}

			if msg == nil {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			if err := c.processReplicationMessage(msg); err != nil {
				log.Printf("Failed to process message: %v", err)
			}
		}
	}
}

func (c *CDCSource) sendHeartbeat() error {
	// Simple implementation - in production you'd track LSN progress
	return nil
}

func (c *CDCSource) processReplicationMessage(msg *pgconn.ReplicationMessage) error {
	if msg.WalMessage == nil {
		return nil
	}

	// In a production implementation, you would decode the actual WAL data
	// For now, we'll create a mock change event
	change := &ChangeEvent{
		Operation: "INSERT", // Would be parsed from WAL
		Schema:    "public",
		Table:     "events", // Would be parsed from WAL
		After: map[string]interface{}{
			"id":         1,
			"event_type": "user_action",
			"data":       `{"user_id": 123, "action": "login"}`,
		},
		LSN:       msg.WalMessage.WalStart.String(),
		Timestamp: time.Now(),
		TxID:      0, // Would be extracted from WAL
	}

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

	if fact != nil {
		select {
		case c.factChan <- fact:
			c.metrics.RecordMessage(c.config.SourceID, len(msg.WalMessage.WalData))
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
	schemaName := fmt.Sprintf("%s.%s", change.Schema, change.Table)
	if mappedSchema, exists := t.config.SchemaMapping[change.Table]; exists {
		schemaName = mappedSchema
	}

	// Serialize change event
	data, err := json.Marshal(change)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal change event: %w", err)
	}

	return &adapters.TypedFact{
		SchemaName:    schemaName,
		SchemaVersion: "v1.0.0",
		Data:          nil, // Would contain proto message in real implementation
		RawData:       data,
		Timestamp:     change.Timestamp,
		SourceID:      t.config.SourceID,
		TraceID:       "", // Could extract from transaction context
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

func (f *CDCFactory) CreateSource(config adapters.SourceConfig) (adapters.FactSource, error) {
	cdcConfig := &CDCConfig{
		BaseConfig: adapters.BaseConfig{
			SourceID:   config.SourceID,
			SourceType: config.SourceType,
			Enabled:    config.Enabled,
			BufferSize: config.BufferSize,
			Transforms: config.Transforms,
		},
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
		Type:        "postgres_cdc",
		Description: "PostgreSQL Change Data Capture source using logical replication",
		Properties: map[string]adapters.ConfigProperty{
			"connection_string": {
				Type:        "string",
				Description: "PostgreSQL connection string",
				Required:    true,
				Examples:    []string{"postgres://user:pass@localhost:5432/db"},
			},
			"slot_name": {
				Type:        "string",
				Description: "Logical replication slot name (auto-generated if not provided)",
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
				Default:     `["INSERT", "UPDATE", "DELETE"]`,
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
				Description: "Batch size for processing changes",
				Default:     "100",
			},
			"heartbeat_interval_sec": {
				Type:        "integer",
				Description: "Heartbeat interval in seconds",
				Default:     "30",
			},
		},
		Required: []string{"connection_string"},
	}
}

func init() {
	adapters.RegisterSourceType("postgres_cdc", &CDCFactory{})
}
