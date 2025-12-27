package runtime

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/effectus/effectus-go/runtime/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"
)

// PostgresStorage implements RuleStorageBackend using sqlc and goose
type PostgresStorage struct {
	pool    *pgxpool.Pool
	queries *db.Queries
	config  *PostgresStorageConfig
}

// PostgresStorageConfig configures PostgreSQL storage with modern tooling
type PostgresStorageConfig struct {
	// Database connection
	DSN             string        `yaml:"dsn"`
	MaxConnections  int           `yaml:"max_connections"`
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime"`
	ConnMaxIdleTime time.Duration `yaml:"conn_max_idle_time"`

	// Migration settings
	MigrationsPath string `yaml:"migrations_path"`
	AutoMigrate    bool   `yaml:"auto_migrate"`

	// Performance settings
	PreparedStatements bool          `yaml:"prepared_statements"`
	CacheEnabled       bool          `yaml:"cache_enabled"`
	CacheTTL           time.Duration `yaml:"cache_ttl"`

	// Maintenance settings
	AuditRetentionDays int    `yaml:"audit_retention_days"`
	VacuumEnabled      bool   `yaml:"vacuum_enabled"`
	VacuumSchedule     string `yaml:"vacuum_schedule"`

	// Monitoring
	MetricsEnabled bool `yaml:"metrics_enabled"`
	LogQueries     bool `yaml:"log_queries"`
}

// NewPostgresStorage creates a new PostgreSQL storage backend with modern tooling
func NewPostgresStorage(config *PostgresStorageConfig) (*PostgresStorage, error) {
	// Set defaults
	if config.MaxConnections == 0 {
		config.MaxConnections = 25
	}
	if config.ConnMaxLifetime == 0 {
		config.ConnMaxLifetime = time.Hour
	}
	if config.ConnMaxIdleTime == 0 {
		config.ConnMaxIdleTime = 30 * time.Minute
	}
	if config.MigrationsPath == "" {
		config.MigrationsPath = "migrations"
	}

	// Configure connection pool
	poolConfig, err := pgxpool.ParseConfig(config.DSN)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DSN: %w", err)
	}

	poolConfig.MaxConns = int32(config.MaxConnections)
	poolConfig.MaxConnLifetime = config.ConnMaxLifetime
	poolConfig.MaxConnIdleTime = config.ConnMaxIdleTime

	// Create connection pool
	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Test connection
	if err := pool.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	storage := &PostgresStorage{
		pool:    pool,
		queries: db.New(pool),
		config:  config,
	}

	// Run migrations if enabled
	if config.AutoMigrate {
		if err := storage.runMigrations(); err != nil {
			return nil, fmt.Errorf("failed to run migrations: %w", err)
		}
	}

	return storage, nil
}

// runMigrations runs database migrations using goose
func (p *PostgresStorage) runMigrations() error {
	// Get a regular sql.DB connection for goose
	db, err := sql.Open("postgres", p.config.DSN)
	if err != nil {
		return fmt.Errorf("failed to open database for migrations: %w", err)
	}
	defer db.Close()

	// Set goose dialect
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	// Run migrations
	if err := goose.Up(db, p.config.MigrationsPath); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

// StoreRuleset stores a compiled ruleset using sqlc-generated queries
func (p *PostgresStorage) StoreRuleset(ctx context.Context, ruleset *StoredRuleset) error {
	// Convert to database format
	rulesetData, err := json.Marshal(ruleset.Ruleset)
	if err != nil {
		return fmt.Errorf("failed to marshal ruleset: %w", err)
	}

	metadata, err := json.Marshal(ruleset.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Convert types
	gitCommit := toPgText(ruleset.GitCommit)
	gitBranch := toPgText(ruleset.GitBranch)
	gitTag := toPgText(ruleset.GitTag)
	gitAuthor := toPgText(ruleset.GitAuthor)
	pullRequest := toPgText(ruleset.PullRequest)

	description := toPgText(ruleset.Description)
	owner := toPgText(ruleset.Owner)
	team := toPgText(ruleset.Team)

	createdBy := toPgText(ruleset.CreatedBy)
	updatedBy := toPgText(ruleset.UpdatedBy)

	compilerVersion := toPgText(ruleset.CompilerVersion)
	schemaVersion := toPgText(ruleset.SchemaVersion)
	validationHash := toPgText(ruleset.ValidationHash)

	compiledAt := toPgTimestamptz(ruleset.CompiledAt)

	// Use upsert to handle conflicts
	dbRuleset, err := p.queries.UpsertRuleset(
		ctx,
		ruleset.Name,
		ruleset.Version,
		ruleset.Environment,
		db.RulesetStatus(ruleset.Status),
		rulesetData,
		int32(len(ruleset.Ruleset.Rules)),
		description,
		ruleset.Tags,
		owner,
		team,
		metadata,
		gitCommit,
		gitBranch,
		gitTag,
		gitAuthor,
		pullRequest,
		compiledAt,
		compilerVersion,
		schemaVersion,
		validationHash,
		createdBy,
		updatedBy,
	)

	if err != nil {
		return fmt.Errorf("failed to store ruleset: %w", err)
	}

	// Update the ID in the input struct
	ruleset.ID = dbRuleset.ID.String()

	// Record audit entry
	auditEntry := &AuditEntry{
		Action:      "store_ruleset",
		Resource:    ruleset.Name,
		ResourceID:  &dbRuleset.ID,
		Version:     ruleset.Version,
		Environment: ruleset.Environment,
		UserID:      ruleset.CreatedBy,
		Details: map[string]interface{}{
			"rule_count":      len(ruleset.Ruleset.Rules),
			"schema_version":  ruleset.SchemaVersion,
			"validation_hash": ruleset.ValidationHash,
		},
		Result: "success",
	}

	if err := p.RecordActivity(ctx, auditEntry); err != nil {
		// Log error but don't fail the operation
		fmt.Printf("Failed to record audit entry: %v\n", err)
	}

	return nil
}

// GetRuleset retrieves a ruleset using sqlc-generated queries
func (p *PostgresStorage) GetRuleset(ctx context.Context, name, version string) (*StoredRuleset, error) {
	// First try to get with explicit environment, then fall back to any environment
	environments := []string{"production", "staging", "development"}

	for _, env := range environments {
		dbRuleset, err := p.queries.GetRuleset(ctx, name, version, env)

		if err != nil {
			if err == pgx.ErrNoRows {
				continue // Try next environment
			}
			return nil, fmt.Errorf("failed to get ruleset: %w", err)
		}

		return p.convertDBRuleset(dbRuleset)
	}

	return nil, fmt.Errorf("ruleset not found: %s:%s", name, version)
}

// ListRulesets lists rulesets with filtering using sqlc-generated queries
func (p *PostgresStorage) ListRulesets(ctx context.Context, filters *RulesetFilters) ([]*RulesetMetadata, error) {
	if filters == nil {
		filters = &RulesetFilters{}
	}

	// Convert filters to database parameters
	var environments []string
	if len(filters.Environments) > 0 {
		environments = filters.Environments
	}

	var status []db.RulesetStatus
	if len(filters.Status) > 0 {
		status = make([]db.RulesetStatus, len(filters.Status))
		for i, s := range filters.Status {
			status[i] = db.RulesetStatus(s)
		}
	}

	limit := int32(filters.Limit)
	if limit == 0 {
		limit = 50
	}

	nameFilter := toTextArrayLiteral(filters.Names)
	createdAfter := pgtype.Timestamptz{}
	if filters.CreatedAfter != nil {
		createdAfter = pgtype.Timestamptz{Time: *filters.CreatedAfter, Valid: true}
	}

	// Execute query
	dbRulesets, err := p.queries.ListRulesets(
		ctx,
		nameFilter,
		environments,
		status,
		filters.Owner,
		filters.Team,
		filters.Tags,
		createdAfter,
		filters.CreatedBy,
		filters.GitCommit,
		limit,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to list rulesets: %w", err)
	}

	// Convert to metadata
	results := make([]*RulesetMetadata, len(dbRulesets))
	for i, dbRuleset := range dbRulesets {
		results[i] = &RulesetMetadata{
			ID:             dbRuleset.ID.String(),
			Name:           dbRuleset.Name,
			Version:        dbRuleset.Version,
			Environment:    dbRuleset.Environment,
			Status:         RulesetStatus(dbRuleset.Status),
			RuleCount:      int(dbRuleset.RuleCount),
			CreatedAt:      dbRuleset.CreatedAt.Time,
			UpdatedAt:      dbRuleset.UpdatedAt.Time,
			CreatedBy:      getStringValue(dbRuleset.CreatedBy),
			Tags:           dbRuleset.Tags,
			Description:    getStringValue(dbRuleset.Description),
			Owner:          getStringValue(dbRuleset.Owner),
			Team:           getStringValue(dbRuleset.Team),
			SchemaVersion:  getStringValue(dbRuleset.SchemaVersion),
			ValidationHash: getStringValue(dbRuleset.ValidationHash),
		}
	}

	return results, nil
}

// RecordActivity records an audit entry using sqlc-generated queries
func (p *PostgresStorage) RecordActivity(ctx context.Context, entry *AuditEntry) error {
	// Convert details to JSON
	details, err := json.Marshal(entry.Details)
	if err != nil {
		return fmt.Errorf("failed to marshal audit details: %w", err)
	}

	// Convert types
	resourceID := uuid.Nil
	if entry.ResourceID != nil {
		resourceID = *entry.ResourceID
	}

	version := toPgText(entry.Version)
	environment := toPgText(entry.Environment)
	userID := toPgText(entry.UserID)
	userEmail := toPgText(entry.UserEmail)
	userAgent := toPgText(entry.UserAgent)
	sessionID := toPgText(entry.SessionID)
	requestID := toPgText(entry.RequestID)
	traceID := toPgText(entry.TraceID)
	errorMessage := toPgText(entry.ErrorMsg)

	var ipAddress net.IP
	if entry.IPAddress != "" {
		ipAddress = net.ParseIP(entry.IPAddress)
	}

	durationMs := toPgInt4(entry.DurationMs)

	_, err = p.queries.CreateAuditEntry(
		ctx,
		entry.Action,
		entry.Resource,
		resourceID,
		version,
		environment,
		userID,
		userEmail,
		ipAddress,
		userAgent,
		sessionID,
		details,
		requestID,
		traceID,
		entry.Result,
		errorMessage,
		durationMs,
	)

	if err != nil {
		return fmt.Errorf("failed to record audit entry: %w", err)
	}

	return nil
}

// GetAuditLog retrieves audit logs using sqlc-generated queries
func (p *PostgresStorage) GetAuditLog(ctx context.Context, filters *AuditFilters) ([]*AuditEntry, error) {
	if filters == nil {
		filters = &AuditFilters{}
	}

	// Convert filters to database parameters
	var actions, resources, userIDs []string
	if len(filters.Actions) > 0 {
		actions = filters.Actions
	}
	if len(filters.Resources) > 0 {
		resources = filters.Resources
	}
	if len(filters.UserIDs) > 0 {
		userIDs = filters.UserIDs
	}

	startTime := pgtype.Timestamptz{}
	if !filters.StartTime.IsZero() {
		startTime = pgtype.Timestamptz{Time: filters.StartTime, Valid: true}
	}

	endTime := pgtype.Timestamptz{}
	if !filters.EndTime.IsZero() {
		endTime = pgtype.Timestamptz{Time: filters.EndTime, Valid: true}
	}

	limit := int32(filters.Limit)
	if limit == 0 {
		limit = 100
	}

	offset := int32(filters.Offset)

	// Execute query
	dbEntries, err := p.queries.GetAuditLogs(
		ctx,
		actions,
		resources,
		userIDs,
		startTime,
		endTime,
		filters.Result,
		"",
		limit,
		offset,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get audit logs: %w", err)
	}

	// Convert to audit entries
	entries := make([]*AuditEntry, len(dbEntries))
	for i, dbEntry := range dbEntries {
		var details map[string]interface{}
		if len(dbEntry.Details) > 0 {
			json.Unmarshal(dbEntry.Details, &details)
		}

		var resourceID *uuid.UUID
		if dbEntry.ResourceID != uuid.Nil {
			resourceID = &dbEntry.ResourceID
		}

		entries[i] = &AuditEntry{
			ID:          dbEntry.ID.String(),
			Timestamp:   dbEntry.Timestamp.Time,
			Action:      dbEntry.Action,
			Resource:    dbEntry.Resource,
			ResourceID:  resourceID,
			Version:     getStringValue(dbEntry.Version),
			Environment: getStringValue(dbEntry.Environment),
			UserID:      getStringValue(dbEntry.UserID),
			UserEmail:   getStringValue(dbEntry.UserEmail),
			IPAddress:   dbEntry.IpAddress.String(),
			UserAgent:   getStringValue(dbEntry.UserAgent),
			SessionID:   getStringValue(dbEntry.SessionID),
			Details:     details,
			RequestID:   getStringValue(dbEntry.RequestID),
			TraceID:     getStringValue(dbEntry.TraceID),
			Result:      dbEntry.Result,
			ErrorMsg:    getStringValue(dbEntry.ErrorMessage),
			DurationMs:  getInt32Value(dbEntry.DurationMs),
		}
	}

	return entries, nil
}

// HealthCheck checks the database connection
func (p *PostgresStorage) HealthCheck(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

// Cleanup removes old data based on retention policies
func (p *PostgresStorage) Cleanup(ctx context.Context, olderThan time.Time) error {
	// Clean up old audit logs
	if err := p.queries.CleanupOldAuditLogs(ctx, pgtype.Timestamptz{Time: olderThan, Valid: true}); err != nil {
		return fmt.Errorf("failed to cleanup audit logs: %w", err)
	}

	return nil
}

// Close closes the database connection pool
func (p *PostgresStorage) Close() {
	p.pool.Close()
}

// Helper functions for converting database results
func (p *PostgresStorage) convertDBRuleset(dbRuleset *db.Ruleset) (*StoredRuleset, error) {
	// Unmarshal ruleset data
	var compiledRuleset CompiledRuleset
	if err := json.Unmarshal(dbRuleset.RulesetData, &compiledRuleset); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ruleset data: %w", err)
	}

	// Unmarshal metadata
	var metadata map[string]string
	if len(dbRuleset.Metadata) > 0 {
		if err := json.Unmarshal(dbRuleset.Metadata, &metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	}

	return &StoredRuleset{
		ID:              dbRuleset.ID.String(),
		Name:            dbRuleset.Name,
		Version:         dbRuleset.Version,
		Environment:     dbRuleset.Environment,
		CreatedAt:       dbRuleset.CreatedAt.Time,
		UpdatedAt:       dbRuleset.UpdatedAt.Time,
		CreatedBy:       getStringValue(dbRuleset.CreatedBy),
		UpdatedBy:       getStringValue(dbRuleset.UpdatedBy),
		GitCommit:       getStringValue(dbRuleset.GitCommit),
		GitBranch:       getStringValue(dbRuleset.GitBranch),
		GitTag:          getStringValue(dbRuleset.GitTag),
		GitAuthor:       getStringValue(dbRuleset.GitAuthor),
		PullRequest:     getStringValue(dbRuleset.PullRequest),
		CompiledAt:      getTimeValue(dbRuleset.CompiledAt),
		CompilerVersion: getStringValue(dbRuleset.CompilerVersion),
		SchemaVersion:   getStringValue(dbRuleset.SchemaVersion),
		ValidationHash:  getStringValue(dbRuleset.ValidationHash),
		Status:          RulesetStatus(dbRuleset.Status),
		Ruleset:         &compiledRuleset,
		Tags:            dbRuleset.Tags,
		Description:     getStringValue(dbRuleset.Description),
		Owner:           getStringValue(dbRuleset.Owner),
		Team:            getStringValue(dbRuleset.Team),
		Metadata:        metadata,
		Deployments:     make(map[string]*Deployment),
	}, nil
}

// Helper functions for nullable types
func getStringValue(s pgtype.Text) string {
	if !s.Valid {
		return ""
	}
	return s.String
}

func getTimeValue(t pgtype.Timestamptz) time.Time {
	if !t.Valid {
		return time.Time{}
	}
	return t.Time
}

func getInt32Value(i pgtype.Int4) int {
	if !i.Valid {
		return 0
	}
	return int(i.Int32)
}

func toPgText(value string) pgtype.Text {
	if value == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: value, Valid: true}
}

func toPgTimestamptz(value time.Time) pgtype.Timestamptz {
	if value.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: value, Valid: true}
}

func toPgInt4(value int) pgtype.Int4 {
	if value == 0 {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: int32(value), Valid: true}
}

func toTextArrayLiteral(values []string) string {
	if len(values) == 0 {
		return ""
	}

	escaped := make([]string, len(values))
	for i, value := range values {
		escaped[i] = strings.ReplaceAll(value, "\"", "\\\"")
	}
	return "{" + strings.Join(escaped, ",") + "}"
}

// Placeholder implementations for remaining interface methods
func (p *PostgresStorage) DeleteRuleset(ctx context.Context, name, version string) error {
	return fmt.Errorf("not implemented")
}

func (p *PostgresStorage) GetRulesetVersions(ctx context.Context, name string) ([]*RulesetVersion, error) {
	return nil, fmt.Errorf("not implemented")
}

func (p *PostgresStorage) GetActiveVersion(ctx context.Context, name, environment string) (*RulesetVersion, error) {
	return nil, fmt.Errorf("not implemented")
}

func (p *PostgresStorage) SetActiveVersion(ctx context.Context, name, environment, version string) error {
	return fmt.Errorf("not implemented")
}

func (p *PostgresStorage) DeployRuleset(ctx context.Context, name, version, environment string, config *DeploymentConfig) error {
	return fmt.Errorf("not implemented")
}

func (p *PostgresStorage) GetDeploymentStatus(ctx context.Context, name, environment string) (*DeploymentStatus, error) {
	return nil, fmt.Errorf("not implemented")
}

func (p *PostgresStorage) RollbackDeployment(ctx context.Context, name, environment, targetVersion string) error {
	return fmt.Errorf("not implemented")
}
