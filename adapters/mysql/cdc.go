package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-mysql-org/go-mysql/replication"
	_ "github.com/go-sql-driver/mysql"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/effectus/effectus-go/adapters"
)

// CDCConfig holds MySQL CDC configuration.
type CDCConfig struct {
	SourceID      string            `json:"source_id" yaml:"source_id"`
	Host          string            `json:"host" yaml:"host"`
	Port          int               `json:"port" yaml:"port"`
	User          string            `json:"user" yaml:"user"`
	Password      string            `json:"password" yaml:"password"`
	Flavor        string            `json:"flavor" yaml:"flavor"` // mysql or mariadb
	ServerID      uint32            `json:"server_id" yaml:"server_id"`
	Database      string            `json:"database" yaml:"database"`
	Tables        []string          `json:"tables" yaml:"tables"`
	Operations    []string          `json:"operations" yaml:"operations"`
	SchemaMapping map[string]string `json:"schema_mapping" yaml:"schema_mapping"`
	StartFile     string            `json:"start_file" yaml:"start_file"`
	StartPos      uint32            `json:"start_pos" yaml:"start_pos"`
	GTID          string            `json:"gtid" yaml:"gtid"`
	DSN           string            `json:"dsn" yaml:"dsn"`
	BufferSize    int               `json:"buffer_size" yaml:"buffer_size"`
	Timeout       time.Duration     `json:"timeout" yaml:"timeout"`
}

// CDCSource implements MySQL binlog streaming.
type CDCSource struct {
	config   *CDCConfig
	syncer   *replication.BinlogSyncer
	streamer *replication.BinlogStreamer
	db       *sql.DB
	factChan chan *adapters.TypedFact
	metrics  adapters.SourceMetrics
	ctx      context.Context
	cancel   context.CancelFunc
	schema   *adapters.Schema
	running  bool

	mu            sync.Mutex
	tableColumns  map[string][]string
	currentBinlog string
}

// ChangeEvent represents a MySQL change event.
type ChangeEvent struct {
	Operation string                 `json:"operation"`
	Schema    string                 `json:"schema"`
	Table     string                 `json:"table"`
	Before    map[string]interface{} `json:"before,omitempty"`
	After     map[string]interface{} `json:"after,omitempty"`
	Binlog    string                 `json:"binlog"`
	Pos       uint32                 `json:"pos"`
	Timestamp time.Time              `json:"timestamp"`
}

// NewCDCSource creates a new MySQL CDC source.
func NewCDCSource(config *CDCConfig) (*CDCSource, error) {
	if config == nil {
		return nil, fmt.Errorf("config is nil")
	}
	if config.Host == "" {
		return nil, fmt.Errorf("host is required")
	}
	if config.User == "" {
		return nil, fmt.Errorf("user is required")
	}
	if config.Port == 0 {
		config.Port = 3306
	}
	if config.Flavor == "" {
		config.Flavor = "mysql"
	}
	if config.ServerID == 0 {
		config.ServerID = 100
	}
	if config.BufferSize == 0 {
		config.BufferSize = 1000
	}
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}
	if len(config.Operations) == 0 {
		config.Operations = []string{"INSERT", "UPDATE", "DELETE"}
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &CDCSource{
		config:       config,
		metrics:      adapters.GetGlobalMetrics(),
		ctx:          ctx,
		cancel:       cancel,
		tableColumns: make(map[string][]string),
		schema: &adapters.Schema{
			Name:    "mysql_cdc",
			Version: "v1.0.0",
			Fields: map[string]interface{}{
				"operation": "string",
				"schema":    "string",
				"table":     "string",
				"before":    "object",
				"after":     "object",
				"binlog":    "string",
				"pos":       "uint32",
				"timestamp": "timestamp",
			},
		},
	}, nil
}

// Subscribe starts the CDC stream.
func (c *CDCSource) Subscribe(ctx context.Context, factTypes []string) (<-chan *adapters.TypedFact, error) {
	c.factChan = make(chan *adapters.TypedFact, c.config.BufferSize)
	if err := c.Start(ctx); err != nil {
		close(c.factChan)
		return nil, err
	}
	return c.factChan, nil
}

// Start opens connections and begins reading binlog events.
func (c *CDCSource) Start(ctx context.Context) error {
	if c.running {
		return fmt.Errorf("source already running")
	}

	db, err := sql.Open("mysql", c.schemaDSN())
	if err != nil {
		return fmt.Errorf("open schema db: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("ping schema db: %w", err)
	}
	c.db = db

	cfg := replication.BinlogSyncerConfig{
		ServerID: c.config.ServerID,
		Flavor:   c.config.Flavor,
		Host:     c.config.Host,
		Port:     uint16(c.config.Port),
		User:     c.config.User,
		Password: c.config.Password,
	}
	c.syncer = replication.NewBinlogSyncer(cfg)

	streamer, err := c.startStreamer(ctx)
	if err != nil {
		return err
	}
	c.streamer = streamer

	c.running = true
	go c.consumeEvents()

	log.Printf("MySQL CDC source started for %s:%d", c.config.Host, c.config.Port)
	return nil
}

// Stop stops the CDC stream.
func (c *CDCSource) Stop(ctx context.Context) error {
	if !c.running {
		return nil
	}
	c.cancel()
	c.running = false
	if c.syncer != nil {
		c.syncer.Close()
	}
	if c.db != nil {
		c.db.Close()
	}
	if c.factChan != nil {
		close(c.factChan)
	}
	log.Printf("MySQL CDC source stopped")
	return nil
}

// GetSourceSchema returns schema metadata.
func (c *CDCSource) GetSourceSchema() *adapters.Schema {
	return c.schema
}

// HealthCheck checks connectivity.
func (c *CDCSource) HealthCheck() error {
	if c.db == nil {
		return fmt.Errorf("schema db not initialized")
	}
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	return c.db.PingContext(ctx)
}

// GetMetadata returns source metadata.
func (c *CDCSource) GetMetadata() adapters.SourceMetadata {
	return adapters.SourceMetadata{
		SourceID:      c.config.SourceID,
		SourceType:    "mysql_cdc",
		Version:       "1.0.0",
		Capabilities:  []string{"streaming", "realtime"},
		SchemaFormats: []string{"json"},
		Config: map[string]string{
			"host":   c.config.Host,
			"port":   fmt.Sprintf("%d", c.config.Port),
			"db":     c.config.Database,
			"tables": strings.Join(c.config.Tables, ","),
		},
		Tags: []string{"database", "mysql", "cdc"},
	}
}

func (c *CDCSource) startStreamer(ctx context.Context) (*replication.BinlogStreamer, error) {
	if c.config.GTID != "" {
		gtidSet, err := mysql.ParseGTIDSet(c.config.Flavor, c.config.GTID)
		if err != nil {
			return nil, fmt.Errorf("parse gtid: %w", err)
		}
		return c.syncer.StartSyncGTID(gtidSet)
	}

	pos := mysql.Position{Name: c.config.StartFile, Pos: c.config.StartPos}
	if pos.Name == "" {
		status, err := c.masterStatus(ctx)
		if err != nil {
			return nil, err
		}
		if c.config.GTID != "" {
			gtidSet, err := mysql.ParseGTIDSet(c.config.Flavor, c.config.GTID)
			if err != nil {
				return nil, fmt.Errorf("parse gtid: %w", err)
			}
			return c.syncer.StartSyncGTID(gtidSet)
		}
		pos = status
	}
	c.currentBinlog = pos.Name
	return c.syncer.StartSync(pos)
}

func (c *CDCSource) masterStatus(ctx context.Context) (mysql.Position, error) {
	queryCtx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	row := c.db.QueryRowContext(queryCtx, "SHOW MASTER STATUS")
	var file string
	var position uint32
	var binlogDoDB, binlogIgnoreDB, gtidSet sql.NullString
	if err := row.Scan(&file, &position, &binlogDoDB, &binlogIgnoreDB, &gtidSet); err != nil {
		return mysql.Position{}, err
	}
	if c.config.GTID == "" && gtidSet.Valid {
		c.config.GTID = gtidSet.String
	}
	return mysql.Position{Name: file, Pos: position}, nil
}

func (c *CDCSource) consumeEvents() {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		ev, err := c.streamer.GetEvent(c.ctx)
		if err != nil {
			if c.ctx.Err() != nil {
				return
			}
			log.Printf("MySQL CDC stream error: %v", err)
			time.Sleep(1 * time.Second)
			continue
		}

		switch event := ev.Event.(type) {
		case *replication.RotateEvent:
			c.currentBinlog = string(event.NextLogName)
			continue
		}

		rowsEvent, ok := ev.Event.(*replication.RowsEvent)
		if !ok {
			continue
		}

		if err := c.handleRowsEvent(rowsEvent, ev.Header); err != nil {
			log.Printf("MySQL CDC handle error: %v", err)
		}
	}
}

func (c *CDCSource) handleRowsEvent(event *replication.RowsEvent, header *replication.EventHeader) error {
	if event.Table == nil {
		return nil
	}

	schemaName := string(event.Table.Schema)
	tableName := string(event.Table.Table)
	fullName := fmt.Sprintf("%s.%s", schemaName, tableName)

	if !c.isTableEnabled(fullName, tableName) {
		return nil
	}

	operation := operationForEvent(event, header)
	if !c.isOperationEnabled(operation) {
		return nil
	}

	columns := c.getColumns(event.Table, schemaName, tableName)

	switch header.EventType {
	case replication.WRITE_ROWS_EVENTv1, replication.WRITE_ROWS_EVENTv2:
		for _, row := range event.Rows {
			after := rowToMap(columns, row)
			c.emitChange(operation, schemaName, tableName, nil, after, header)
		}
	case replication.DELETE_ROWS_EVENTv1, replication.DELETE_ROWS_EVENTv2:
		for _, row := range event.Rows {
			before := rowToMap(columns, row)
			c.emitChange(operation, schemaName, tableName, before, nil, header)
		}
	case replication.UPDATE_ROWS_EVENTv1, replication.UPDATE_ROWS_EVENTv2:
		for i := 0; i+1 < len(event.Rows); i += 2 {
			before := rowToMap(columns, event.Rows[i])
			after := rowToMap(columns, event.Rows[i+1])
			c.emitChange(operation, schemaName, tableName, before, after, header)
		}
	}

	return nil
}

func (c *CDCSource) emitChange(operation, schemaName, tableName string, before, after map[string]interface{}, header *replication.EventHeader) {
	change := &ChangeEvent{
		Operation: operation,
		Schema:    schemaName,
		Table:     tableName,
		Before:    before,
		After:     after,
		Binlog:    c.currentBinlog,
		Pos:       header.LogPos,
		Timestamp: time.Unix(int64(header.Timestamp), 0).UTC(),
	}

	schemaKey := fmt.Sprintf("%s.%s", schemaName, tableName)
	mappedSchema := schemaKey
	if mapped, ok := c.config.SchemaMapping[schemaKey]; ok {
		mappedSchema = mapped
	} else if mapped, ok := c.config.SchemaMapping[tableName]; ok {
		mappedSchema = mapped
	}

	payload := map[string]interface{}{
		"operation": change.Operation,
		"schema":    change.Schema,
		"table":     change.Table,
		"before":    change.Before,
		"after":     change.After,
		"binlog":    change.Binlog,
		"pos":       change.Pos,
		"timestamp": change.Timestamp.Format(time.RFC3339Nano),
	}

	structData, err := structpb.NewStruct(payload)
	if err != nil {
		c.metrics.RecordError(c.config.SourceID, "struct_payload", err)
		return
	}

	rawData, err := json.Marshal(change)
	if err != nil {
		c.metrics.RecordError(c.config.SourceID, "marshal", err)
		return
	}

	fact := &adapters.TypedFact{
		SchemaName:    mappedSchema,
		SchemaVersion: "v1.0.0",
		Data:          structData,
		RawData:       rawData,
		Timestamp:     change.Timestamp,
		SourceID:      c.config.SourceID,
		Metadata: map[string]string{
			"mysql.operation": operation,
			"mysql.schema":    schemaName,
			"mysql.table":     tableName,
			"mysql.pos":       fmt.Sprintf("%d", change.Pos),
			"source_type":     "mysql_cdc",
		},
	}

	select {
	case c.factChan <- fact:
		c.metrics.RecordFactProcessed(c.config.SourceID, mappedSchema)
	case <-c.ctx.Done():
	default:
		c.metrics.RecordError(c.config.SourceID, "channel_full", fmt.Errorf("fact channel full"))
	}
}

func (c *CDCSource) isTableEnabled(fullName, table string) bool {
	if len(c.config.Tables) == 0 {
		return true
	}
	for _, t := range c.config.Tables {
		if t == fullName || t == table {
			return true
		}
	}
	return false
}

func (c *CDCSource) isOperationEnabled(operation string) bool {
	for _, op := range c.config.Operations {
		if strings.EqualFold(op, operation) {
			return true
		}
	}
	return false
}

func operationForEvent(event *replication.RowsEvent, header *replication.EventHeader) string {
	switch header.EventType {
	case replication.WRITE_ROWS_EVENTv1, replication.WRITE_ROWS_EVENTv2:
		return "INSERT"
	case replication.UPDATE_ROWS_EVENTv1, replication.UPDATE_ROWS_EVENTv2:
		return "UPDATE"
	case replication.DELETE_ROWS_EVENTv1, replication.DELETE_ROWS_EVENTv2:
		return "DELETE"
	default:
	}
	return "UNKNOWN"
}

func (c *CDCSource) getColumns(table *replication.TableMapEvent, schemaName, tableName string) []string {
	key := fmt.Sprintf("%s.%s", schemaName, tableName)
	c.mu.Lock()
	if cols, ok := c.tableColumns[key]; ok {
		c.mu.Unlock()
		return cols
	}
	c.mu.Unlock()

	var columns []string
	if len(table.ColumnName) > 0 {
		for _, name := range table.ColumnName {
			columns = append(columns, string(name))
		}
	}
	if len(columns) == 0 {
		cols, err := c.fetchColumns(schemaName, tableName)
		if err == nil && len(cols) > 0 {
			columns = cols
		}
	}
	if len(columns) == 0 {
		columns = fallbackColumnNames(table.ColumnCount)
	}

	c.mu.Lock()
	c.tableColumns[key] = columns
	c.mu.Unlock()
	return columns
}

func (c *CDCSource) fetchColumns(schemaName, tableName string) ([]string, error) {
	query := "SELECT COLUMN_NAME FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION"
	rows, err := c.db.QueryContext(c.ctx, query, schemaName, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		columns = append(columns, name)
	}
	return columns, rows.Err()
}

func fallbackColumnNames(count uint64) []string {
	columns := make([]string, count)
	for i := range columns {
		columns[i] = fmt.Sprintf("col_%d", i)
	}
	return columns
}

func rowToMap(columns []string, row []interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for i, value := range row {
		name := fmt.Sprintf("col_%d", i)
		if i < len(columns) {
			name = columns[i]
		}
		result[name] = value
	}
	return result
}

func (c *CDCSource) schemaDSN() string {
	if c.config.DSN != "" {
		return c.config.DSN
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/information_schema?parseTime=true",
		c.config.User,
		c.config.Password,
		c.config.Host,
		c.config.Port,
	)
}

// Factory for MySQL CDC sources.
type CDCFactory struct{}

func (f *CDCFactory) Create(config adapters.SourceConfig) (adapters.FactSource, error) {
	cdcConfig := &CDCConfig{
		SourceID:      config.SourceID,
		SchemaMapping: make(map[string]string),
	}

	if v, ok := config.Config["host"].(string); ok {
		cdcConfig.Host = v
	}
	if v, ok := config.Config["port"].(float64); ok {
		cdcConfig.Port = int(v)
	}
	if v, ok := config.Config["port"].(int); ok {
		cdcConfig.Port = v
	}
	if v, ok := config.Config["user"].(string); ok {
		cdcConfig.User = v
	}
	if v, ok := config.Config["password"].(string); ok {
		cdcConfig.Password = v
	}
	if v, ok := config.Config["flavor"].(string); ok {
		cdcConfig.Flavor = v
	}
	if v, ok := config.Config["server_id"].(float64); ok {
		cdcConfig.ServerID = uint32(v)
	}
	if v, ok := config.Config["server_id"].(int); ok {
		cdcConfig.ServerID = uint32(v)
	}
	if v, ok := config.Config["database"].(string); ok {
		cdcConfig.Database = v
	}
	if v, ok := config.Config["tables"].([]interface{}); ok {
		for _, t := range v {
			if str, ok := t.(string); ok {
				cdcConfig.Tables = append(cdcConfig.Tables, str)
			}
		}
	}
	if v, ok := config.Config["operations"].([]interface{}); ok {
		for _, op := range v {
			if str, ok := op.(string); ok {
				cdcConfig.Operations = append(cdcConfig.Operations, str)
			}
		}
	}
	if v, ok := config.Config["schema_mapping"].(map[string]interface{}); ok {
		for key, value := range v {
			if str, ok := value.(string); ok {
				cdcConfig.SchemaMapping[key] = str
			}
		}
	}
	if v, ok := config.Config["start_file"].(string); ok {
		cdcConfig.StartFile = v
	}
	if v, ok := config.Config["start_pos"].(float64); ok {
		cdcConfig.StartPos = uint32(v)
	}
	if v, ok := config.Config["start_pos"].(int); ok {
		cdcConfig.StartPos = uint32(v)
	}
	if v, ok := config.Config["gtid"].(string); ok {
		cdcConfig.GTID = v
	}
	if v, ok := config.Config["dsn"].(string); ok {
		cdcConfig.DSN = v
	}
	if v, ok := config.Config["buffer_size"].(float64); ok {
		cdcConfig.BufferSize = int(v)
	}
	if v, ok := config.Config["timeout"].(string); ok {
		if parsed, err := time.ParseDuration(v); err == nil {
			cdcConfig.Timeout = parsed
		}
	}

	return NewCDCSource(cdcConfig)
}

func (f *CDCFactory) ValidateConfig(config adapters.SourceConfig) error {
	if _, ok := config.Config["host"]; !ok {
		return fmt.Errorf("host is required for mysql_cdc source")
	}
	if _, ok := config.Config["user"]; !ok {
		return fmt.Errorf("user is required for mysql_cdc source")
	}
	return nil
}

func (f *CDCFactory) GetConfigSchema() adapters.ConfigSchema {
	return adapters.ConfigSchema{
		Properties: map[string]adapters.ConfigProperty{
			"host": {
				Type:        "string",
				Description: "MySQL host",
			},
			"port": {
				Type:        "int",
				Description: "MySQL port",
				Default:     3306,
			},
			"user": {
				Type:        "string",
				Description: "MySQL user",
			},
			"password": {
				Type:        "string",
				Description: "MySQL password",
			},
			"flavor": {
				Type:        "string",
				Description: "mysql or mariadb",
				Default:     "mysql",
			},
			"server_id": {
				Type:        "int",
				Description: "Replication server ID",
				Default:     100,
			},
			"database": {
				Type:        "string",
				Description: "Default database schema (optional)",
			},
			"tables": {
				Type:        "array",
				Description: "Tables to monitor (schema.table or table)",
			},
			"operations": {
				Type:        "array",
				Description: "Operations to capture",
				Default:     []string{"INSERT", "UPDATE", "DELETE"},
			},
			"schema_mapping": {
				Type:        "object",
				Description: "Map table name to Effectus schema name",
			},
			"start_file": {
				Type:        "string",
				Description: "Binlog filename to start from",
			},
			"start_pos": {
				Type:        "int",
				Description: "Binlog position to start from",
			},
			"gtid": {
				Type:        "string",
				Description: "GTID set to start from",
			},
			"dsn": {
				Type:        "string",
				Description: "Optional DSN for schema metadata queries",
			},
			"buffer_size": {
				Type:        "int",
				Description: "Channel buffer size for facts",
				Default:     1000,
			},
			"timeout": {
				Type:        "string",
				Description: "Connection timeout (e.g., 10s)",
			},
		},
		Required: []string{"host", "user"},
	}
}

func init() {
	adapters.RegisterSourceType("mysql_cdc", &CDCFactory{})
}
