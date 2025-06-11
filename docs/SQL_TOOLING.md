# Modern SQL Tooling with sqlc and goose

Effectus uses modern SQL tooling to provide type-safe database operations and robust schema management:

- **[sqlc](https://sqlc.dev/)**: Generates type-safe Go code from SQL queries
- **[goose](https://pressly.github.io/goose/)**: Database migration management
- **[pgx](https://github.com/jackc/pgx)**: High-performance PostgreSQL driver

## Benefits of This Approach

### 🔒 **Type Safety**
- All database operations are compile-time type-checked
- No more runtime errors from typos in column names
- Automatic handling of nullable fields

### 🚀 **Performance**
- Generated code is optimized for performance
- Uses pgx driver for maximum PostgreSQL performance
- Connection pooling with configurable parameters

### 🛡️ **Maintainability**
- Schema changes are versioned and trackable
- Database migrations are reproducible across environments
- No manual SQL string concatenation

### 🔧 **Developer Experience**
- IDE autocompletion for all database operations
- Clear separation of SQL logic from Go code
- Easy testing with database fixtures

## Architecture Overview

```mermaid
graph TB
    subgraph "Schema Management"
        M1[Migration Files<br/>*.sql] --> G[goose<br/>Migration Engine]
        G --> DB[(PostgreSQL<br/>Database)]
    end
    
    subgraph "Code Generation"
        Q[SQL Queries<br/>queries/*.sql] --> S[sqlc<br/>Code Generator]
        M1 --> S
        S --> GC[Generated Go Code<br/>internal/db/*.go]
    end
    
    subgraph "Application Layer"
        GC --> PS[PostgresStorage<br/>Implementation]
        PS --> RS[RuleStorageBackend<br/>Interface]
        RS --> APP[Application Code]
    end
    
    PS --> DB
```

## Setup Instructions

### 1. Install Tools

```bash
# Install sqlc
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest

# Install goose
go install github.com/pressly/goose/v3/cmd/goose@latest

# Verify installations
sqlc version
goose -version
```

### 2. Database Setup

```bash
# Create development database
createdb effectus_dev

# Set environment variables
export EFFECTUS_DB_DSN="postgres://user:password@localhost/effectus_dev?sslmode=disable"

# Run initial migration
effectusc migrate up
```

### 3. Code Generation

```bash
# Generate Go code from SQL queries
cd effectus-go/runtime
sqlc generate

# This creates files in internal/db/
ls internal/db/
# db.go  models.go  querier.go  rulesets.sql.go  deployments.sql.go  audit.sql.go
```

## Migration Management

### Creating Migrations

```bash
# Create a new migration
effectusc migrate create add_user_preferences

# Creates: effectus-go/runtime/migrations/002_add_user_preferences.sql
```

Example migration file:
```sql
-- +goose Up
ALTER TABLE rulesets ADD COLUMN user_preferences JSONB DEFAULT '{}';
CREATE INDEX idx_rulesets_user_preferences ON rulesets USING GIN(user_preferences);

-- +goose Down  
DROP INDEX idx_rulesets_user_preferences;
ALTER TABLE rulesets DROP COLUMN user_preferences;
```

### Running Migrations

```bash
# Run all pending migrations
effectusc migrate up

# Run migrations one by one
effectusc migrate up --by-one

# Migrate to specific version
effectusc migrate up --to-version 20240115000001

# Check migration status
effectusc migrate status

# Show current version
effectusc migrate version
```

### Development Workflows

```bash
# Fresh start (development only)
effectusc migrate dev fresh --confirm

# Reset database and run all migrations
effectusc migrate reset --confirm
effectusc migrate up

# Rollback last migration
effectusc migrate down

# Rollback multiple migrations
effectusc migrate quick rollback 3

# Redo last migration
effectusc migrate redo
```

## Writing SQL Queries

### Query File Structure

Queries are organized by domain in the `queries/` directory:

```
effectus-go/runtime/queries/
├── rulesets.sql      # Ruleset operations
├── deployments.sql   # Deployment operations
├── audit.sql         # Audit log operations
└── metrics.sql       # Performance metrics
```

### sqlc Comments

sqlc uses special comments to generate Go code:

```sql
-- name: CreateRuleset :one
INSERT INTO rulesets (name, version, environment, status, ruleset_data)
VALUES ($1, $2, $3, $4, $5) 
RETURNING *;

-- name: ListRulesets :many
SELECT * FROM rulesets 
WHERE environment = $1 AND status = ANY($2::ruleset_status[])
ORDER BY created_at DESC
LIMIT $3;

-- name: DeleteRuleset :exec
DELETE FROM rulesets WHERE id = $1;

-- name: GetRulesetStats :one
SELECT 
    COUNT(*) as total_rulesets,
    COUNT(*) FILTER (WHERE status = 'deployed') as deployed_rulesets
FROM rulesets;
```

### Parameter Types

sqlc supports PostgreSQL-specific types:

```sql
-- Arrays
WHERE tags && $1::text[]

-- JSONB
WHERE metadata @> $1::jsonb

-- Enums
WHERE status = $1::ruleset_status

-- Nullable parameters
WHERE ($1::text IS NULL OR name = $1)

-- Time ranges
WHERE created_at BETWEEN $1::timestamptz AND $2::timestamptz
```

## Generated Go Code Usage

### Basic Operations

```go
package main

import (
    "context"
    "github.com/effectus/effectus-go/runtime/internal/db"
    "github.com/jackc/pgx/v5/pgxpool"
)

func main() {
    // Create connection pool
    pool, err := pgxpool.New(context.Background(), dsn)
    if err != nil {
        log.Fatal(err)
    }
    defer pool.Close()

    // Create queries instance
    queries := db.New(pool)

    // Create a ruleset
    ruleset, err := queries.CreateRuleset(ctx, db.CreateRulesetParams{
        Name:        "user-onboarding",
        Version:     "v1.0.0", 
        Environment: "production",
        Status:      db.RulesetStatusReady,
        RulesetData: rulesetJSON,
        RuleCount:   5,
        Description: &description,
        Tags:        []string{"onboarding", "users"},
        Owner:       &owner,
        CreatedBy:   &userID,
    })
    if err != nil {
        log.Fatal(err)
    }

    // List rulesets with filters
    rulesets, err := queries.ListRulesets(ctx, db.ListRulesetsParams{
        Column2: []string{"production"},  // environments
        Column3: []db.RulesetStatus{db.RulesetStatusDeployed},
        Limit:   50,
    })
    if err != nil {
        log.Fatal(err)
    }

    // Update ruleset status
    updated, err := queries.UpdateRulesetStatus(ctx, db.UpdateRulesetStatusParams{
        ID:        ruleset.ID,
        Status:    db.RulesetStatusDeployed,
        UpdatedBy: &userID,
    })
    if err != nil {
        log.Fatal(err)
    }
}
```

### Working with Transactions

```go
func (p *PostgresStorage) StoreRulesetWithAudit(ctx context.Context, ruleset *StoredRuleset) error {
    // Start transaction
    tx, err := p.pool.Begin(ctx)
    if err != nil {
        return err
    }
    defer tx.Rollback(ctx)

    // Use transaction-aware queries
    qtx := p.queries.WithTx(tx)

    // Store ruleset
    dbRuleset, err := qtx.CreateRuleset(ctx, db.CreateRulesetParams{
        // ... parameters
    })
    if err != nil {
        return err
    }

    // Record audit entry
    _, err = qtx.CreateAuditEntry(ctx, db.CreateAuditEntryParams{
        Action:     "store_ruleset",
        Resource:   ruleset.Name,
        ResourceID: &dbRuleset.ID,
        UserID:     &ruleset.CreatedBy,
        Result:     "success",
    })
    if err != nil {
        return err
    }

    // Commit transaction
    return tx.Commit(ctx)
}
```

### Handling Complex Queries

```go
// Search with full-text search and ranking
func (p *PostgresStorage) SearchRulesets(ctx context.Context, query string, limit int32) ([]*RulesetSearchResult, error) {
    results, err := p.queries.SearchRulesets(ctx, db.SearchRulesetsParams{
        Column1: query,  // search term
        Limit:   limit,
    })
    if err != nil {
        return nil, err
    }

    // Convert to domain objects
    searchResults := make([]*RulesetSearchResult, len(results))
    for i, result := range results {
        searchResults[i] = &RulesetSearchResult{
            Ruleset: convertDBRuleset(&result.Ruleset),
            Rank:    result.Rank, // PostgreSQL ts_rank score
        }
    }

    return searchResults, nil
}
```

## Configuration

### sqlc Configuration (`sqlc.yaml`)

```yaml
version: "2"
sql:
  - engine: "postgresql"
    queries: "queries"
    schema: "migrations"
    gen:
      go:
        package: "db"
        out: "internal/db"
        sql_package: "pgx/v5"
        emit_json_tags: true
        emit_db_tags: true
        emit_prepared_queries: true
        emit_interface: true
        overrides:
          - column: "rulesets.id"
            go_type: "github.com/google/uuid.UUID"
          - column: "rulesets.ruleset_data"
            go_type: "json.RawMessage"
          - column: "rulesets.tags"
            go_type: "[]string"
```

### Storage Configuration

```yaml
# .effectus/storage.yaml
type: postgres
postgres:
  dsn: "postgres://user:password@localhost/effectus?sslmode=disable"
  max_connections: 25
  conn_max_lifetime: "1h"
  conn_max_idle_time: "30m"
  migrations_path: "effectus-go/runtime/migrations"
  auto_migrate: true
  prepared_statements: true
  cache_enabled: true
  cache_ttl: "10m"
  audit_retention_days: 365
  metrics_enabled: true
  log_queries: false
```

## Testing

### Unit Tests with Database

```go
func TestPostgresStorage(t *testing.T) {
    // Setup test database
    testDB := setupTestDB(t)
    defer testDB.Close()

    storage, err := NewPostgresStorage(&PostgresStorageConfig{
        DSN:            testDB.DSN,
        AutoMigrate:    true,
        MigrationsPath: "../migrations",
    })
    require.NoError(t, err)
    defer storage.Close()

    // Test operations
    t.Run("StoreRuleset", func(t *testing.T) {
        ruleset := &StoredRuleset{
            Name:        "test-ruleset",
            Version:     "v1.0.0",
            Environment: "test",
            // ...
        }

        err := storage.StoreRuleset(context.Background(), ruleset)
        assert.NoError(t, err)
        assert.NotEmpty(t, ruleset.ID)
    })

    t.Run("GetRuleset", func(t *testing.T) {
        retrieved, err := storage.GetRuleset(context.Background(), "test-ruleset", "v1.0.0")
        assert.NoError(t, err)
        assert.Equal(t, "test-ruleset", retrieved.Name)
    })
}

func setupTestDB(t *testing.T) *TestDB {
    // Create test database and run migrations
    // Return connection info for tests
}
```

### Integration Tests

```go
func TestMigrations(t *testing.T) {
    db := setupTestDB(t)
    defer db.Close()

    // Test migration up
    err := goose.Up(db.DB, "../migrations")
    assert.NoError(t, err)

    // Test schema exists
    var count int
    err = db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'rulesets'").Scan(&count)
    assert.NoError(t, err)
    assert.Equal(t, 1, count)

    // Test migration down
    err = goose.Down(db.DB, "../migrations")
    assert.NoError(t, err)
}
```

## Production Considerations

### Connection Pooling

```go
config := &PostgresStorageConfig{
    DSN:             dsn,
    MaxConnections:  25,                  // Adjust based on load
    ConnMaxLifetime: time.Hour,           // Recycle connections
    ConnMaxIdleTime: 30 * time.Minute,    // Close idle connections
}
```

### Monitoring and Metrics

```go
// Enable query logging in development
config.LogQueries = true

// Enable metrics collection
config.MetricsEnabled = true

// Monitor connection pool
pool.Stat().MaxConns
pool.Stat().AcquiredConns
pool.Stat().IdleConns
```

### Security

```sql
-- Use parameterized queries (sqlc handles this automatically)
-- name: GetUserRulesets :many
SELECT * FROM rulesets 
WHERE owner = $1 AND environment = $2;  -- Never concatenate strings!
```

### Performance Optimization

```sql
-- Add indexes for common query patterns
CREATE INDEX idx_rulesets_owner_env ON rulesets(owner, environment);
CREATE INDEX idx_rulesets_tags ON rulesets USING GIN(tags);
CREATE INDEX idx_audit_log_timestamp ON audit_log(timestamp);

-- Use EXPLAIN ANALYZE to optimize queries
EXPLAIN ANALYZE SELECT * FROM rulesets WHERE tags && ARRAY['production'];
```

## Migration Strategies

### Environment Promotion

```bash
# Development → Staging
effectusc migrate status --dsn $STAGING_DSN
effectusc migrate up --dsn $STAGING_DSN

# Staging → Production
effectusc migrate status --dsn $PRODUCTION_DSN
effectusc migrate up --dsn $PRODUCTION_DSN --by-one
```

### Rollback Strategies

```bash
# Check what would be rolled back
effectusc migrate status

# Rollback specific number of migrations
effectusc migrate quick rollback 2

# Rollback to specific version
effectusc migrate down --to-version 20240115000001
```

### Zero-Downtime Migrations

```sql
-- +goose Up
-- Step 1: Add new column (nullable)
ALTER TABLE rulesets ADD COLUMN new_field TEXT;

-- Step 2: Populate data (separate migration)
-- UPDATE rulesets SET new_field = 'default_value' WHERE new_field IS NULL;

-- Step 3: Add constraint (separate migration)  
-- ALTER TABLE rulesets ALTER COLUMN new_field SET NOT NULL;

-- +goose Down
ALTER TABLE rulesets DROP COLUMN new_field;
```

## Troubleshooting

### Common Issues

1. **Migration Conflicts**
   ```bash
   # Check migration status
   effectusc migrate status
   
   # Manual fix in database, then mark as applied
   effectusc migrate up --to-version 20240115000001
   ```

2. **Type Mismatches**
   ```bash
   # Regenerate code after schema changes
   sqlc generate
   ```

3. **Connection Issues**
   ```bash
   # Test database connectivity
   effectusc storage health --dsn $DATABASE_URL
   ```

### Debug Mode

```bash
# Enable query logging
export EFFECTUS_LOG_QUERIES=true

# Enable migration debug
export GOOSE_DEBUG=true

# Run with verbose output
effectusc migrate up --verbose
```

This modern SQL tooling setup provides a robust foundation for database operations in Effectus, combining type safety, performance, and maintainability. 