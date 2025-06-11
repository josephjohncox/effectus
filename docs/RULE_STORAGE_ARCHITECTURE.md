# Rule Storage Architecture

Effectus implements a comprehensive rule storage system that bridges development workflows (git) with production runtime requirements (database/cache), similar to ABAC/RBAC policy storage systems.

## Architecture Overview

```mermaid
graph TB
    subgraph "Development"
        Git[Git Repository<br/>rules/*.eff]
        IDE[VS Code<br/>Rule Authoring]
        CLI[effectusc CLI<br/>Compilation & Testing]
    end
    
    subgraph "CI/CD Pipeline"
        Build[Build & Compile]
        Test[Schema Validation<br/>& Testing]
        Package[Package Ruleset]
    end
    
    subgraph "Storage Layer"
        Postgres[(PostgreSQL<br/>Primary Storage)]
        Redis[(Redis<br/>Cache & Sessions)]
        Etcd[(etcd<br/>Configuration)]
        InMem[In-Memory<br/>Development]
    end
    
    subgraph "Runtime Environments"
        Dev[Development]
        Staging[Staging]
        Prod[Production]
    end
    
    subgraph "Management"
        Manager[Rule Manager<br/>Orchestration]
        Deploy[Deployment Controller]
        Monitor[Health Monitoring]
        Audit[Audit Logging]
    end
    
    Git --> Build
    IDE --> Git
    CLI --> Git
    
    Build --> Test
    Test --> Package
    Package --> Manager
    
    Manager --> Postgres
    Manager --> Redis
    Manager --> Etcd
    Manager --> InMem
    
    Manager --> Deploy
    Deploy --> Dev
    Deploy --> Staging  
    Deploy --> Prod
    
    Deploy --> Monitor
    Deploy --> Audit
    
    Postgres --> Audit
    Redis --> Dev
    Postgres --> Staging
    Postgres --> Prod
```

## Storage Backends

### 1. PostgreSQL (Primary Production Storage)

**Use Case**: Production storage with ACID guarantees, complex queries, audit trails

```go
type PostgresRuleStorage struct {
    db     *sql.DB
    cache  *redis.Client  // Optional caching layer
    config *PostgresStorageConfig
}
```

**Features**:
- Full ACID compliance
- Complex querying and filtering
- Audit trail with retention policies
- Multi-environment support
- Version management and rollback
- Deployment tracking
- Performance metrics storage

**Schema**:
```sql
CREATE TABLE effectus_rulesets (
    id VARCHAR(255) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    version VARCHAR(100) NOT NULL,
    environment VARCHAR(100) NOT NULL,
    status VARCHAR(50) NOT NULL,
    ruleset_data JSONB NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    git_commit VARCHAR(255),
    schema_version VARCHAR(100),
    validation_hash VARCHAR(255),
    tags TEXT[],
    owner VARCHAR(255),
    team VARCHAR(255),
    metadata JSONB,
    UNIQUE(name, version, environment)
);

CREATE TABLE effectus_deployments (
    id SERIAL PRIMARY KEY,
    ruleset_name VARCHAR(255) NOT NULL,
    environment VARCHAR(100) NOT NULL,
    status VARCHAR(50) NOT NULL,
    deployed_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    config JSONB,
    health_check JSONB
);

CREATE TABLE effectus_audit_log (
    id SERIAL PRIMARY KEY,
    timestamp TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    action VARCHAR(100) NOT NULL,
    resource VARCHAR(255) NOT NULL,
    user_id VARCHAR(255),
    details JSONB,
    result VARCHAR(50)
);
```

### 2. Redis (Fast Runtime Cache)

**Use Case**: High-performance runtime cache, session storage, real-time data

```go
type RedisRuleStorage struct {
    client *redis.Client
    config *RedisStorageConfig
}
```

**Features**:
- Sub-millisecond read latency
- Cluster support for high availability
- TTL-based expiration
- Pub/sub for real-time updates
- Atomic operations
- Memory-optimized storage

**Usage Pattern**:
```go
// Cache compiled rulesets for fast access
key := "ruleset:user_onboarding:v1.2.0:prod"
cached, err := redis.Get(ctx, key).Result()

// Index management
indexKey := "index:ruleset:user_onboarding"
redis.SAdd(ctx, indexKey, "v1.2.0")
```

### 3. etcd (Configuration & Coordination)

**Use Case**: Distributed configuration, leader election, service discovery

```go
type EtcdRuleStorage struct {
    client *clientv3.Client
    config *EtcdStorageConfig
}
```

**Features**:
- Strong consistency guarantees
- Distributed coordination
- Watch capabilities for real-time updates
- Lease-based TTL
- Transaction support
- Multi-version concurrency control

### 4. In-Memory (Development & Testing)

**Use Case**: Local development, unit testing, temporary storage

```go
type InMemoryRuleStorage struct {
    rulesets    map[string]*StoredRuleset
    deployments map[string]*Deployment
    auditLog    []*AuditEntry
    mu          sync.RWMutex
}
```

**Features**:
- Zero-latency access
- No external dependencies
- Perfect for testing
- Full feature compatibility
- Memory-efficient

### 5. Multi-Backend (High Availability)

**Use Case**: Production systems requiring high availability and consistency

```go
type MultiBackendRuleStorage struct {
    primary   RuleStorageBackend
    secondary []RuleStorageBackend
    config    *MultiBackendConfig
}
```

**Strategies**:
- **Write Strategies**: primary_only, all, majority
- **Read Strategies**: primary_first, fastest, majority
- **Consistency Levels**: eventual, strong
- **Failover**: Automatic failover to secondary backends

## Development to Production Workflow

### 1. Development Phase

```bash
# Local development with VS Code
code rules/user_onboarding.eff

# Real-time validation and testing
effectusc lint rules/
effectusc test rules/user_onboarding.eff --synthetic

# Hot-reload development server
effectusc dev --watch rules/ --backend inmemory
```

**Storage**: In-Memory backend for instant feedback

### 2. Git Integration

```bash
# Commit rules to git
git add rules/
git commit -m "Add user onboarding rules"
git push origin feature/user-onboarding

# CI/CD pipeline triggered
```

**Automatic Process**:
1. Schema validation against latest protobuf definitions
2. Rule compilation and optimization
3. Performance testing with synthetic data
4. Security scanning
5. Package as deployment artifact

### 3. Staging Deployment

```yaml
# .effectus/deployment.yaml
environments:
  staging:
    type: staging
    storage: postgres
    require_approval: false
    auto_deploy: true
    health_checks: true
    rollback_on_error: true
```

**Process**:
```go
result, err := ruleManager.DeployFromGit(ctx, "staging", &DeploymentOptions{
    Strategy:    "blue_green",
    HealthCheck: true,
    DryRun:      false,
})
```

### 4. Production Deployment

```yaml
environments:
  production:
    type: production
    storage: postgres
    require_approval: true
    approvers: ["team-lead", "engineering-manager"]
    strategy: canary
    rollout_percent: 10
    health_check_url: "/health"
    performance_budget: "100ms"
```

**Approval Workflow**:
1. Deployment request created
2. Approval notifications sent
3. Required approvals collected
4. Automated canary deployment
5. Health monitoring and metrics
6. Gradual rollout or automatic rollback

## Storage Interface

All backends implement the `RuleStorageBackend` interface:

```go
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
```

## Rule Lifecycle States

```mermaid
stateDiagram-v2
    [*] --> Draft: Create Rule
    Draft --> Validating: Compile & Validate
    Validating --> Ready: Validation Success
    Validating --> Failed: Validation Error
    Ready --> Deployed: Deploy to Environment
    Deployed --> Deprecated: Mark Deprecated
    Deprecated --> [*]: Delete
    Failed --> Draft: Fix & Retry
    Deployed --> Deployed: Update/Rollback
```

### State Descriptions

- **Draft**: Rule exists in git but not compiled/validated
- **Validating**: Rule is being compiled and validated
- **Ready**: Rule passed validation, ready for deployment
- **Deployed**: Rule is active in one or more environments
- **Deprecated**: Rule is marked for removal
- **Failed**: Rule failed validation or deployment

## Deployment Strategies

### 1. Blue-Green Deployment

```go
type BlueGreenDeployment struct {
    BlueVersion  string `json:"blue_version"`
    GreenVersion string `json:"green_version"`
    ActiveColor  string `json:"active_color"`
    SwitchTime   time.Time `json:"switch_time"`
}
```

**Process**:
1. Deploy new version to inactive environment (green)
2. Run health checks and validation tests
3. Switch traffic from blue to green
4. Keep blue as rollback option

### 2. Canary Deployment

```go
type CanaryDeployment struct {
    BaselineVersion string  `json:"baseline_version"`
    CanaryVersion   string  `json:"canary_version"`
    TrafficPercent  int     `json:"traffic_percent"`
    SuccessRate     float64 `json:"success_rate"`
    ErrorRate       float64 `json:"error_rate"`
}
```

**Process**:
1. Deploy new version alongside current version
2. Route small percentage of traffic to new version
3. Monitor metrics and error rates
4. Gradually increase traffic or rollback

### 3. Rolling Deployment

```go
type RollingDeployment struct {
    TotalInstances     int    `json:"total_instances"`
    UpdatedInstances   int    `json:"updated_instances"`
    BatchSize          int    `json:"batch_size"`
    CurrentBatch       int    `json:"current_batch"`
    MaxUnavailable     int    `json:"max_unavailable"`
}
```

**Process**:
1. Update instances in small batches
2. Wait for health checks between batches
3. Continue until all instances updated
4. Rollback batch by batch if needed

## Security and Compliance

### 1. Access Control

```go
type AccessPolicy struct {
    Environment string   `json:"environment"`
    Actions     []string `json:"actions"`
    Users       []string `json:"users"`
    Roles       []string `json:"roles"`
    Conditions  []string `json:"conditions"`
}
```

### 2. Audit Logging

```go
type AuditEntry struct {
    ID          string                 `json:"id"`
    Timestamp   time.Time              `json:"timestamp"`
    Action      string                 `json:"action"`
    Resource    string                 `json:"resource"`
    UserID      string                 `json:"user_id"`
    IPAddress   string                 `json:"ip_address"`
    Details     map[string]interface{} `json:"details"`
    Result      string                 `json:"result"`
}
```

### 3. Encryption

- **At Rest**: Database encryption for sensitive rule data
- **In Transit**: TLS for all network communication
- **Key Management**: Integration with external key management systems

## Monitoring and Observability

### 1. Health Monitoring

```go
type HealthStatus struct {
    IsHealthy      bool          `json:"is_healthy"`
    LastCheck      time.Time     `json:"last_check"`
    SuccessRate    float64       `json:"success_rate"`
    AverageLatency time.Duration `json:"average_latency"`
    ErrorRate      float64       `json:"error_rate"`
}
```

### 2. Performance Metrics

```go
type RulesetMetrics struct {
    ExecutionCount    int64         `json:"execution_count"`
    SuccessCount      int64         `json:"success_count"`
    ErrorCount        int64         `json:"error_count"`
    AverageLatency    time.Duration `json:"average_latency"`
    MemoryUsage       int64         `json:"memory_usage"`
    CPUUsage          float64       `json:"cpu_usage"`
}
```

### 3. Alerting

- **Health Degradation**: Success rate drops below threshold
- **Performance Issues**: Latency exceeds budget
- **Deployment Failures**: Failed deployments or rollbacks
- **Security Events**: Unauthorized access attempts

## Configuration Examples

### Complete Configuration

```yaml
# .effectus/config.yaml
rule_manager:
  git_repository: "https://github.com/company/effectus-rules.git"
  git_branch: "main"
  rules_directory: "rules"
  
  environments:
    - name: "development"
      type: "development"
      config:
        storage: "inmemory"
        hot_reload: true
        auto_deploy: true
    
    - name: "staging"
      type: "staging"
      config:
        storage: "postgres"
        require_approval: false
        health_checks: true
        performance_budget: "200ms"
    
    - name: "production"
      type: "production"
      config:
        storage: "postgres"
        require_approval: true
        approvers: ["team-lead", "engineering-manager"]
        strategy: "canary"
        rollout_percent: 10
        health_check_url: "/health"
        performance_budget: "100ms"
  
  compiler_settings:
    optimization_level: "O2"
    strict_validation: true
    performance_profile: true
  
  storage:
    postgres:
      dsn: "postgres://user:pass@localhost/effectus"
      max_connections: 20
      cache_enabled: true
      cache_ttl: "10m"
    
    redis:
      addr: "localhost:6379"
      key_prefix: "effectus:"
      ttl: "1h"
      cluster_mode: false
```

### Usage Examples

```go
// Initialize storage
postgresStorage, err := NewPostgresRuleStorage(&PostgresStorageConfig{
    DSN:            "postgres://user:pass@localhost/effectus",
    MaxConnections: 20,
    CacheEnabled:   true,
    CacheTTL:       10 * time.Minute,
})

// Create rule manager
config := &RuleManagerConfig{
    GitRepository:    "https://github.com/company/rules.git",
    GitBranch:        "main",
    RulesDirectory:   "rules",
    HotReloadEnabled: true,
    PollInterval:     30 * time.Second,
}

ruleManager, err := NewRuleManager(postgresStorage, config)

// Deploy from git
result, err := ruleManager.DeployFromGit(ctx, "production", &DeploymentOptions{
    Strategy:    "canary",
    HealthCheck: true,
    Timeout:     5 * time.Minute,
})

// Query deployed rulesets
filters := &RulesetFilters{
    Environments: []string{"production"},
    Status:       []RulesetStatus{RulesetStatusDeployed},
}
rulesets, err := postgresStorage.ListRulesets(ctx, filters)

// Get audit trail
auditFilters := &AuditFilters{
    Actions:   []string{"deploy_ruleset", "rollback_deployment"},
    StartTime: time.Now().Add(-24 * time.Hour),
    EndTime:   time.Now(),
}
auditEntries, err := postgresStorage.GetAuditLog(ctx, auditFilters)
```

## Benefits of This Architecture

1. **Development Velocity**: Git-based development with instant feedback
2. **Production Safety**: Multi-stage validation and deployment strategies
3. **Operational Excellence**: Comprehensive monitoring and audit trails
4. **Scalability**: Multiple storage backends for different use cases
5. **Compliance**: Full audit trails and access controls
6. **Reliability**: High availability with multi-backend support
7. **Performance**: Optimized storage and caching strategies

This architecture provides enterprise-grade rule management while maintaining developer productivity and operational safety. 