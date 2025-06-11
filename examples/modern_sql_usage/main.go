package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/effectus/effectus-go/runtime"
	"github.com/google/uuid"
)

func main() {
	fmt.Println("=== Modern SQL Tooling Example ===")

	// Example 1: Basic setup with PostgreSQL storage
	if err := exampleBasicSetup(); err != nil {
		log.Printf("Basic setup example failed: %v", err)
	}

	// Example 2: Type-safe database operations
	if err := exampleTypeSafeOperations(); err != nil {
		log.Printf("Type-safe operations example failed: %v", err)
	}

	// Example 3: Advanced querying with filters
	if err := exampleAdvancedQuerying(); err != nil {
		log.Printf("Advanced querying example failed: %v", err)
	}

	// Example 4: Transaction handling
	if err := exampleTransactionHandling(); err != nil {
		log.Printf("Transaction handling example failed: %v", err)
	}
}

// exampleBasicSetup demonstrates basic PostgreSQL storage setup
func exampleBasicSetup() error {
	fmt.Println("\n--- Example 1: Basic Setup ---")

	// Configuration with modern tooling
	config := &runtime.PostgresStorageConfig{
		DSN:                "postgres://effectus:effectus@localhost/effectus_dev?sslmode=disable",
		MaxConnections:     25,
		ConnMaxLifetime:    time.Hour,
		ConnMaxIdleTime:    30 * time.Minute,
		MigrationsPath:     "../runtime/migrations",
		AutoMigrate:        true,
		PreparedStatements: true,
		CacheEnabled:       true,
		CacheTTL:           10 * time.Minute,
		MetricsEnabled:     true,
		LogQueries:         false, // Set to true for debugging
	}

	// Create storage backend
	storage, err := runtime.NewPostgresStorage(config)
	if err != nil {
		return fmt.Errorf("failed to create storage: %w", err)
	}
	defer storage.Close()

	fmt.Println("✅ PostgreSQL storage initialized with modern tooling")
	fmt.Println("   - Migrations: Auto-run with goose")
	fmt.Println("   - Type safety: Ensured by sqlc")
	fmt.Println("   - Connection pooling: Optimized with pgx")

	// Health check
	ctx := context.Background()
	if err := storage.HealthCheck(ctx); err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	fmt.Println("✅ Health check passed")
	return nil
}

// exampleTypeSafeOperations demonstrates type-safe database operations
func exampleTypeSafeOperations() error {
	fmt.Println("\n--- Example 2: Type-Safe Operations ---")

	storage, err := createStorageForExample()
	if err != nil {
		return err
	}
	defer storage.Close()

	ctx := context.Background()

	// Create a sample ruleset with type safety
	ruleset := &runtime.StoredRuleset{
		Name:        "modern-sql-example",
		Version:     "v1.0.0",
		Environment: "development",
		Status:      runtime.RulesetStatusReady,
		Ruleset: &runtime.CompiledRuleset{
			Name:        "modern-sql-example",
			Version:     "v1.0.0",
			Description: "Example ruleset for modern SQL tooling",
			Rules: []runtime.CompiledRule{
				{
					Name:        "example_rule",
					Type:        runtime.RuleTypeList,
					Description: "Example rule for demonstration",
					Priority:    10,
				},
			},
			Dependencies: []string{"email-service"},
			Capabilities: []string{"send_email"},
		},
		Description: "Example ruleset demonstrating modern SQL tooling",
		Tags:        []string{"example", "modern-sql", "demo"},
		Owner:       "engineering-team",
		Team:        "platform",
		CreatedBy:   "example-user",
		Metadata: map[string]string{
			"purpose":    "demonstration",
			"technology": "sqlc+goose",
		},
		CompiledAt:      time.Now(),
		CompilerVersion: "v1.0.0",
		SchemaVersion:   "v1.0.0",
	}

	// Store ruleset - this uses type-safe generated code
	fmt.Println("Storing ruleset with type-safe operations...")
	if err := storage.StoreRuleset(ctx, ruleset); err != nil {
		return fmt.Errorf("failed to store ruleset: %w", err)
	}

	fmt.Printf("✅ Stored ruleset with ID: %s\n", ruleset.ID)
	fmt.Println("   - All parameters are type-checked at compile time")
	fmt.Println("   - No SQL injection vulnerabilities")
	fmt.Println("   - Automatic handling of nullable fields")

	// Retrieve ruleset - also type-safe
	fmt.Println("Retrieving ruleset...")
	retrieved, err := storage.GetRuleset(ctx, ruleset.Name, ruleset.Version)
	if err != nil {
		return fmt.Errorf("failed to retrieve ruleset: %w", err)
	}

	fmt.Printf("✅ Retrieved ruleset: %s v%s\n", retrieved.Name, retrieved.Version)
	fmt.Printf("   - Rule count: %d\n", len(retrieved.Ruleset.Rules))
	fmt.Printf("   - Tags: %v\n", retrieved.Tags)

	return nil
}

// exampleAdvancedQuerying demonstrates advanced querying capabilities
func exampleAdvancedQuerying() error {
	fmt.Println("\n--- Example 3: Advanced Querying ---")

	storage, err := createStorageForExample()
	if err != nil {
		return err
	}
	defer storage.Close()

	ctx := context.Background()

	// Create multiple rulesets for filtering examples
	rulesets := []*runtime.StoredRuleset{
		{
			Name: "user-management", Version: "v1.0.0", Environment: "production",
			Status: runtime.RulesetStatusDeployed, Tags: []string{"users", "production"},
			Owner: "user-team", Team: "backend", CreatedBy: "alice",
			Ruleset: &runtime.CompiledRuleset{Rules: make([]runtime.CompiledRule, 3)},
		},
		{
			Name: "order-processing", Version: "v1.1.0", Environment: "production",
			Status: runtime.RulesetStatusDeployed, Tags: []string{"orders", "production"},
			Owner: "order-team", Team: "backend", CreatedBy: "bob",
			Ruleset: &runtime.CompiledRuleset{Rules: make([]runtime.CompiledRule, 5)},
		},
		{
			Name: "analytics", Version: "v2.0.0", Environment: "staging",
			Status: runtime.RulesetStatusReady, Tags: []string{"analytics", "experimental"},
			Owner: "data-team", Team: "analytics", CreatedBy: "charlie",
			Ruleset: &runtime.CompiledRuleset{Rules: make([]runtime.CompiledRule, 2)},
		},
	}

	// Store test rulesets
	fmt.Println("Creating test rulesets...")
	for _, rs := range rulesets {
		if err := storage.StoreRuleset(ctx, rs); err != nil {
			return fmt.Errorf("failed to store test ruleset %s: %w", rs.Name, err)
		}
	}

	// Example 1: Filter by environment
	fmt.Println("\n1. Filtering by environment (production):")
	prodFilters := &runtime.RulesetFilters{
		Environments: []string{"production"},
		Limit:        10,
	}
	prodRulesets, err := storage.ListRulesets(ctx, prodFilters)
	if err != nil {
		return fmt.Errorf("failed to list production rulesets: %w", err)
	}
	for _, rs := range prodRulesets {
		fmt.Printf("   - %s v%s (%d rules)\n", rs.Name, rs.Version, rs.RuleCount)
	}

	// Example 2: Filter by team and status
	fmt.Println("\n2. Filtering by team (backend) and status (deployed):")
	backendFilters := &runtime.RulesetFilters{
		Team:   "backend",
		Status: []runtime.RulesetStatus{runtime.RulesetStatusDeployed},
		Limit:  10,
	}
	backendRulesets, err := storage.ListRulesets(ctx, backendFilters)
	if err != nil {
		return fmt.Errorf("failed to list backend rulesets: %w", err)
	}
	for _, rs := range backendRulesets {
		fmt.Printf("   - %s v%s (owner: %s)\n", rs.Name, rs.Version, rs.Owner)
	}

	// Example 3: Filter by tags
	fmt.Println("\n3. Filtering by tags (production):")
	tagFilters := &runtime.RulesetFilters{
		Tags:  []string{"production"},
		Limit: 10,
	}
	taggedRulesets, err := storage.ListRulesets(ctx, tagFilters)
	if err != nil {
		return fmt.Errorf("failed to list tagged rulesets: %w", err)
	}
	for _, rs := range taggedRulesets {
		fmt.Printf("   - %s v%s (tags: %v)\n", rs.Name, rs.Version, rs.Tags)
	}

	fmt.Println("\n✅ Advanced querying completed")
	fmt.Println("   - All queries are type-safe and compiled")
	fmt.Println("   - Complex filters handled efficiently")
	fmt.Println("   - PostgreSQL-specific features utilized")

	return nil
}

// exampleTransactionHandling demonstrates transaction handling
func exampleTransactionHandling() error {
	fmt.Println("\n--- Example 4: Transaction Handling ---")

	storage, err := createStorageForExample()
	if err != nil {
		return err
	}
	defer storage.Close()

	ctx := context.Background()

	// Create a ruleset that will be stored with audit trail
	ruleset := &runtime.StoredRuleset{
		Name:        "transaction-example",
		Version:     "v1.0.0",
		Environment: "development",
		Status:      runtime.RulesetStatusReady,
		Ruleset: &runtime.CompiledRuleset{
			Name:        "transaction-example",
			Version:     "v1.0.0",
			Description: "Example with transaction handling",
			Rules:       make([]runtime.CompiledRule, 1),
		},
		CreatedBy: "example-user",
		Owner:     "platform-team",
	}

	// Store ruleset (this internally uses transactions for consistency)
	fmt.Println("Storing ruleset with automatic audit trail...")
	if err := storage.StoreRuleset(ctx, ruleset); err != nil {
		return fmt.Errorf("failed to store ruleset: %w", err)
	}

	fmt.Printf("✅ Stored ruleset: %s\n", ruleset.ID)

	// Record additional audit entry
	auditEntry := &runtime.AuditEntry{
		Action:      "example_operation",
		Resource:    ruleset.Name,
		Version:     ruleset.Version,
		Environment: ruleset.Environment,
		UserID:      "example-user",
		UserEmail:   "user@example.com",
		IPAddress:   "192.168.1.100",
		Details: map[string]interface{}{
			"operation": "transaction_example",
			"success":   true,
		},
		Result: "success",
	}

	// Convert string ID back to UUID for audit entry
	if rulesetID, err := uuid.Parse(ruleset.ID); err == nil {
		auditEntry.ResourceID = &rulesetID
	}

	if err := storage.RecordActivity(ctx, auditEntry); err != nil {
		return fmt.Errorf("failed to record audit entry: %w", err)
	}

	fmt.Println("✅ Audit entry recorded")

	// Query audit logs
	fmt.Println("Querying audit logs...")
	auditFilters := &runtime.AuditFilters{
		Resources: []string{ruleset.Name},
		Limit:     10,
	}

	auditLogs, err := storage.GetAuditLog(ctx, auditFilters)
	if err != nil {
		return fmt.Errorf("failed to get audit logs: %w", err)
	}

	for _, entry := range auditLogs {
		fmt.Printf("   - %s: %s on %s (result: %s)\n",
			entry.Timestamp.Format("15:04:05"), entry.Action, entry.Resource, entry.Result)
	}

	fmt.Println("\n✅ Transaction handling completed")
	fmt.Println("   - All operations are ACID compliant")
	fmt.Println("   - Automatic audit trail maintenance")
	fmt.Println("   - Type-safe transaction management")

	return nil
}

// Helper function to create storage for examples
func createStorageForExample() (*runtime.PostgresStorage, error) {
	config := &runtime.PostgresStorageConfig{
		DSN:                "postgres://effectus:effectus@localhost/effectus_dev?sslmode=disable",
		MaxConnections:     10,
		ConnMaxLifetime:    30 * time.Minute,
		ConnMaxIdleTime:    5 * time.Minute,
		MigrationsPath:     "../runtime/migrations",
		AutoMigrate:        false, // Assume already migrated
		PreparedStatements: true,
		CacheEnabled:       false, // Disable for examples
		MetricsEnabled:     false, // Disable for examples
		LogQueries:         false, // Set to true for debugging
	}

	return runtime.NewPostgresStorage(config)
}

// Example migration workflow (would be run separately)
func exampleMigrationWorkflow() {
	fmt.Println("\n=== Migration Workflow Example ===")
	fmt.Println("# Set up development environment")
	fmt.Println("make dev-setup")
	fmt.Println("")
	fmt.Println("# Create a new migration")
	fmt.Println("make migrate-create NAME=add_performance_metrics")
	fmt.Println("")
	fmt.Println("# Edit the migration file, then apply it")
	fmt.Println("make migrate-up")
	fmt.Println("")
	fmt.Println("# Generate new Go code from updated schema")
	fmt.Println("make generate")
	fmt.Println("")
	fmt.Println("# Run tests to ensure everything works")
	fmt.Println("make test")
	fmt.Println("")
	fmt.Println("# Deploy to staging")
	fmt.Println("STAGING_DSN=$STAGING_URL make staging-migrate")
	fmt.Println("")
	fmt.Println("# Deploy to production (with confirmation)")
	fmt.Println("PROD_DSN=$PRODUCTION_URL make prod-migrate")
}
