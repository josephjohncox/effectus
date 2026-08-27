package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrRulesetActive = errors.New("ruleset is active")
var ErrUnsupportedDeploymentStrategy = errors.New("unsupported deployment strategy")

type RulesetStore interface {
	StoreRuleset(context.Context, *StoredRuleset) error
	GetRuleset(context.Context, string, string) (*StoredRuleset, error)
	ListRulesets(context.Context, *RulesetFilters) ([]*RulesetMetadata, error)
	DeleteRuleset(context.Context, string, string) error
	GetRulesetVersions(context.Context, string) ([]*RulesetVersion, error)
}

type ActivationStore interface {
	GetActiveVersion(context.Context, string, string) (*RulesetVersion, error)
	SetActiveVersion(context.Context, string, string, string) error
	DeployRuleset(context.Context, string, string, string, *DeploymentConfig) error
	GetDeploymentStatus(context.Context, string, string) (*DeploymentStatus, error)
	RollbackDeployment(context.Context, string, string, string) error
}

type AuditStore interface {
	GetAuditLog(context.Context, *AuditFilters) ([]*AuditEntry, error)
	RecordActivity(context.Context, *AuditEntry) error
}

type StorageMaintenance interface {
	HealthCheck(context.Context) error
	Cleanup(context.Context, time.Time) error
}

// RuleStorageBackend composes the storage roles for compatibility.
type RuleStorageBackend interface {
	RulesetStore
	ActivationStore
	AuditStore
	StorageMaintenance
}

type StoredRuleset struct {
	Ruleset *CompiledRuleset

	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Environment string    `json:"environment"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	CreatedBy   string    `json:"created_by"`
	UpdatedBy   string    `json:"updated_by"`

	GitCommit   string `json:"git_commit,omitempty"`
	GitBranch   string `json:"git_branch,omitempty"`
	GitTag      string `json:"git_tag,omitempty"`
	GitAuthor   string `json:"git_author,omitempty"`
	PullRequest string `json:"pull_request,omitempty"`

	CompiledAt      time.Time `json:"compiled_at"`
	CompilerVersion string    `json:"compiler_version"`
	SchemaVersion   string    `json:"schema_version"`
	ValidationHash  string    `json:"validation_hash"`

	Status      RulesetStatus          `json:"status"`
	Deployments map[string]*Deployment `json:"deployments"`

	Tags        []string          `json:"tags"`
	Description string            `json:"description"`
	Owner       string            `json:"owner"`
	Team        string            `json:"team"`
	Metadata    map[string]string `json:"metadata"`
}

type RulesetStatus string

const (
	RulesetStatusDraft      RulesetStatus = "draft"
	RulesetStatusValidating RulesetStatus = "validating"
	RulesetStatusReady      RulesetStatus = "ready"
	RulesetStatusDeployed   RulesetStatus = "deployed"
	RulesetStatusDeprecated RulesetStatus = "deprecated"
	RulesetStatusFailed     RulesetStatus = "failed"
)

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

type DeploymentConfig struct {
	Strategy        string            `json:"strategy"`
	HealthCheckURL  string            `json:"health_check_url"`
	RollbackOnError bool              `json:"rollback_on_error"`
	MaxRollbackDays int               `json:"max_rollback_days"`
	Environments    []string          `json:"environments"`
	RequiredTests   []string          `json:"required_tests"`
	Approvers       []string          `json:"approvers"`
	Metadata        map[string]string `json:"metadata"`
}

func normalizeDeploymentConfig(config *DeploymentConfig) (*DeploymentConfig, error) {
	if config == nil {
		return &DeploymentConfig{Strategy: "atomic"}, nil
	}
	resolved := *config
	resolved.Strategy = strings.ToLower(strings.TrimSpace(resolved.Strategy))
	if resolved.Strategy == "" {
		resolved.Strategy = "atomic"
	}
	if resolved.Strategy != "atomic" {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedDeploymentStrategy, resolved.Strategy)
	}
	return &resolved, nil
}

type CanaryConfig struct {
	TrafficPercent   int           `json:"traffic_percent"`
	Duration         time.Duration `json:"duration"`
	SuccessThreshold float64       `json:"success_threshold"`
	ErrorThreshold   float64       `json:"error_threshold"`
	MetricsQueries   []string      `json:"metrics_queries"`
}

type RollbackInfo struct {
	PreviousVersion string    `json:"previous_version"`
	RollbackReason  string    `json:"rollback_reason"`
	RolledBackAt    time.Time `json:"rolled_back_at"`
	RolledBackBy    string    `json:"rolled_back_by"`
	AutoRollback    bool      `json:"auto_rollback"`
}

type HealthCheckResult struct {
	Status      string            `json:"status"`
	LastChecked time.Time         `json:"last_checked"`
	Details     map[string]string `json:"details"`
	Errors      []string          `json:"errors"`
}

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

type RulesetVersion struct {
	Version       string    `json:"version"`
	CreatedAt     time.Time `json:"created_at"`
	CreatedBy     string    `json:"created_by"`
	GitCommit     string    `json:"git_commit"`
	IsActive      bool      `json:"is_active"`
	DeployedEnvs  []string  `json:"deployed_envs"`
	ChangeMessage string    `json:"change_message"`
}

type AuditEntry struct {
	ID          string                 `json:"id"`
	Timestamp   time.Time              `json:"timestamp"`
	Action      string                 `json:"action"`
	Resource    string                 `json:"resource"`
	ResourceID  *uuid.UUID             `json:"resource_id,omitempty"`
	Version     string                 `json:"version"`
	Environment string                 `json:"environment"`
	UserID      string                 `json:"user_id"`
	UserEmail   string                 `json:"user_email"`
	IPAddress   string                 `json:"ip_address"`
	UserAgent   string                 `json:"user_agent"`
	SessionID   string                 `json:"session_id"`
	Details     map[string]interface{} `json:"details"`
	RequestID   string                 `json:"request_id"`
	TraceID     string                 `json:"trace_id"`
	Result      string                 `json:"result"`
	ErrorMsg    string                 `json:"error_msg,omitempty"`
	DurationMs  int                    `json:"duration_ms,omitempty"`
}

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
