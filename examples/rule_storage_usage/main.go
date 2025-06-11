package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/effectus/effectus-go/runtime"
)

func main() {
	ctx := context.Background()

	// Example 1: PostgreSQL Storage Setup
	if err := examplePostgresStorage(ctx); err != nil {
		log.Printf("PostgreSQL example failed: %v", err)
	}

	// Example 2: Multi-Backend Setup
	if err := exampleMultiBackendStorage(ctx); err != nil {
		log.Printf("Multi-backend example failed: %v", err)
	}

	// Example 3: Rule Manager with Git Integration
	if err := exampleRuleManagerWorkflow(ctx); err != nil {
		log.Printf("Rule manager example failed: %v", err)
	}

	// Example 4: ABAC-style Policy Storage
	if err := exampleABACPolicyStorage(ctx); err != nil {
		log.Printf("ABAC policy example failed: %v", err)
	}
}

// examplePostgresStorage demonstrates PostgreSQL storage usage
func examplePostgresStorage(ctx context.Context) error {
	fmt.Println("=== PostgreSQL Storage Example ===")

	// Initialize PostgreSQL storage
	config := &runtime.PostgresStorageConfig{
		DSN:                "postgres://user:password@localhost/effectus_rules",
		MaxConnections:     20,
		CacheEnabled:       true,
		CacheTTL:           10 * time.Minute,
		AuditRetentionDays: 365,
		TablePrefix:        "effectus_",
	}

	storage, err := runtime.NewPostgresRuleStorage(config)
	if err != nil {
		return fmt.Errorf("failed to create storage: %w", err)
	}

	// Create a sample ruleset
	sampleRuleset := createSampleRuleset()

	// Store the ruleset
	fmt.Println("Storing ruleset...")
	if err := storage.StoreRuleset(ctx, sampleRuleset); err != nil {
		return fmt.Errorf("failed to store ruleset: %w", err)
	}

	// Retrieve the ruleset
	fmt.Println("Retrieving ruleset...")
	retrieved, err := storage.GetRuleset(ctx, sampleRuleset.Name, sampleRuleset.Version)
	if err != nil {
		return fmt.Errorf("failed to retrieve ruleset: %w", err)
	}

	fmt.Printf("Retrieved ruleset: %s v%s (%d rules)\n",
		retrieved.Name, retrieved.Version, len(retrieved.Ruleset.Rules))

	// List rulesets with filters
	fmt.Println("Listing rulesets...")
	filters := &runtime.RulesetFilters{
		Environments: []string{"production"},
		Status:       []runtime.RulesetStatus{runtime.RulesetStatusReady, runtime.RulesetStatusDeployed},
		Tags:         []string{"customer-management"},
		Limit:        10,
	}

	rulesets, err := storage.ListRulesets(ctx, filters)
	if err != nil {
		return fmt.Errorf("failed to list rulesets: %w", err)
	}

	fmt.Printf("Found %d rulesets\n", len(rulesets))
	for _, rs := range rulesets {
		fmt.Printf("  - %s v%s (%s) - %d rules\n",
			rs.Name, rs.Version, rs.Environment, rs.RuleCount)
	}

	return nil
}

// exampleMultiBackendStorage demonstrates multi-backend storage with failover
func exampleMultiBackendStorage(ctx context.Context) error {
	fmt.Println("\n=== Multi-Backend Storage Example ===")

	// Setup primary storage (PostgreSQL)
	primaryConfig := &runtime.PostgresStorageConfig{
		DSN:            "postgres://user:password@primary-db/effectus",
		MaxConnections: 20,
		CacheEnabled:   true,
		CacheTTL:       5 * time.Minute,
	}
	primary, err := runtime.NewPostgresRuleStorage(primaryConfig)
	if err != nil {
		return fmt.Errorf("failed to create primary storage: %w", err)
	}

	// Setup secondary storage (Redis)
	redisConfig := &runtime.RedisStorageConfig{
		Addr:      "redis-cluster:6379",
		KeyPrefix: "effectus:",
		TTL:       30 * time.Minute,
	}
	redis, err := runtime.NewRedisRuleStorage(redisConfig)
	if err != nil {
		return fmt.Errorf("failed to create Redis storage: %w", err)
	}

	// Setup etcd storage
	etcdConfig := &runtime.EtcdStorageConfig{
		Endpoints:   []string{"etcd1:2379", "etcd2:2379", "etcd3:2379"},
		DialTimeout: 5 * time.Second,
		KeyPrefix:   "/effectus/rules",
		TTL:         60 * time.Minute,
	}
	etcd, err := runtime.NewEtcdRuleStorage(etcdConfig)
	if err != nil {
		return fmt.Errorf("failed to create etcd storage: %w", err)
	}

	// Create multi-backend storage
	multiConfig := &runtime.MultiBackendConfig{
		WriteStrategy:    "majority", // Require majority write success
		ReadStrategy:     "fastest",  // Return fastest response
		FailoverEnabled:  true,
		HealthCheckTTL:   30 * time.Second,
		ConsistencyLevel: "eventual",
	}

	multiStorage := runtime.NewMultiBackendRuleStorage(
		primary,
		[]runtime.RuleStorageBackend{redis, etcd},
		multiConfig,
	)

	// Store with majority write
	sampleRuleset := createHighAvailabilityRuleset()
	fmt.Println("Storing with majority write...")
	if err := multiStorage.StoreRuleset(ctx, sampleRuleset); err != nil {
		return fmt.Errorf("failed to store with majority: %w", err)
	}

	// Read with fastest strategy
	fmt.Println("Reading with fastest strategy...")
	retrieved, err := multiStorage.GetRuleset(ctx, sampleRuleset.Name, sampleRuleset.Version)
	if err != nil {
		return fmt.Errorf("failed to retrieve: %w", err)
	}

	fmt.Printf("Retrieved from fastest backend: %s v%s\n",
		retrieved.Name, retrieved.Version)

	return nil
}

// exampleRuleManagerWorkflow demonstrates the complete rule management workflow
func exampleRuleManagerWorkflow(ctx context.Context) error {
	fmt.Println("\n=== Rule Manager Workflow Example ===")

	// Setup storage backend
	storage := runtime.NewInMemoryRuleStorage() // Use in-memory for demo

	// Create rule manager configuration
	config := &runtime.RuleManagerConfig{
		GitRepository:  "https://github.com/company/effectus-rules.git",
		GitBranch:      "main",
		RulesDirectory: "rules",
		DefaultEnv:     "development",
		Environments: []runtime.Environment{
			{
				Name: "development",
				Type: "development",
				Config: map[string]string{
					"auto_deploy":  "true",
					"hot_reload":   "true",
					"storage_type": "inmemory",
				},
			},
			{
				Name: "staging",
				Type: "staging",
				Config: map[string]string{
					"require_approval": "false",
					"health_checks":    "true",
					"storage_type":     "postgres",
				},
			},
			{
				Name: "production",
				Type: "production",
				Config: map[string]string{
					"require_approval": "true",
					"strategy":         "canary",
					"rollout_percent":  "10",
					"storage_type":     "postgres",
				},
				Approvers: []string{"team-lead", "engineering-manager"},
				Constraints: &runtime.EnvConstraints{
					RequireApproval:   true,
					MaxRuleComplexity: 10,
					PerformanceBudget: "100ms",
					SecurityPolicies:  []string{"data-privacy", "sox-compliance"},
				},
			},
		},
		CompilerSettings: &runtime.CompilerSettings{
			OptimizationLevel:  "O2",
			StrictValidation:   true,
			PerformanceProfile: true,
		},
		DeploymentSettings: &runtime.DeploymentSettings{
			Strategy:           "blue_green",
			RolloutPercent:     10,
			HealthCheckTimeout: 30 * time.Second,
			RollbackOnFailure:  true,
		},
		HotReloadEnabled: true,
		PollInterval:     30 * time.Second,
	}

	// Create rule manager
	ruleManager, err := runtime.NewRuleManager(storage, config)
	if err != nil {
		return fmt.Errorf("failed to create rule manager: %w", err)
	}

	// Start the rule manager
	fmt.Println("Starting rule manager...")
	if err := ruleManager.Start(ctx); err != nil {
		return fmt.Errorf("failed to start rule manager: %w", err)
	}

	// Deploy from git to development
	fmt.Println("Deploying to development...")
	devOptions := &runtime.DeploymentOptions{
		Strategy:    "rolling",
		DryRun:      false,
		Force:       false,
		HealthCheck: true,
		Timeout:     2 * time.Minute,
		Metadata: map[string]string{
			"deployed_by": "auto-deployment",
			"trigger":     "git-push",
		},
	}

	devResult, err := ruleManager.DeployFromGit(ctx, "development", devOptions)
	if err != nil {
		return fmt.Errorf("development deployment failed: %w", err)
	}

	fmt.Printf("Development deployment: %d rulesets deployed (commit: %s)\n",
		devResult.Rulesets, devResult.CommitInfo.Hash[:8])

	// Simulate staging deployment after testing
	fmt.Println("Deploying to staging...")
	stagingOptions := &runtime.DeploymentOptions{
		Strategy:    "blue_green",
		DryRun:      false,
		HealthCheck: true,
		Timeout:     5 * time.Minute,
	}

	stagingResult, err := ruleManager.DeployFromGit(ctx, "staging", stagingOptions)
	if err != nil {
		return fmt.Errorf("staging deployment failed: %w", err)
	}

	fmt.Printf("Staging deployment: %d rulesets deployed\n", stagingResult.Rulesets)

	// Production deployment with approval (simulated)
	fmt.Println("Initiating production deployment...")
	prodOptions := &runtime.DeploymentOptions{
		Strategy:       "canary",
		RolloutPercent: 10,
		HealthCheck:    true,
		Timeout:        10 * time.Minute,
		Metadata: map[string]string{
			"approved_by":     "team-lead",
			"approval_ticket": "DEPLOY-12345",
		},
	}

	// In a real scenario, this would wait for approval
	prodResult, err := ruleManager.DeployFromGit(ctx, "production", prodOptions)
	if err != nil {
		return fmt.Errorf("production deployment failed: %w", err)
	}

	fmt.Printf("Production deployment: %d rulesets deployed with canary strategy\n",
		prodResult.Rulesets)

	return nil
}

// exampleABACPolicyStorage demonstrates ABAC-style policy storage patterns
func exampleABACPolicyStorage(ctx context.Context) error {
	fmt.Println("\n=== ABAC-Style Policy Storage Example ===")

	storage := runtime.NewInMemoryRuleStorage()

	// Create access control policies as rulesets
	accessPolicies := []*runtime.StoredRuleset{
		createAccessControlRuleset("user-management", "v1.0.0"),
		createDataPrivacyRuleset("data-privacy", "v1.0.0"),
		createComplianceRuleset("sox-compliance", "v1.0.0"),
	}

	// Store all policies
	for _, policy := range accessPolicies {
		fmt.Printf("Storing policy: %s\n", policy.Name)
		if err := storage.StoreRuleset(ctx, policy); err != nil {
			return fmt.Errorf("failed to store policy %s: %w", policy.Name, err)
		}
	}

	// Query policies by tags (similar to ABAC attribute queries)
	fmt.Println("Querying access control policies...")
	filters := &runtime.RulesetFilters{
		Tags:        []string{"access-control"},
		Environment: []string{"production"},
		Status:      []runtime.RulesetStatus{runtime.RulesetStatusDeployed},
	}

	policies, err := storage.ListRulesets(ctx, filters)
	if err != nil {
		return fmt.Errorf("failed to query policies: %w", err)
	}

	fmt.Printf("Found %d access control policies:\n", len(policies))
	for _, policy := range policies {
		fmt.Printf("  - %s v%s (owner: %s, team: %s)\n",
			policy.Name, policy.Version, policy.Owner, policy.Team)
	}

	// Demonstrate audit trail for compliance
	fmt.Println("Audit trail for compliance...")
	auditFilters := &runtime.AuditFilters{
		Actions:   []string{"store_ruleset", "deploy_ruleset"},
		Resources: []string{"sox-compliance", "data-privacy"},
		StartTime: time.Now().Add(-24 * time.Hour),
		EndTime:   time.Now(),
		Limit:     100,
	}

	auditEntries, err := storage.GetAuditLog(ctx, auditFilters)
	if err != nil {
		return fmt.Errorf("failed to get audit log: %w", err)
	}

	fmt.Printf("Audit entries: %d\n", len(auditEntries))
	for _, entry := range auditEntries {
		fmt.Printf("  - %s: %s on %s (result: %s)\n",
			entry.Timestamp.Format("15:04:05"), entry.Action, entry.Resource, entry.Result)
	}

	return nil
}

// Helper functions to create sample rulesets

func createSampleRuleset() *runtime.StoredRuleset {
	return &runtime.StoredRuleset{
		ID:          "user-onboarding-prod-v1.0.0",
		Name:        "user-onboarding",
		Version:     "v1.0.0",
		Environment: "production",
		CreatedAt:   time.Now(),
		CreatedBy:   "engineering-team",
		GitCommit:   "abc123def456",
		GitBranch:   "main",
		Status:      runtime.RulesetStatusReady,
		Ruleset: &runtime.CompiledRuleset{
			Name:        "user-onboarding",
			Version:     "v1.0.0",
			Description: "User onboarding automation rules",
			Rules: []runtime.CompiledRule{
				{
					Name:        "welcome_new_user",
					Type:        runtime.RuleTypeList,
					Description: "Send welcome email to new users",
					Priority:    10,
				},
				{
					Name:        "setup_user_profile",
					Type:        runtime.RuleTypeFlow,
					Description: "Guide user through profile setup",
					Priority:    20,
				},
			},
			Dependencies: []string{"email-service", "user-service"},
			Capabilities: []string{"send_email", "update_user_profile"},
		},
		Tags:        []string{"onboarding", "customer-management"},
		Description: "Automated user onboarding workflow",
		Owner:       "product-team",
		Team:        "customer-success",
		Metadata: map[string]string{
			"business_impact": "high",
			"compliance":      "gdpr",
		},
	}
}

func createHighAvailabilityRuleset() *runtime.StoredRuleset {
	ruleset := createSampleRuleset()
	ruleset.ID = "ha-ruleset-prod-v1.0.0"
	ruleset.Name = "high-availability-rules"
	ruleset.Tags = append(ruleset.Tags, "high-availability", "critical")
	ruleset.Metadata["sla_tier"] = "tier1"
	return ruleset
}

func createAccessControlRuleset(name, version string) *runtime.StoredRuleset {
	return &runtime.StoredRuleset{
		ID:          fmt.Sprintf("%s-prod-%s", name, version),
		Name:        name,
		Version:     version,
		Environment: "production",
		CreatedAt:   time.Now(),
		CreatedBy:   "security-team",
		Status:      runtime.RulesetStatusDeployed,
		Ruleset: &runtime.CompiledRuleset{
			Name:        name,
			Version:     version,
			Description: "Access control and authorization rules",
			Rules: []runtime.CompiledRule{
				{
					Name:        "admin_access_control",
					Type:        runtime.RuleTypeList,
					Description: "Control admin access to sensitive resources",
					Priority:    1,
				},
			},
			Capabilities: []string{"authorize_access", "audit_access"},
		},
		Tags:        []string{"access-control", "security", "authorization"},
		Description: "Access control policy enforcement",
		Owner:       "security-team",
		Team:        "security",
		Metadata: map[string]string{
			"policy_type":  "access_control",
			"compliance":   "iso27001",
			"review_cycle": "quarterly",
		},
	}
}

func createDataPrivacyRuleset(name, version string) *runtime.StoredRuleset {
	return &runtime.StoredRuleset{
		ID:          fmt.Sprintf("%s-prod-%s", name, version),
		Name:        name,
		Version:     version,
		Environment: "production",
		CreatedAt:   time.Now(),
		CreatedBy:   "privacy-team",
		Status:      runtime.RulesetStatusDeployed,
		Ruleset: &runtime.CompiledRuleset{
			Name:        name,
			Version:     version,
			Description: "Data privacy and protection rules",
			Rules: []runtime.CompiledRule{
				{
					Name:        "pii_access_control",
					Type:        runtime.RuleTypeList,
					Description: "Control access to personally identifiable information",
					Priority:    1,
				},
				{
					Name:        "data_retention_policy",
					Type:        runtime.RuleTypeFlow,
					Description: "Enforce data retention and deletion policies",
					Priority:    2,
				},
			},
			Capabilities: []string{"mask_pii", "delete_data", "audit_access"},
		},
		Tags:        []string{"data-privacy", "gdpr", "compliance"},
		Description: "Data privacy policy enforcement",
		Owner:       "privacy-team",
		Team:        "legal",
		Metadata: map[string]string{
			"policy_type": "data_privacy",
			"compliance":  "gdpr,ccpa",
			"scope":       "global",
		},
	}
}

func createComplianceRuleset(name, version string) *runtime.StoredRuleset {
	return &runtime.StoredRuleset{
		ID:          fmt.Sprintf("%s-prod-%s", name, version),
		Name:        name,
		Version:     version,
		Environment: "production",
		CreatedAt:   time.Now(),
		CreatedBy:   "compliance-team",
		Status:      runtime.RulesetStatusDeployed,
		Ruleset: &runtime.CompiledRuleset{
			Name:        name,
			Version:     version,
			Description: "SOX compliance monitoring and enforcement",
			Rules: []runtime.CompiledRule{
				{
					Name:        "financial_transaction_monitoring",
					Type:        runtime.RuleTypeList,
					Description: "Monitor financial transactions for compliance",
					Priority:    1,
				},
				{
					Name:        "audit_trail_enforcement",
					Type:        runtime.RuleTypeFlow,
					Description: "Ensure comprehensive audit trails",
					Priority:    2,
				},
			},
			Capabilities: []string{"audit_transaction", "generate_report"},
		},
		Tags:        []string{"compliance", "sox", "financial"},
		Description: "SOX compliance enforcement rules",
		Owner:       "compliance-team",
		Team:        "finance",
		Metadata: map[string]string{
			"policy_type":    "compliance",
			"regulation":     "sox",
			"audit_required": "true",
		},
	}
}
