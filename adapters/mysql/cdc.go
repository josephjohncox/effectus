package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
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
	SourceID       string            `json:"source_id" yaml:"source_id"`
	Host           string            `json:"host" yaml:"host"`
	Port           int               `json:"port" yaml:"port"`
	User           string            `json:"user" yaml:"user"`
	Password       string            `json:"password" yaml:"password"`
	Flavor         string            `json:"flavor" yaml:"flavor"` // mysql or mariadb
	ServerID       uint32            `json:"server_id" yaml:"server_id"`
	Database       string            `json:"database" yaml:"database"`
	Tables         []string          `json:"tables" yaml:"tables"`
	Operations     []string          `json:"operations" yaml:"operations"`
	SchemaMapping  map[string]string `json:"schema_mapping" yaml:"schema_mapping"`
	StartFile      string            `json:"start_file" yaml:"start_file"`
	StartPos       uint32            `json:"start_pos" yaml:"start_pos"`
	GTID           string            `json:"gtid" yaml:"gtid"`
	CheckpointPath string            `json:"checkpoint_path" yaml:"checkpoint_path"`
	DSN            string            `json:"dsn" yaml:"dsn"`
	BufferSize     int               `json:"buffer_size" yaml:"buffer_size"`
	Timeout        time.Duration     `json:"timeout" yaml:"timeout"`
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
	stopping bool
	done     chan struct{}

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
	if strings.TrimSpace(config.CheckpointPath) == "" {
		return nil, fmt.Errorf("checkpoint_path is required for durable MySQL CDC recovery")
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

	return &CDCSource{
		config:       config,
		metrics:      adapters.GetGlobalMetrics(),
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

// Start opens connections and begins reading binlog events.
func (c *CDCSource) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
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
	if err := c.initializeCheckpoint(ctx); err != nil {
		db.Close()
		c.db = nil
		return err
	}

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
		c.syncer.Close()
		db.Close()
		c.db = nil
		return err
	}
	c.streamer = streamer
	c.ctx, c.cancel = context.WithCancel(ctx)
	c.factChan = make(chan *adapters.TypedFact, c.config.BufferSize)
	c.done = make(chan struct{})
	c.running = true
	c.stopping = false
	workerCtx, factChan, done := c.ctx, c.factChan, c.done
	go c.consumeEvents(workerCtx, streamer, factChan, done)

	log.Printf("MySQL CDC source started for %s:%d", c.config.Host, c.config.Port)
	return nil
}

// Stop stops the CDC stream.
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
	cancel, syncer, db := c.cancel, c.syncer, c.db
	c.mu.Unlock()
	cancel()
	if syncer != nil {
		syncer.Close()
	}
	if db != nil {
		_ = db.Close()
	}
	if err := waitForCDCStop(ctx, done); err != nil {
		go func() { <-done; c.finishStop(done) }()
		return err
	}
	c.finishStop(done)
	log.Printf("MySQL CDC source stopped")
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
	c.syncer = nil
	c.streamer = nil
	c.db = nil
	c.ctx = nil
	c.cancel = nil
}

// GetSourceSchema returns schema metadata.
func (c *CDCSource) GetSourceSchema() *adapters.Schema {
	return c.schema
}

// HealthCheck checks connectivity.
func (c *CDCSource) HealthCheck() error {
	c.mu.Lock()
	db, workerCtx := c.db, c.ctx
	c.mu.Unlock()
	if db == nil || workerCtx == nil {
		return fmt.Errorf("schema db not initialized")
	}
	ctx, cancel := context.WithTimeout(workerCtx, 5*time.Second)
	defer cancel()
	return db.PingContext(ctx)
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
		return nil, fmt.Errorf("durable MySQL CDC checkpoint has no binlog coordinate")
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
	_ = gtidSet
	return mysql.Position{Name: file, Pos: position}, nil
}

type cdcCheckpoint struct {
	Binlog string `json:"binlog"`
	Pos    uint32 `json:"pos"`
	GTID   string `json:"gtid,omitempty"`
}

func (c *CDCSource) initializeCheckpoint(ctx context.Context) error {
	payload, err := os.ReadFile(c.config.CheckpointPath)
	if err == nil {
		var checkpoint cdcCheckpoint
		if err := json.Unmarshal(payload, &checkpoint); err != nil {
			return fmt.Errorf("decode MySQL CDC checkpoint: %w", err)
		}
		if checkpoint.Binlog == "" && checkpoint.GTID == "" {
			return fmt.Errorf("MySQL CDC checkpoint has no recovery coordinate")
		}
		c.config.StartFile, c.config.StartPos, c.config.GTID = checkpoint.Binlog, checkpoint.Pos, checkpoint.GTID
		c.currentBinlog = checkpoint.Binlog
		return nil
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("read MySQL CDC checkpoint: %w", err)
	}

	checkpoint := cdcCheckpoint{Binlog: c.config.StartFile, Pos: c.config.StartPos, GTID: c.config.GTID}
	if checkpoint.Binlog == "" {
		position, statusErr := c.masterStatus(ctx)
		if statusErr != nil {
			return fmt.Errorf("initialize MySQL CDC checkpoint: %w", statusErr)
		}
		checkpoint.Binlog, checkpoint.Pos = position.Name, position.Pos
	}
	if err := c.persistCheckpoint(ctx, checkpoint); err != nil {
		return err
	}
	c.config.StartFile, c.config.StartPos = checkpoint.Binlog, checkpoint.Pos
	c.currentBinlog = checkpoint.Binlog
	return nil
}

func (c *CDCSource) persistCheckpoint(ctx context.Context, checkpoint cdcCheckpoint) error {
	if checkpoint.Binlog == "" || checkpoint.Pos == 0 {
		return fmt.Errorf("MySQL CDC checkpoint requires a binlog file and position")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	directory := filepath.Dir(c.config.CheckpointPath)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create MySQL CDC checkpoint directory: %w", err)
	}
	payload, err := json.Marshal(checkpoint)
	if err != nil {
		return fmt.Errorf("encode MySQL CDC checkpoint: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".effectus-mysql-checkpoint-*")
	if err != nil {
		return fmt.Errorf("create MySQL CDC checkpoint: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("secure MySQL CDC checkpoint: %w", err)
	}
	written, err := temporary.Write(payload)
	if err == nil && written != len(payload) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = temporary.Sync()
	}
	closeErr := temporary.Close()
	if err != nil {
		return fmt.Errorf("write MySQL CDC checkpoint: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("close MySQL CDC checkpoint: %w", closeErr)
	}
	if err := os.Rename(temporaryName, c.config.CheckpointPath); err != nil {
		return fmt.Errorf("replace MySQL CDC checkpoint: %w", err)
	}
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open MySQL CDC checkpoint directory: %w", err)
	}
	syncErr := directoryHandle.Sync()
	closeDirectoryErr := directoryHandle.Close()
	if syncErr != nil {
		return fmt.Errorf("sync MySQL CDC checkpoint directory: %w", syncErr)
	}
	if closeDirectoryErr != nil {
		return fmt.Errorf("close MySQL CDC checkpoint directory: %w", closeDirectoryErr)
	}
	return nil
}

func (c *CDCSource) consumeEvents(ctx context.Context, streamer *replication.BinlogStreamer, factChan chan *adapters.TypedFact, done chan struct{}) {
	defer close(done)
	defer close(factChan)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		ev, err := streamer.GetEvent(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("MySQL CDC stream error: %v", err)
			time.Sleep(1 * time.Second)
			continue
		}

		switch event := ev.Event.(type) {
		case *replication.RotateEvent:
			c.mu.Lock()
			c.currentBinlog = string(event.NextLogName)
			c.mu.Unlock()
			continue
		}

		rowsEvent, ok := ev.Event.(*replication.RowsEvent)
		if !ok {
			continue
		}

		if err := c.handleRowsEvent(ctx, factChan, rowsEvent, ev.Header); err != nil {
			if ctx.Err() == nil {
				log.Printf("MySQL CDC handle error: %v", err)
			}
			return
		}
	}
}

func (c *CDCSource) handleRowsEvent(ctx context.Context, factChan chan<- *adapters.TypedFact, event *replication.RowsEvent, header *replication.EventHeader) error {
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
	type rowChange struct {
		before map[string]interface{}
		after  map[string]interface{}
	}
	var changes []rowChange
	switch header.EventType {
	case replication.WRITE_ROWS_EVENTv1, replication.WRITE_ROWS_EVENTv2:
		for _, row := range event.Rows {
			changes = append(changes, rowChange{after: rowToMap(columns, row)})
		}
	case replication.DELETE_ROWS_EVENTv1, replication.DELETE_ROWS_EVENTv2:
		for _, row := range event.Rows {
			changes = append(changes, rowChange{before: rowToMap(columns, row)})
		}
	case replication.UPDATE_ROWS_EVENTv1, replication.UPDATE_ROWS_EVENTv2:
		for i := 0; i+1 < len(event.Rows); i += 2 {
			changes = append(changes, rowChange{before: rowToMap(columns, event.Rows[i]), after: rowToMap(columns, event.Rows[i+1])})
		}
	}
	if len(changes) == 0 {
		return nil
	}
	c.mu.Lock()
	binlog := c.currentBinlog
	c.mu.Unlock()
	barrier := adapters.NewAcknowledgementBarrier(len(changes), func(ackCtx context.Context) error {
		return c.persistCheckpoint(ackCtx, cdcCheckpoint{Binlog: binlog, Pos: header.LogPos})
	})
	for index, change := range changes {
		if err := c.emitChange(ctx, factChan, operation, schemaName, tableName, change.before, change.after, header, binlog, barrier.Callback(index)); err != nil {
			return err
		}
	}
	return barrier.Wait(ctx)
}

func (c *CDCSource) emitChange(ctx context.Context, factChan chan<- *adapters.TypedFact, operation, schemaName, tableName string, before, after map[string]interface{}, header *replication.EventHeader, binlog string, acknowledge func(context.Context) error) error {
	change := &ChangeEvent{
		Operation: operation,
		Schema:    schemaName,
		Table:     tableName,
		Before:    before,
		After:     after,
		Binlog:    binlog,
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
		return err
	}

	rawData, err := json.Marshal(change)
	if err != nil {
		c.metrics.RecordError(c.config.SourceID, "marshal", err)
		return err
	}

	fact := &adapters.TypedFact{
		SchemaName:    mappedSchema,
		SchemaVersion: "v1.0.0",
		Data:          structData,
		RawData:       rawData,
		Timestamp:     change.Timestamp,
		SourceID:      c.config.SourceID,
		Acknowledge:   acknowledge,
		Metadata: map[string]string{
			"mysql.operation": operation,
			"mysql.schema":    schemaName,
			"mysql.table":     tableName,
			"mysql.pos":       fmt.Sprintf("%d", change.Pos),
			"source_type":     "mysql_cdc",
		},
	}

	select {
	case factChan <- fact:
		c.metrics.RecordFactProcessed(c.config.SourceID, mappedSchema)
		return nil
	case <-ctx.Done():
		return ctx.Err()
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
	if v, ok := config.Config["checkpoint_path"].(string); ok {
		cdcConfig.CheckpointPath = v
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
	if checkpoint, ok := config.Config["checkpoint_path"].(string); !ok || strings.TrimSpace(checkpoint) == "" {
		return fmt.Errorf("checkpoint_path is required for mysql_cdc source")
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
			"checkpoint_path": {
				Type:        "string",
				Description: "Durable local checkpoint file for acknowledged binlog coordinates",
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
