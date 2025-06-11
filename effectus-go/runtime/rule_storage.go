package runtime

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/lib/pq"
)

// RuleStorageBackend defines interface for persistent rule storage
type RuleStorageBackend interface {
	// Core CRUD operations
	StoreRuleset(ctx context.Context, ruleset *StoredRuleset) error
	GetRuleset(ctx context.Context, name, version string) (*StoredRuleset, error)
	ListRulesets(ctx context.Context, filters *RulesetFilters) ([]*RulesetMetadata, error)
	DeleteRuleset(ctx context.Context, name, version string) error

	// Version management
	GetRulesetVersions(ctx context.Context, name string) ([]*RulesetVersion, error)
	GetActiveVersion(ctx context.Context, name, environment string) (*RulesetVersion, error)
	SetActiveVersion(ctx context.Context, name, environment, version string) error

	// Deployment management
	DeployRuleset(ctx context.Context, name, version, environment string, config *DeploymentConfig) error
	GetDeploymentStatus(ctx context.Context, name, environment string) (*DeploymentStatus, error)
	RollbackDeployment(ctx context.Context, name, environment, targetVersion string) error

	// Audit and compliance
	GetAuditLog(ctx context.Context, filters *AuditFilters) ([]*AuditEntry, error)
	RecordActivity(ctx context.Context, entry *AuditEntry) error

	// Health and maintenance
	HealthCheck(ctx context.Context) error
	Cleanup(ctx context.Context, olderThan time.Time) error
}

// StoredRuleset represents a ruleset with metadata for storage
type StoredRuleset struct {
	// Core ruleset data
	Ruleset *CompiledRuleset

	// Storage metadata
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Environment string    `json:"environment"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	CreatedBy   string    `json:"created_by"`
	UpdatedBy   string    `json:"updated_by"`

	// Git integration
	GitCommit   string `json:"git_commit,omitempty"`
	GitBranch   string `json:"git_branch,omitempty"`
	GitTag      string `json:"git_tag,omitempty"`
	GitAuthor   string `json:"git_author,omitempty"`
	PullRequest string `json:"pull_request,omitempty"`

	// Validation and compilation
	CompiledAt      time.Time `json:"compiled_at"`
	CompilerVersion string    `json:"compiler_version"`
	SchemaVersion   string    `json:"schema_version"`
	ValidationHash  string    `json:"validation_hash"`

	// Deployment status
	Status      RulesetStatus          `json:"status"`
	Deployments map[string]*Deployment `json:"deployments"`

	// Business metadata
	Tags        []string          `json:"tags"`
	Description string            `json:"description"`
	Owner       string            `json:"owner"`
	Team        string            `json:"team"`
	Metadata    map[string]string `json:"metadata"`
}

// RulesetStatus represents the lifecycle status of a ruleset
type RulesetStatus string

const (
	RulesetStatusDraft      RulesetStatus = "draft"
	RulesetStatusValidating RulesetStatus = "validating"
	RulesetStatusReady      RulesetStatus = "ready"
	RulesetStatusDeployed   RulesetStatus = "deployed"
	RulesetStatusDeprecated RulesetStatus = "deprecated"
	RulesetStatusFailed     RulesetStatus = "failed"
)

// Deployment represents a deployment to a specific environment
type Deployment struct {
	Environment  string             `json:"environment"`
	Version      string             `json:"version"`
	DeployedAt   time.Time          `json:"deployed_at"`
	DeployedBy   string             `json:"deployed_by"`
	Status       DeploymentStatus   `json:"status"`
	Config       *DeploymentConfig  `json:"config"`
	HealthCheck  *HealthCheckResult `json:"health_check"`
	RollbackInfo *RollbackInfo      `json:"rollback_info,omitempty"`
	CanaryConfig *CanaryConfig      `json:"canary_config,omitempty"`
}

// DeploymentStatus represents deployment state
type DeploymentStatus string

const (
	DeploymentStatusPending     DeploymentStatus = "pending"
	DeploymentStatusDeploying   DeploymentStatus = "deploying"
	DeploymentStatusActive      DeploymentStatus = "active"
	DeploymentStatusCanary      DeploymentStatus = "canary"
	DeploymentStatusRollingBack DeploymentStatus = "rolling_back"
	DeploymentStatusFailed      DeploymentStatus = "failed"
	DeploymentStatusInactive    DeploymentStatus = "inactive"
)

// DeploymentConfig controls deployment behavior
type DeploymentConfig struct {
	Strategy        string            `json:"strategy"` // "blue_green", "rolling", "canary"
	HealthCheckURL  string            `json:"health_check_url"`
	RollbackOnError bool              `json:"rollback_on_error"`
	MaxRollbackDays int               `json:"max_rollback_days"`
	Environments    []string          `json:"environments"`
	RequiredTests   []string          `json:"required_tests"`
	Approvers       []string          `json:"approvers"`
	Metadata        map[string]string `json:"metadata"`
}

// CanaryConfig for canary deployments
type CanaryConfig struct {
	TrafficPercent   int           `json:"traffic_percent"`
	Duration         time.Duration `json:"duration"`
	SuccessThreshold float64       `json:"success_threshold"`
	ErrorThreshold   float64       `json:"error_threshold"`
	MetricsQueries   []string      `json:"metrics_queries"`
}

// RollbackInfo tracks rollback information
type RollbackInfo struct {
	PreviousVersion string    `json:"previous_version"`
	RollbackReason  string    `json:"rollback_reason"`
	RolledBackAt    time.Time `json:"rolled_back_at"`
	RolledBackBy    string    `json:"rolled_back_by"`
	AutoRollback    bool      `json:"auto_rollback"`
}

// HealthCheckResult represents health check status
type HealthCheckResult struct {
	Status      string            `json:"status"`
	LastChecked time.Time         `json:"last_checked"`
	Details     map[string]string `json:"details"`
	Errors      []string          `json:"errors"`
}

// RulesetFilters for querying rulesets
type RulesetFilters struct {
	Names        []string          `json:"names"`
	Versions     []string          `json:"versions"`
	Environments []string          `json:"environments"`
	Status       []RulesetStatus   `json:"status"`
	Tags         []string          `json:"tags"`
	Owner        string            `json:"owner"`
	Team         string            `json:"team"`
	CreatedAfter *time.Time        `json:"created_after"`
	CreatedBy    string            `json:"created_by"`
	GitCommit    string            `json:"git_commit"`
	Metadata     map[string]string `json:"metadata"`
	Limit        int               `json:"limit"`
	Offset       int               `json:"offset"`
}

// RulesetMetadata provides summary information
type RulesetMetadata struct {
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	Version         string        `json:"version"`
	Environment     string        `json:"environment"`
	Status          RulesetStatus `json:"status"`
	RuleCount       int           `json:"rule_count"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
	CreatedBy       string        `json:"created_by"`
	Tags            []string      `json:"tags"`
	Description     string        `json:"description"`
	Owner           string        `json:"owner"`
	Team            string        `json:"team"`
	SchemaVersion   string        `json:"schema_version"`
	ValidationHash  string        `json:"validation_hash"`
	DeploymentCount int           `json:"deployment_count"`
}

// RulesetVersion represents a version of a ruleset
type RulesetVersion struct {
	Version       string    `json:"version"`
	CreatedAt     time.Time `json:"created_at"`
	CreatedBy     string    `json:"created_by"`
	GitCommit     string    `json:"git_commit"`
	IsActive      bool      `json:"is_active"`
	DeployedEnvs  []string  `json:"deployed_envs"`
	ChangeMessage string    `json:"change_message"`
}

// AuditEntry for tracking all rule operations
type AuditEntry struct {
	ID          string                 `json:"id"`
	Timestamp   time.Time              `json:"timestamp"`
	Action      string                 `json:"action"`
	Resource    string                 `json:"resource"` // ruleset name
	Version     string                 `json:"version"`
	Environment string                 `json:"environment"`
	UserID      string                 `json:"user_id"`
	UserEmail   string                 `json:"user_email"`
	IPAddress   string                 `json:"ip_address"`
	UserAgent   string                 `json:"user_agent"`
	Details     map[string]interface{} `json:"details"`
	Result      string                 `json:"result"` // success, failure, error
	ErrorMsg    string                 `json:"error_msg,omitempty"`
}

// AuditFilters for querying audit logs
type AuditFilters struct {
	Actions   []string  `json:"actions"`
	Resources []string  `json:"resources"`
	UserIDs   []string  `json:"user_ids"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Result    string    `json:"result"`
	Limit     int       `json:"limit"`
	Offset    int       `json:"offset"`
}

// PostgresRuleStorage implements RuleStorageBackend with PostgreSQL
type PostgresRuleStorage struct {
	db     *sql.DB
	cache  *redis.Client
	config *PostgresStorageConfig
}

// PostgresStorageConfig configures PostgreSQL storage
type PostgresStorageConfig struct {
	DSN                string        `json:"dsn"`
	MaxConnections     int           `json:"max_connections"`
	CacheEnabled       bool          `json:"cache_enabled"`
	CacheTTL           time.Duration `json:"cache_ttl"`
	AuditRetentionDays int           `json:"audit_retention_days"`
	TablePrefix        string        `json:"table_prefix"`
}

// NewPostgresRuleStorage creates a new PostgreSQL rule storage backend
func NewPostgresRuleStorage(config *PostgresStorageConfig) (*PostgresRuleStorage, error) {
	db, err := sql.Open("postgres", config.DSN)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	db.SetMaxOpenConns(config.MaxConnections)
	db.SetMaxIdleConns(config.MaxConnections / 2)

	storage := &PostgresRuleStorage{
		db:     db,
		config: config,
	}

	// Setup Redis cache if enabled
	if config.CacheEnabled {
		storage.cache = redis.NewClient(&redis.Options{
			Addr: "localhost:6379", // Should be configurable
		})
	}

	// Initialize database schema
	if err := storage.initializeSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return storage, nil
}

// initializeSchema creates necessary database tables
func (p *PostgresRuleStorage) initializeSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS effectus_rulesets (
		id VARCHAR(255) PRIMARY KEY,
		name VARCHAR(255) NOT NULL,
		version VARCHAR(100) NOT NULL,
		environment VARCHAR(100) NOT NULL,
		status VARCHAR(50) NOT NULL,
		ruleset_data JSONB NOT NULL,
		created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
		created_by VARCHAR(255),
		updated_by VARCHAR(255),
		git_commit VARCHAR(255),
		git_branch VARCHAR(255),
		git_tag VARCHAR(255),
		compiled_at TIMESTAMP WITH TIME ZONE,
		compiler_version VARCHAR(100),
		schema_version VARCHAR(100),
		validation_hash VARCHAR(255),
		tags TEXT[],
		description TEXT,
		owner VARCHAR(255),
		team VARCHAR(255),
		metadata JSONB,
		UNIQUE(name, version, environment)
	);

	CREATE INDEX IF NOT EXISTS idx_rulesets_name ON effectus_rulesets(name);
	CREATE INDEX IF NOT EXISTS idx_rulesets_version ON effectus_rulesets(version);
	CREATE INDEX IF NOT EXISTS idx_rulesets_environment ON effectus_rulesets(environment);
	CREATE INDEX IF NOT EXISTS idx_rulesets_status ON effectus_rulesets(status);
	CREATE INDEX IF NOT EXISTS idx_rulesets_created_at ON effectus_rulesets(created_at);
	CREATE INDEX IF NOT EXISTS idx_rulesets_owner ON effectus_rulesets(owner);
	CREATE INDEX IF NOT EXISTS idx_rulesets_team ON effectus_rulesets(team);
	CREATE INDEX IF NOT EXISTS idx_rulesets_tags ON effectus_rulesets USING GIN(tags);

	CREATE TABLE IF NOT EXISTS effectus_deployments (
		id SERIAL PRIMARY KEY,
		ruleset_name VARCHAR(255) NOT NULL,
		ruleset_version VARCHAR(100) NOT NULL,
		environment VARCHAR(100) NOT NULL,
		status VARCHAR(50) NOT NULL,
		deployed_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
		deployed_by VARCHAR(255),
		config JSONB,
		health_check JSONB,
		rollback_info JSONB,
		canary_config JSONB,
		created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(ruleset_name, environment)
	);

	CREATE INDEX IF NOT EXISTS idx_deployments_ruleset ON effectus_deployments(ruleset_name);
	CREATE INDEX IF NOT EXISTS idx_deployments_environment ON effectus_deployments(environment);
	CREATE INDEX IF NOT EXISTS idx_deployments_status ON effectus_deployments(status);

	CREATE TABLE IF NOT EXISTS effectus_audit_log (
		id SERIAL PRIMARY KEY,
		timestamp TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
		action VARCHAR(100) NOT NULL,
		resource VARCHAR(255) NOT NULL,
		version VARCHAR(100),
		environment VARCHAR(100),
		user_id VARCHAR(255),
		user_email VARCHAR(255),
		ip_address INET,
		user_agent TEXT,
		details JSONB,
		result VARCHAR(50),
		error_msg TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON effectus_audit_log(timestamp);
	CREATE INDEX IF NOT EXISTS idx_audit_action ON effectus_audit_log(action);
	CREATE INDEX IF NOT EXISTS idx_audit_resource ON effectus_audit_log(resource);
	CREATE INDEX IF NOT EXISTS idx_audit_user_id ON effectus_audit_log(user_id);
	CREATE INDEX IF NOT EXISTS idx_audit_result ON effectus_audit_log(result);

	-- Create trigger for updating updated_at
	CREATE OR REPLACE FUNCTION update_updated_at_column()
	RETURNS TRIGGER AS $$
	BEGIN
		NEW.updated_at = CURRENT_TIMESTAMP;
		RETURN NEW;
	END;
	$$ language 'plpgsql';

	CREATE OR REPLACE TRIGGER update_rulesets_updated_at
		BEFORE UPDATE ON effectus_rulesets
		FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

	CREATE OR REPLACE TRIGGER update_deployments_updated_at
		BEFORE UPDATE ON effectus_deployments
		FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
	`

	_, err := p.db.Exec(schema)
	return err
}

// StoreRuleset stores a compiled ruleset
func (p *PostgresRuleStorage) StoreRuleset(ctx context.Context, ruleset *StoredRuleset) error {
	// Serialize ruleset data
	rulesetData, err := json.Marshal(ruleset.Ruleset)
	if err != nil {
		return fmt.Errorf("failed to marshal ruleset: %w", err)
	}

	// Generate ID if not provided
	if ruleset.ID == "" {
		ruleset.ID = p.generateRulesetID(ruleset.Name, ruleset.Version, ruleset.Environment)
	}

	// Generate validation hash
	ruleset.ValidationHash = p.generateValidationHash(ruleset)

	// Store in database
	query := `
		INSERT INTO effectus_rulesets (
			id, name, version, environment, status, ruleset_data,
			created_by, updated_by, git_commit, git_branch, git_tag,
			compiled_at, compiler_version, schema_version, validation_hash,
			tags, description, owner, team, metadata
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
			$16, $17, $18, $19, $20
		) ON CONFLICT (name, version, environment) DO UPDATE SET
			status = EXCLUDED.status,
			ruleset_data = EXCLUDED.ruleset_data,
			updated_by = EXCLUDED.updated_by,
			git_commit = EXCLUDED.git_commit,
			validation_hash = EXCLUDED.validation_hash,
			metadata = EXCLUDED.metadata
	`

	metadataJSON, _ := json.Marshal(ruleset.Metadata)

	_, err = p.db.ExecContext(ctx, query,
		ruleset.ID, ruleset.Name, ruleset.Version, ruleset.Environment,
		ruleset.Status, rulesetData, ruleset.CreatedBy, ruleset.UpdatedBy,
		ruleset.GitCommit, ruleset.GitBranch, ruleset.GitTag,
		ruleset.CompiledAt, ruleset.CompilerVersion, ruleset.SchemaVersion,
		ruleset.ValidationHash, pq.Array(ruleset.Tags), ruleset.Description,
		ruleset.Owner, ruleset.Team, metadataJSON,
	)

	if err != nil {
		return fmt.Errorf("failed to store ruleset: %w", err)
	}

	// Update cache
	if p.cache != nil {
		cacheKey := fmt.Sprintf("ruleset:%s:%s:%s", ruleset.Name, ruleset.Version, ruleset.Environment)
		cacheData, _ := json.Marshal(ruleset)
		p.cache.Set(ctx, cacheKey, cacheData, p.config.CacheTTL)
	}

	// Record audit entry
	auditEntry := &AuditEntry{
		Action:      "store_ruleset",
		Resource:    ruleset.Name,
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
	p.RecordActivity(ctx, auditEntry)

	return nil
}

// GetRuleset retrieves a ruleset by name and version
func (p *PostgresRuleStorage) GetRuleset(ctx context.Context, name, version string) (*StoredRuleset, error) {
	// Try cache first
	if p.cache != nil {
		cacheKey := fmt.Sprintf("ruleset:%s:%s", name, version)
		cached, err := p.cache.Get(ctx, cacheKey).Result()
		if err == nil {
			var ruleset StoredRuleset
			if json.Unmarshal([]byte(cached), &ruleset) == nil {
				return &ruleset, nil
			}
		}
	}

	// Query database
	query := `
		SELECT id, name, version, environment, status, ruleset_data,
			   created_at, updated_at, created_by, updated_by,
			   git_commit, git_branch, git_tag, compiled_at,
			   compiler_version, schema_version, validation_hash,
			   tags, description, owner, team, metadata
		FROM effectus_rulesets 
		WHERE name = $1 AND version = $2
	`

	var ruleset StoredRuleset
	var rulesetData []byte
	var metadataJSON []byte
	var tags pq.StringArray

	err := p.db.QueryRowContext(ctx, query, name, version).Scan(
		&ruleset.ID, &ruleset.Name, &ruleset.Version, &ruleset.Environment,
		&ruleset.Status, &rulesetData, &ruleset.CreatedAt, &ruleset.UpdatedAt,
		&ruleset.CreatedBy, &ruleset.UpdatedBy, &ruleset.GitCommit,
		&ruleset.GitBranch, &ruleset.GitTag, &ruleset.CompiledAt,
		&ruleset.CompilerVersion, &ruleset.SchemaVersion, &ruleset.ValidationHash,
		&tags, &ruleset.Description, &ruleset.Owner, &ruleset.Team, &metadataJSON,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get ruleset: %w", err)
	}

	// Unmarshal ruleset data
	err = json.Unmarshal(rulesetData, &ruleset.Ruleset)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal ruleset data: %w", err)
	}

	// Unmarshal metadata
	if len(metadataJSON) > 0 {
		json.Unmarshal(metadataJSON, &ruleset.Metadata)
	}

	ruleset.Tags = []string(tags)

	// Update cache
	if p.cache != nil {
		cacheKey := fmt.Sprintf("ruleset:%s:%s", name, version)
		cacheData, _ := json.Marshal(&ruleset)
		p.cache.Set(ctx, cacheKey, cacheData, p.config.CacheTTL)
	}

	return &ruleset, nil
}

// Helper methods
func (p *PostgresRuleStorage) generateRulesetID(name, version, environment string) string {
	data := fmt.Sprintf("%s:%s:%s", name, version, environment)
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", hash[:8])
}

func (p *PostgresRuleStorage) generateValidationHash(ruleset *StoredRuleset) string {
	// Create hash from ruleset content for validation
	data := fmt.Sprintf("%s:%s:%s:%d",
		ruleset.Name, ruleset.Version, ruleset.SchemaVersion, len(ruleset.Ruleset.Rules))
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", hash[:16])
}

// Additional interface implementations...
func (p *PostgresRuleStorage) ListRulesets(ctx context.Context, filters *RulesetFilters) ([]*RulesetMetadata, error) {
	// Implementation for listing rulesets with filters
	return nil, nil
}

func (p *PostgresRuleStorage) DeleteRuleset(ctx context.Context, name, version string) error {
	// Implementation for deleting rulesets
	return nil
}

func (p *PostgresRuleStorage) GetRulesetVersions(ctx context.Context, name string) ([]*RulesetVersion, error) {
	// Implementation for getting ruleset versions
	return nil, nil
}

func (p *PostgresRuleStorage) GetActiveVersion(ctx context.Context, name, environment string) (*RulesetVersion, error) {
	// Implementation for getting active version
	return nil, nil
}

func (p *PostgresRuleStorage) SetActiveVersion(ctx context.Context, name, environment, version string) error {
	// Implementation for setting active version
	return nil
}

func (p *PostgresRuleStorage) DeployRuleset(ctx context.Context, name, version, environment string, config *DeploymentConfig) error {
	// Implementation for deploying rulesets
	return nil
}

func (p *PostgresRuleStorage) GetDeploymentStatus(ctx context.Context, name, environment string) (*DeploymentStatus, error) {
	// Implementation for getting deployment status
	return nil, nil
}

func (p *PostgresRuleStorage) RollbackDeployment(ctx context.Context, name, environment, targetVersion string) error {
	// Implementation for rollback
	return nil
}

func (p *PostgresRuleStorage) GetAuditLog(ctx context.Context, filters *AuditFilters) ([]*AuditEntry, error) {
	// Implementation for audit log retrieval
	return nil, nil
}

func (p *PostgresRuleStorage) RecordActivity(ctx context.Context, entry *AuditEntry) error {
	// Implementation for recording audit entries
	return nil
}

func (p *PostgresRuleStorage) HealthCheck(ctx context.Context) error {
	// Implementation for health check
	return nil
}

func (p *PostgresRuleStorage) Cleanup(ctx context.Context, olderThan time.Time) error {
	// Implementation for cleanup
	return nil
}
