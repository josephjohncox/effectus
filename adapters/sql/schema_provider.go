package sql

import (
	"context"
	sqldb "database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/effectus/effectus-go/adapters"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
)

// SchemaConfig controls SQL schema introspection.
type SchemaConfig struct {
	Driver        string            `json:"driver" yaml:"driver"`
	DSN           string            `json:"dsn" yaml:"dsn"`
	Table         string            `json:"table" yaml:"table"`
	Schema        string            `json:"schema" yaml:"schema"`
	Catalog       string            `json:"catalog" yaml:"catalog"`
	SchemaName    string            `json:"schema_name" yaml:"schema_name"`
	SchemaVersion string            `json:"schema_version" yaml:"schema_version"`
	IncludeCols   []string          `json:"include_columns" yaml:"include_columns"`
	ExcludeCols   []string          `json:"exclude_columns" yaml:"exclude_columns"`
	ColumnMapping map[string]string `json:"column_mapping" yaml:"column_mapping"`
}

// SchemaProvider introspects SQL tables into JSON schema definitions.
type SchemaProvider struct {
	config *SchemaConfig
	db     *sqldb.DB
}

// SchemaFactory creates SQL schema providers.
type SchemaFactory struct{}

func (f *SchemaFactory) ValidateConfig(config adapters.SchemaSourceConfig) error {
	cfg, err := decodeSchemaConfig(config)
	if err != nil {
		return err
	}
	if cfg.Driver == "" {
		return fmt.Errorf("driver is required")
	}
	if cfg.DSN == "" {
		return fmt.Errorf("dsn is required")
	}
	if cfg.Table == "" {
		return fmt.Errorf("table is required")
	}
	return nil
}

func (f *SchemaFactory) Create(config adapters.SchemaSourceConfig) (adapters.SchemaProvider, error) {
	cfg, err := decodeSchemaConfig(config)
	if err != nil {
		return nil, err
	}
	if cfg.SchemaName == "" {
		cfg.SchemaName = cfg.Table
	}
	if cfg.SchemaVersion == "" {
		cfg.SchemaVersion = "v1"
	}

	db, err := sqldb.Open(cfg.Driver, cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("opening sql connection: %w", err)
	}

	return &SchemaProvider{config: cfg, db: db}, nil
}

func (f *SchemaFactory) GetConfigSchema() adapters.ConfigSchema {
	return adapters.ConfigSchema{
		Properties: map[string]adapters.ConfigProperty{
			"driver": {
				Type:        "string",
				Description: "database/sql driver name (postgres, pgx, mysql)",
			},
			"dsn": {
				Type:        "string",
				Description: "connection string for the database",
			},
			"table": {
				Type:        "string",
				Description: "table name to introspect",
			},
			"schema": {
				Type:        "string",
				Description: "database schema (defaults to public for Postgres, current DB for MySQL)",
			},
			"schema_name": {
				Type:        "string",
				Description: "fact schema name/prefix for Effectus",
			},
			"schema_version": {
				Type:        "string",
				Description: "schema version label (default: v1)",
			},
			"include_columns": {
				Type:        "array",
				Description: "only include these columns",
			},
			"exclude_columns": {
				Type:        "array",
				Description: "exclude these columns",
			},
		},
		Required: []string{"driver", "dsn", "table"},
	}
}

func (p *SchemaProvider) LoadSchemas(ctx context.Context) ([]adapters.SchemaDefinition, error) {
	if p == nil || p.config == nil {
		return nil, fmt.Errorf("sql schema provider not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if p.db == nil {
		return nil, fmt.Errorf("sql connection not initialized")
	}
	if err := p.db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("sql ping failed: %w", err)
	}

	columns, err := p.fetchColumns(ctx)
	if err != nil {
		return nil, err
	}

	schema := buildJSONSchema(p.config, columns)
	data, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("encoding schema: %w", err)
	}

	def := adapters.SchemaDefinition{
		Name:    p.config.SchemaName,
		Version: p.config.SchemaVersion,
		Format:  adapters.SchemaFormatJSONSchema,
		Data:    data,
		Source:  p.config.Driver,
	}

	return []adapters.SchemaDefinition{def}, nil
}

func (p *SchemaProvider) Close() error {
	if p == nil || p.db == nil {
		return nil
	}
	return p.db.Close()
}

type columnInfo struct {
	Name     string
	DataType string
	Nullable bool
}

func (p *SchemaProvider) fetchColumns(ctx context.Context) ([]columnInfo, error) {
	driver := strings.ToLower(strings.TrimSpace(p.config.Driver))
	switch {
	case driver == "postgres" || driver == "pgx" || strings.Contains(driver, "postgres"):
		schemaName := p.config.Schema
		if schemaName == "" {
			schemaName = "public"
		}
		rows, err := p.db.QueryContext(ctx,
			"SELECT column_name, data_type, is_nullable FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 ORDER BY ordinal_position",
			schemaName, p.config.Table,
		)
		if err != nil {
			return nil, fmt.Errorf("querying columns: %w", err)
		}
		defer rows.Close()
		return scanColumnRows(rows)
	case strings.Contains(driver, "mysql"):
		schemaName := p.config.Schema
		if schemaName == "" {
			row := p.db.QueryRowContext(ctx, "SELECT DATABASE()")
			if err := row.Scan(&schemaName); err != nil {
				return nil, fmt.Errorf("fetching mysql schema: %w", err)
			}
		}
		rows, err := p.db.QueryContext(ctx,
			"SELECT column_name, data_type, is_nullable FROM information_schema.columns WHERE table_schema = ? AND table_name = ? ORDER BY ordinal_position",
			schemaName, p.config.Table,
		)
		if err != nil {
			return nil, fmt.Errorf("querying columns: %w", err)
		}
		defer rows.Close()
		return scanColumnRows(rows)
	case driver == "sqlite" || driver == "sqlite3":
		query := fmt.Sprintf("PRAGMA table_info(%s)", p.config.Table)
		rows, err := p.db.QueryContext(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("querying sqlite table info: %w", err)
		}
		defer rows.Close()
		return scanSQLiteColumns(rows)
	default:
		return nil, fmt.Errorf("unsupported sql driver for schema introspection: %s", p.config.Driver)
	}
}

func scanColumnRows(rows *sqldb.Rows) ([]columnInfo, error) {
	cols := make([]columnInfo, 0)
	for rows.Next() {
		var name, dataType, nullable string
		if err := rows.Scan(&name, &dataType, &nullable); err != nil {
			return nil, fmt.Errorf("scanning column row: %w", err)
		}
		cols = append(cols, columnInfo{
			Name:     name,
			DataType: dataType,
			Nullable: strings.EqualFold(nullable, "yes") || strings.EqualFold(nullable, "true"),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return cols, nil
}

func scanSQLiteColumns(rows *sqldb.Rows) ([]columnInfo, error) {
	cols := make([]columnInfo, 0)
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull int
		var dflt interface{}
		var pk int
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &dflt, &pk); err != nil {
			return nil, fmt.Errorf("scanning sqlite column row: %w", err)
		}
		cols = append(cols, columnInfo{
			Name:     name,
			DataType: dataType,
			Nullable: notNull == 0,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return cols, nil
}

func buildJSONSchema(config *SchemaConfig, columns []columnInfo) map[string]interface{} {
	props := make(map[string]interface{})
	required := make([]string, 0)
	include := makeStringSet(config.IncludeCols)
	exclude := makeStringSet(config.ExcludeCols)

	for _, column := range columns {
		name := column.Name
		lower := strings.ToLower(name)
		if len(include) > 0 {
			if _, ok := include[lower]; !ok {
				continue
			}
		}
		if _, ok := exclude[lower]; ok {
			continue
		}
		if mapped, ok := config.ColumnMapping[name]; ok {
			name = mapped
		}
		props[name] = buildPropertySchema(column.DataType, column.Nullable)
		if !column.Nullable {
			required = append(required, name)
		}
	}

	schema := map[string]interface{}{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func buildPropertySchema(dataType string, nullable bool) map[string]interface{} {
	lower := strings.ToLower(strings.TrimSpace(dataType))
	schema := make(map[string]interface{})
	if strings.HasSuffix(lower, "[]") {
		elemType := sqlTypeToJSONType(strings.TrimSuffix(lower, "[]"))
		schema["type"] = "array"
		schema["items"] = map[string]interface{}{"type": elemType}
	} else {
		schema["type"] = sqlTypeToJSONType(lower)
	}
	if nullable {
		switch t := schema["type"].(type) {
		case string:
			schema["type"] = []string{t, "null"}
		case []string:
			schema["type"] = append(t, "null")
		}
	}
	return schema
}

func sqlTypeToJSONType(dataType string) string {
	lower := strings.ToLower(strings.TrimSpace(dataType))
	switch {
	case strings.Contains(lower, "bool"):
		return "boolean"
	case strings.Contains(lower, "int"):
		return "integer"
	case strings.Contains(lower, "numeric") || strings.Contains(lower, "decimal") || strings.Contains(lower, "real") || strings.Contains(lower, "double") || strings.Contains(lower, "float"):
		return "number"
	case strings.Contains(lower, "json"):
		return "object"
	case strings.Contains(lower, "time") || strings.Contains(lower, "date"):
		return "string"
	case strings.Contains(lower, "uuid"):
		return "string"
	default:
		return "string"
	}
}

func makeStringSet(items []string) map[string]struct{} {
	set := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		set[item] = struct{}{}
	}
	return set
}

func decodeSchemaConfig(config adapters.SchemaSourceConfig) (*SchemaConfig, error) {
	raw, err := json.Marshal(config.Config)
	if err != nil {
		return nil, fmt.Errorf("encoding schema config: %w", err)
	}
	var cfg SchemaConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("decoding schema config: %w", err)
	}
	return &cfg, nil
}

func init() {
	_ = adapters.RegisterSchemaProvider("sql_introspect", &SchemaFactory{})
}
