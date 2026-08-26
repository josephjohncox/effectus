package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// InMemoryRuleStorage implements RuleStorageBackend in memory
// Useful for testing and local development
type InMemoryRuleStorage struct {
	rulesets    map[string]*StoredRuleset
	deployments map[string]*Deployment
	auditLog    []*AuditEntry
	mu          sync.RWMutex
}

// NewInMemoryRuleStorage creates a new in-memory rule storage backend
func NewInMemoryRuleStorage() *InMemoryRuleStorage {
	return &InMemoryRuleStorage{
		rulesets:    make(map[string]*StoredRuleset),
		deployments: make(map[string]*Deployment),
		auditLog:    make([]*AuditEntry, 0),
	}
}

// StoreRuleset stores a ruleset in memory
func (m *InMemoryRuleStorage) StoreRuleset(ctx context.Context, ruleset *StoredRuleset) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := fmt.Sprintf("%s:%s:%s", ruleset.Name, ruleset.Version, ruleset.Environment)

	// Generate ID if not provided
	if ruleset.ID == "" {
		ruleset.ID = fmt.Sprintf("mem-%d", time.Now().UnixNano())
	}

	m.rulesets[key] = ruleset

	// Record audit
	auditEntry := &AuditEntry{
		ID:          fmt.Sprintf("audit-%d", time.Now().UnixNano()),
		Timestamp:   time.Now(),
		Action:      "store_ruleset",
		Resource:    ruleset.Name,
		Version:     ruleset.Version,
		Environment: ruleset.Environment,
		UserID:      ruleset.CreatedBy,
		Result:      "success",
	}
	m.auditLog = append(m.auditLog, auditEntry)

	return nil
}

// GetRuleset retrieves a ruleset from memory
func (m *InMemoryRuleStorage) GetRuleset(ctx context.Context, name, version string) (*StoredRuleset, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Search across environments
	for _, ruleset := range m.rulesets {
		if ruleset.Name == name && ruleset.Version == version {
			return ruleset, nil
		}
	}

	return nil, fmt.Errorf("ruleset not found: %s:%s", name, version)
}

// ListRulesets lists rulesets from memory with filters
func (m *InMemoryRuleStorage) ListRulesets(ctx context.Context, filters *RulesetFilters) ([]*RulesetMetadata, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []*RulesetMetadata

	for _, ruleset := range m.rulesets {
		if m.matchesFilters(ruleset, filters) {
			metadata := &RulesetMetadata{
				ID:              ruleset.ID,
				Name:            ruleset.Name,
				Version:         ruleset.Version,
				Environment:     ruleset.Environment,
				Status:          ruleset.Status,
				RuleCount:       len(ruleset.Ruleset.Rules),
				CreatedAt:       ruleset.CreatedAt,
				UpdatedAt:       ruleset.UpdatedAt,
				CreatedBy:       ruleset.CreatedBy,
				Tags:            ruleset.Tags,
				Description:     ruleset.Description,
				Owner:           ruleset.Owner,
				Team:            ruleset.Team,
				SchemaVersion:   ruleset.SchemaVersion,
				ValidationHash:  ruleset.ValidationHash,
				DeploymentCount: len(ruleset.Deployments),
			}
			results = append(results, metadata)
		}
	}

	return results, nil
}

// DeleteRuleset removes a ruleset from memory
func (m *InMemoryRuleStorage) DeleteRuleset(ctx context.Context, name, version string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for key, deployment := range m.deployments {
		if strings.HasPrefix(key, name+":") && deployment.Version == version && deployment.Status == DeploymentStatusActive {
			return fmt.Errorf("%w: %s:%s", ErrRulesetActive, name, version)
		}
	}

	// Find and delete the ruleset
	for key, ruleset := range m.rulesets {
		if ruleset.Name == name && ruleset.Version == version {
			delete(m.rulesets, key)

			// Record audit
			auditEntry := &AuditEntry{
				ID:          fmt.Sprintf("audit-%d", time.Now().UnixNano()),
				Timestamp:   time.Now(),
				Action:      "delete_ruleset",
				Resource:    name,
				Version:     version,
				Environment: ruleset.Environment,
				Result:      "success",
			}
			m.auditLog = append(m.auditLog, auditEntry)

			return nil
		}
	}

	return fmt.Errorf("ruleset not found: %s:%s", name, version)
}

// GetRulesetVersions returns versions of a ruleset
func (m *InMemoryRuleStorage) GetRulesetVersions(ctx context.Context, name string) ([]*RulesetVersion, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var versions []*RulesetVersion
	seen := make(map[string]bool)

	for _, ruleset := range m.rulesets {
		if ruleset.Name == name && !seen[ruleset.Version] {
			versions = append(versions, &RulesetVersion{
				Version:      ruleset.Version,
				CreatedAt:    ruleset.CreatedAt,
				CreatedBy:    ruleset.CreatedBy,
				IsActive:     ruleset.Status == RulesetStatusDeployed,
				DeployedEnvs: []string{ruleset.Environment},
			})
			seen[ruleset.Version] = true
		}
	}

	return versions, nil
}

// GetActiveVersion returns the active version for an environment
func (m *InMemoryRuleStorage) GetActiveVersion(ctx context.Context, name, environment string) (*RulesetVersion, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	deployment := m.deployments[name+":"+environment]
	if deployment == nil || deployment.Status != DeploymentStatusActive {
		return nil, fmt.Errorf("no active version found for %s in %s", name, environment)
	}
	for _, ruleset := range m.rulesets {
		if ruleset.Name == name && ruleset.Version == deployment.Version {
			return &RulesetVersion{
				Version:      ruleset.Version,
				CreatedAt:    ruleset.CreatedAt,
				CreatedBy:    ruleset.CreatedBy,
				IsActive:     true,
				DeployedEnvs: []string{environment},
			}, nil
		}
	}
	return nil, fmt.Errorf("active ruleset artifact not found for %s in %s", name, environment)
}

// SetActiveVersion atomically replaces the active version for an environment.
// Deprecated: use DeployRuleset with the atomic strategy.
func (m *InMemoryRuleStorage) SetActiveVersion(ctx context.Context, name, environment, version string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activateLocked(name, version, environment, &DeploymentConfig{Strategy: "atomic"}, "activate_ruleset")
}

// DeployRuleset atomically replaces the active version for an environment.
func (m *InMemoryRuleStorage) DeployRuleset(ctx context.Context, name, version, environment string, config *DeploymentConfig) error {
	config, err := normalizeDeploymentConfig(config)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activateLocked(name, version, environment, config, "deploy_ruleset")
}

func (m *InMemoryRuleStorage) activateLocked(name, version, environment string, config *DeploymentConfig, action string) error {
	var target *StoredRuleset
	for _, ruleset := range m.rulesets {
		if ruleset.Name == name && ruleset.Version == version {
			target = ruleset
			break
		}
	}
	if target == nil {
		return fmt.Errorf("ruleset not found for deployment: %s:%s", name, version)
	}

	key := name + ":" + environment
	if current := m.deployments[key]; current != nil {
		current.Status = DeploymentStatusInactive
	}
	for _, ruleset := range m.rulesets {
		if ruleset.Name != name || ruleset.Deployments == nil {
			continue
		}
		if deployment := ruleset.Deployments[environment]; deployment != nil {
			deployment.Status = DeploymentStatusInactive
			if ruleset.Status == RulesetStatusDeployed {
				ruleset.Status = RulesetStatusReady
			}
		}
	}

	now := time.Now()
	deployment := &Deployment{
		Environment: environment,
		Version:     version,
		DeployedAt:  now,
		Status:      DeploymentStatusActive,
		Config:      config,
	}
	if target.Deployments == nil {
		target.Deployments = make(map[string]*Deployment)
	}
	target.Deployments[environment] = deployment
	target.Status = RulesetStatusDeployed
	m.deployments[key] = deployment
	m.auditLog = append(m.auditLog, &AuditEntry{
		ID:          fmt.Sprintf("audit-%d", now.UnixNano()),
		Timestamp:   now,
		Action:      action,
		Resource:    name,
		Version:     version,
		Environment: environment,
		Result:      "success",
	})
	return nil
}

// GetDeploymentStatus returns deployment status
func (m *InMemoryRuleStorage) GetDeploymentStatus(ctx context.Context, name, environment string) (*DeploymentStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	deployment := m.deployments[name+":"+environment]
	if deployment == nil {
		return nil, fmt.Errorf("no deployment found for %s in %s", name, environment)
	}
	status := deployment.Status
	return &status, nil
}

// RollbackDeployment rolls back a deployment
func (m *InMemoryRuleStorage) RollbackDeployment(ctx context.Context, name, environment, targetVersion string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.activateLocked(name, targetVersion, environment, &DeploymentConfig{Strategy: "atomic"}, "rollback_deployment")
}

// GetAuditLog returns audit logs with filters
func (m *InMemoryRuleStorage) GetAuditLog(ctx context.Context, filters *AuditFilters) ([]*AuditEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []*AuditEntry

	for _, entry := range m.auditLog {
		if m.matchesAuditFilters(entry, filters) {
			results = append(results, entry)
		}
	}

	// Apply limit
	if filters != nil && filters.Limit > 0 && len(results) > filters.Limit {
		results = results[:filters.Limit]
	}

	return results, nil
}

// RecordActivity records an audit entry
func (m *InMemoryRuleStorage) RecordActivity(ctx context.Context, entry *AuditEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if entry.ID == "" {
		entry.ID = fmt.Sprintf("audit-%d", time.Now().UnixNano())
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}

	m.auditLog = append(m.auditLog, entry)
	return nil
}

// HealthCheck always returns healthy for in-memory storage
func (m *InMemoryRuleStorage) HealthCheck(ctx context.Context) error {
	return nil
}

// Cleanup removes old entries
func (m *InMemoryRuleStorage) Cleanup(ctx context.Context, olderThan time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Remove old audit entries
	var filteredAudit []*AuditEntry
	for _, entry := range m.auditLog {
		if entry.Timestamp.After(olderThan) {
			filteredAudit = append(filteredAudit, entry)
		}
	}
	m.auditLog = filteredAudit

	return nil
}

// Helper methods

func (m *InMemoryRuleStorage) matchesFilters(ruleset *StoredRuleset, filters *RulesetFilters) bool {
	if filters == nil {
		return true
	}

	// Name filter
	if len(filters.Names) > 0 {
		found := false
		for _, name := range filters.Names {
			if ruleset.Name == name {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Environment filter
	if len(filters.Environments) > 0 {
		found := false
		for _, env := range filters.Environments {
			if ruleset.Environment == env {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Status filter
	if len(filters.Status) > 0 {
		found := false
		for _, status := range filters.Status {
			if ruleset.Status == status {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Owner filter
	if filters.Owner != "" && ruleset.Owner != filters.Owner {
		return false
	}

	// Team filter
	if filters.Team != "" && ruleset.Team != filters.Team {
		return false
	}

	// Tags filter
	if len(filters.Tags) > 0 {
		hasTag := false
		for _, filterTag := range filters.Tags {
			for _, rulesetTag := range ruleset.Tags {
				if rulesetTag == filterTag {
					hasTag = true
					break
				}
			}
			if hasTag {
				break
			}
		}
		if !hasTag {
			return false
		}
	}

	return true
}

func (m *InMemoryRuleStorage) matchesAuditFilters(entry *AuditEntry, filters *AuditFilters) bool {
	if filters == nil {
		return true
	}

	// Actions filter
	if len(filters.Actions) > 0 {
		found := false
		for _, action := range filters.Actions {
			if entry.Action == action {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Resources filter
	if len(filters.Resources) > 0 {
		found := false
		for _, resource := range filters.Resources {
			if entry.Resource == resource {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// User IDs filter
	if len(filters.UserIDs) > 0 {
		found := false
		for _, userID := range filters.UserIDs {
			if entry.UserID == userID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Time range filter
	if !filters.StartTime.IsZero() && entry.Timestamp.Before(filters.StartTime) {
		return false
	}
	if !filters.EndTime.IsZero() && entry.Timestamp.After(filters.EndTime) {
		return false
	}

	// Result filter
	if filters.Result != "" && entry.Result != filters.Result {
		return false
	}

	return true
}
