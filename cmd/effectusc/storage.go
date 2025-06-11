package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/effectus/effectus-go/runtime"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// storageCmd represents the storage command group
var storageCmd = &cobra.Command{
	Use:   "storage",
	Short: "Manage rule storage backends and deployments",
	Long: `The storage command group provides operations for managing rule storage
across different backends (postgres, redis, etcd, inmemory) and handling
deployments, versioning, and audit trails.`,
}

// Storage subcommands
var (
	storageListCmd     = createStorageListCmd()
	storageShowCmd     = createStorageShowCmd()
	storageDeployCmd   = createStorageDeployCmd()
	storageRollbackCmd = createStorageRollbackCmd()
	storageAuditCmd    = createStorageAuditCmd()
	storageHealthCmd   = createStorageHealthCmd()
	storageCleanupCmd  = createStorageCleanupCmd()
	storageConfigCmd   = createStorageConfigCmd()
)

func init() {
	rootCmd.AddCommand(storageCmd)

	// Add subcommands
	storageCmd.AddCommand(storageListCmd)
	storageCmd.AddCommand(storageShowCmd)
	storageCmd.AddCommand(storageDeployCmd)
	storageCmd.AddCommand(storageRollbackCmd)
	storageCmd.AddCommand(storageAuditCmd)
	storageCmd.AddCommand(storageHealthCmd)
	storageCmd.AddCommand(storageCleanupCmd)
	storageCmd.AddCommand(storageConfigCmd)
}

// createStorageListCmd creates the storage list command
func createStorageListCmd() *cobra.Command {
	var (
		environment string
		status      []string
		tags        []string
		owner       string
		team        string
		format      string
		limit       int
		showDetails bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List stored rulesets",
		Long: `List stored rulesets with optional filtering by environment, status, tags, etc.

Examples:
  # List all rulesets
  effectusc storage list

  # List production rulesets
  effectusc storage list --environment production

  # List rulesets by team
  effectusc storage list --team customer-success

  # List with specific status
  effectusc storage list --status deployed,ready

  # Show detailed view
  effectusc storage list --details`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// Load storage configuration
			storage, err := loadStorageBackend()
			if err != nil {
				return fmt.Errorf("failed to initialize storage: %w", err)
			}

			// Build filters
			filters := &runtime.RulesetFilters{
				Owner: owner,
				Team:  team,
				Tags:  tags,
				Limit: limit,
			}

			if environment != "" {
				filters.Environments = []string{environment}
			}

			if len(status) > 0 {
				filters.Status = make([]runtime.RulesetStatus, len(status))
				for i, s := range status {
					filters.Status[i] = runtime.RulesetStatus(s)
				}
			}

			// List rulesets
			rulesets, err := storage.ListRulesets(ctx, filters)
			if err != nil {
				return fmt.Errorf("failed to list rulesets: %w", err)
			}

			// Output results
			return outputRulesets(rulesets, format, showDetails)
		},
	}

	cmd.Flags().StringVar(&environment, "environment", "", "Filter by environment")
	cmd.Flags().StringSliceVar(&status, "status", nil, "Filter by status (comma-separated)")
	cmd.Flags().StringSliceVar(&tags, "tags", nil, "Filter by tags (comma-separated)")
	cmd.Flags().StringVar(&owner, "owner", "", "Filter by owner")
	cmd.Flags().StringVar(&team, "team", "", "Filter by team")
	cmd.Flags().StringVar(&format, "format", "table", "Output format (table, json, yaml)")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum number of results")
	cmd.Flags().BoolVar(&showDetails, "details", false, "Show detailed information")

	return cmd
}

// createStorageShowCmd creates the storage show command
func createStorageShowCmd() *cobra.Command {
	var (
		format    string
		showRules bool
	)

	cmd := &cobra.Command{
		Use:   "show <name> <version>",
		Short: "Show detailed information about a stored ruleset",
		Long: `Show detailed information about a specific ruleset including metadata,
deployment history, and optionally the actual rules.

Examples:
  # Show basic ruleset info
  effectusc storage show user-onboarding v1.2.0

  # Show with rules included
  effectusc storage show user-onboarding v1.2.0 --rules

  # Output as JSON
  effectusc storage show user-onboarding v1.2.0 --format json`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			name, version := args[0], args[1]

			// Load storage
			storage, err := loadStorageBackend()
			if err != nil {
				return fmt.Errorf("failed to initialize storage: %w", err)
			}

			// Get ruleset
			ruleset, err := storage.GetRuleset(ctx, name, version)
			if err != nil {
				return fmt.Errorf("failed to get ruleset: %w", err)
			}

			// Output result
			return outputRulesetDetails(ruleset, format, showRules)
		},
	}

	cmd.Flags().StringVar(&format, "format", "yaml", "Output format (table, json, yaml)")
	cmd.Flags().BoolVar(&showRules, "rules", false, "Include actual rule definitions")

	return cmd
}

// createStorageDeployCmd creates the storage deploy command
func createStorageDeployCmd() *cobra.Command {
	var (
		environment    string
		strategy       string
		dryRun         bool
		force          bool
		healthCheck    bool
		rolloutPercent int
		timeout        string
		gitRef         string
	)

	cmd := &cobra.Command{
		Use:   "deploy <name> <version>",
		Short: "Deploy a ruleset to an environment",
		Long: `Deploy a specific ruleset version to an environment with configurable
deployment strategies and options.

Examples:
  # Deploy to staging
  effectusc storage deploy user-onboarding v1.2.0 --environment staging

  # Deploy with canary strategy
  effectusc storage deploy user-onboarding v1.2.0 --environment production \\
    --strategy canary --rollout-percent 10

  # Dry run deployment
  effectusc storage deploy user-onboarding v1.2.0 --environment production --dry-run

  # Deploy from git
  effectusc storage deploy --git-ref main --environment staging`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			if gitRef != "" {
				return deployFromGit(ctx, gitRef, environment, &deploymentOptions{
					strategy:       strategy,
					dryRun:         dryRun,
					force:          force,
					healthCheck:    healthCheck,
					rolloutPercent: rolloutPercent,
					timeout:        timeout,
				})
			}

			if len(args) != 2 {
				return fmt.Errorf("name and version required when not using --git-ref")
			}

			name, version := args[0], args[1]

			// Load storage and rule manager
			storage, err := loadStorageBackend()
			if err != nil {
				return fmt.Errorf("failed to initialize storage: %w", err)
			}

			// Get ruleset
			ruleset, err := storage.GetRuleset(ctx, name, version)
			if err != nil {
				return fmt.Errorf("failed to get ruleset: %w", err)
			}

			// Deploy
			deploymentConfig := &runtime.DeploymentConfig{
				Strategy:        strategy,
				RollbackOnError: true,
			}

			err = storage.DeployRuleset(ctx, name, version, environment, deploymentConfig)
			if err != nil {
				return fmt.Errorf("deployment failed: %w", err)
			}

			fmt.Printf("✅ Successfully deployed %s v%s to %s\n", name, version, environment)
			return nil
		},
	}

	cmd.Flags().StringVar(&environment, "environment", "", "Target environment (required)")
	cmd.Flags().StringVar(&strategy, "strategy", "rolling", "Deployment strategy (rolling, blue_green, canary)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Perform a dry run without actual deployment")
	cmd.Flags().BoolVar(&force, "force", false, "Force deployment even with warnings")
	cmd.Flags().BoolVar(&healthCheck, "health-check", true, "Perform health checks after deployment")
	cmd.Flags().IntVar(&rolloutPercent, "rollout-percent", 100, "Percentage of traffic for canary deployments")
	cmd.Flags().StringVar(&timeout, "timeout", "5m", "Deployment timeout")
	cmd.Flags().StringVar(&gitRef, "git-ref", "", "Deploy from git reference (branch, tag, or commit)")

	cmd.MarkFlagRequired("environment")

	return cmd
}

// createStorageRollbackCmd creates the storage rollback command
func createStorageRollbackCmd() *cobra.Command {
	var (
		environment   string
		targetVersion string
		confirm       bool
	)

	cmd := &cobra.Command{
		Use:   "rollback <name>",
		Short: "Rollback a deployment to a previous version",
		Long: `Rollback a deployment to a previous version in the specified environment.

Examples:
  # Rollback to previous version
  effectusc storage rollback user-onboarding --environment production

  # Rollback to specific version
  effectusc storage rollback user-onboarding --environment production \\
    --target-version v1.1.0

  # Skip confirmation prompt
  effectusc storage rollback user-onboarding --environment production --confirm`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			name := args[0]

			// Load storage
			storage, err := loadStorageBackend()
			if err != nil {
				return fmt.Errorf("failed to initialize storage: %w", err)
			}

			// Get current deployment status
			status, err := storage.GetDeploymentStatus(ctx, name, environment)
			if err != nil {
				return fmt.Errorf("failed to get deployment status: %w", err)
			}

			fmt.Printf("Current deployment: %s\n", status)

			// Confirm rollback unless --confirm specified
			if !confirm {
				fmt.Printf("Are you sure you want to rollback %s in %s? (y/N): ", name, environment)
				var response string
				fmt.Scanln(&response)
				if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
					fmt.Println("Rollback cancelled")
					return nil
				}
			}

			// Perform rollback
			err = storage.RollbackDeployment(ctx, name, environment, targetVersion)
			if err != nil {
				return fmt.Errorf("rollback failed: %w", err)
			}

			fmt.Printf("✅ Successfully rolled back %s in %s\n", name, environment)
			return nil
		},
	}

	cmd.Flags().StringVar(&environment, "environment", "", "Target environment (required)")
	cmd.Flags().StringVar(&targetVersion, "target-version", "", "Specific version to rollback to")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Skip confirmation prompt")

	cmd.MarkFlagRequired("environment")

	return cmd
}

// createStorageAuditCmd creates the storage audit command
func createStorageAuditCmd() *cobra.Command {
	var (
		actions   []string
		resources []string
		userIDs   []string
		startTime string
		endTime   string
		result    string
		format    string
		limit     int
	)

	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Query audit logs",
		Long: `Query audit logs for rule storage operations with filtering options.

Examples:
  # Show recent audit logs
  effectusc storage audit --limit 20

  # Show deployment activities
  effectusc storage audit --actions deploy_ruleset,rollback_deployment

  # Show activities for specific ruleset
  effectusc storage audit --resources user-onboarding

  # Show failed operations
  effectusc storage audit --result failure

  # Show activities in date range
  effectusc storage audit --start-time "2024-01-01T00:00:00Z" \\
    --end-time "2024-01-31T23:59:59Z"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// Load storage
			storage, err := loadStorageBackend()
			if err != nil {
				return fmt.Errorf("failed to initialize storage: %w", err)
			}

			// Parse time filters
			filters := &runtime.AuditFilters{
				Actions:   actions,
				Resources: resources,
				UserIDs:   userIDs,
				Result:    result,
				Limit:     limit,
			}

			if startTime != "" {
				start, err := time.Parse(time.RFC3339, startTime)
				if err != nil {
					return fmt.Errorf("invalid start time format: %w", err)
				}
				filters.StartTime = start
			}

			if endTime != "" {
				end, err := time.Parse(time.RFC3339, endTime)
				if err != nil {
					return fmt.Errorf("invalid end time format: %w", err)
				}
				filters.EndTime = end
			}

			// Get audit logs
			auditEntries, err := storage.GetAuditLog(ctx, filters)
			if err != nil {
				return fmt.Errorf("failed to get audit logs: %w", err)
			}

			// Output results
			return outputAuditLogs(auditEntries, format)
		},
	}

	cmd.Flags().StringSliceVar(&actions, "actions", nil, "Filter by actions")
	cmd.Flags().StringSliceVar(&resources, "resources", nil, "Filter by resources")
	cmd.Flags().StringSliceVar(&userIDs, "user-ids", nil, "Filter by user IDs")
	cmd.Flags().StringVar(&startTime, "start-time", "", "Start time (RFC3339 format)")
	cmd.Flags().StringVar(&endTime, "end-time", "", "End time (RFC3339 format)")
	cmd.Flags().StringVar(&result, "result", "", "Filter by result (success, failure)")
	cmd.Flags().StringVar(&format, "format", "table", "Output format (table, json, yaml)")
	cmd.Flags().IntVar(&limit, "limit", 100, "Maximum number of results")

	return cmd
}

// createStorageHealthCmd creates the storage health command
func createStorageHealthCmd() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "health",
		Short: "Check storage backend health",
		Long: `Check the health status of configured storage backends.

Examples:
  # Check health
  effectusc storage health

  # Output as JSON
  effectusc storage health --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// Load storage
			storage, err := loadStorageBackend()
			if err != nil {
				return fmt.Errorf("failed to initialize storage: %w", err)
			}

			// Check health
			err = storage.HealthCheck(ctx)
			if err != nil {
				return fmt.Errorf("health check failed: %w", err)
			}

			fmt.Println("✅ Storage backend is healthy")
			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "text", "Output format (text, json, yaml)")

	return cmd
}

// createStorageCleanupCmd creates the storage cleanup command
func createStorageCleanupCmd() *cobra.Command {
	var (
		olderThan string
		dryRun    bool
		confirm   bool
	)

	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Clean up old rulesets and audit logs",
		Long: `Clean up old rulesets and audit logs based on retention policies.

Examples:
  # Clean up data older than 30 days
  effectusc storage cleanup --older-than 30d

  # Dry run to see what would be cleaned
  effectusc storage cleanup --older-than 30d --dry-run

  # Skip confirmation
  effectusc storage cleanup --older-than 30d --confirm`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// Parse duration
			duration, err := time.ParseDuration(olderThan)
			if err != nil {
				return fmt.Errorf("invalid duration format: %w", err)
			}

			cutoffTime := time.Now().Add(-duration)

			// Load storage
			storage, err := loadStorageBackend()
			if err != nil {
				return fmt.Errorf("failed to initialize storage: %w", err)
			}

			if dryRun {
				fmt.Printf("DRY RUN: Would clean up data older than %s (before %s)\n",
					olderThan, cutoffTime.Format(time.RFC3339))
				return nil
			}

			// Confirm cleanup unless --confirm specified
			if !confirm {
				fmt.Printf("Are you sure you want to clean up data older than %s? (y/N): ", olderThan)
				var response string
				fmt.Scanln(&response)
				if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
					fmt.Println("Cleanup cancelled")
					return nil
				}
			}

			// Perform cleanup
			err = storage.Cleanup(ctx, cutoffTime)
			if err != nil {
				return fmt.Errorf("cleanup failed: %w", err)
			}

			fmt.Printf("✅ Successfully cleaned up data older than %s\n", olderThan)
			return nil
		},
	}

	cmd.Flags().StringVar(&olderThan, "older-than", "90d", "Clean up data older than this duration (e.g., 30d, 12h)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be cleaned without actually doing it")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Skip confirmation prompt")

	return cmd
}

// createStorageConfigCmd creates the storage config command
func createStorageConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage storage configuration",
		Long: `Manage storage backend configuration.

Examples:
  # Show current configuration
  effectusc storage config show

  # Validate configuration
  effectusc storage config validate

  # Generate sample configuration
  effectusc storage config generate`,
	}

	// Add subcommands
	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show current storage configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := loadStorageConfig()
			if err != nil {
				return err
			}

			configYAML, err := yaml.Marshal(config)
			if err != nil {
				return err
			}

			fmt.Println(string(configYAML))
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "validate",
		Short: "Validate storage configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := loadStorageBackend()
			if err != nil {
				return fmt.Errorf("configuration validation failed: %w", err)
			}

			fmt.Println("✅ Configuration is valid")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "generate",
		Short: "Generate sample storage configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			sampleConfig := generateSampleStorageConfig()
			configYAML, err := yaml.Marshal(sampleConfig)
			if err != nil {
				return err
			}

			fmt.Println("# Sample Effectus storage configuration")
			fmt.Println(string(configYAML))
			return nil
		},
	})

	return cmd
}

// Helper types and functions

type deploymentOptions struct {
	strategy       string
	dryRun         bool
	force          bool
	healthCheck    bool
	rolloutPercent int
	timeout        string
}

// loadStorageBackend loads the configured storage backend
func loadStorageBackend() (runtime.RuleStorageBackend, error) {
	config, err := loadStorageConfig()
	if err != nil {
		return nil, err
	}

	switch config.Type {
	case "postgres":
		return runtime.NewPostgresRuleStorage(config.Postgres)
	case "redis":
		return runtime.NewRedisRuleStorage(config.Redis)
	case "etcd":
		return runtime.NewEtcdRuleStorage(config.Etcd)
	case "inmemory":
		return runtime.NewInMemoryRuleStorage(), nil
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", config.Type)
	}
}

// StorageConfig represents storage configuration
type StorageConfig struct {
	Type     string                         `yaml:"type"`
	Postgres *runtime.PostgresStorageConfig `yaml:"postgres,omitempty"`
	Redis    *runtime.RedisStorageConfig    `yaml:"redis,omitempty"`
	Etcd     *runtime.EtcdStorageConfig     `yaml:"etcd,omitempty"`
}

// loadStorageConfig loads storage configuration from file
func loadStorageConfig() (*StorageConfig, error) {
	// Try to load from .effectus/storage.yaml
	configPath := ".effectus/storage.yaml"
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Default to in-memory storage
		return &StorageConfig{Type: "inmemory"}, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config StorageConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &config, nil
}

// generateSampleStorageConfig generates a sample storage configuration
func generateSampleStorageConfig() *StorageConfig {
	return &StorageConfig{
		Type: "postgres",
		Postgres: &runtime.PostgresStorageConfig{
			DSN:                "postgres://user:password@localhost/effectus_rules",
			MaxConnections:     20,
			CacheEnabled:       true,
			CacheTTL:           10 * time.Minute,
			AuditRetentionDays: 365,
			TablePrefix:        "effectus_",
		},
	}
}

// Output formatting functions

func outputRulesets(rulesets []*runtime.RulesetMetadata, format string, showDetails bool) error {
	switch format {
	case "json":
		return json.NewEncoder(os.Stdout).Encode(rulesets)
	case "yaml":
		return yaml.NewEncoder(os.Stdout).Encode(rulesets)
	case "table":
		return outputRulesetsTable(rulesets, showDetails)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
}

func outputRulesetsTable(rulesets []*runtime.RulesetMetadata, showDetails bool) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()

	if showDetails {
		fmt.Fprintln(w, "NAME\tVERSION\tENVIRONMENT\tSTATUS\tRULES\tOWNER\tTEAM\tCREATED\tTAGS")
		for _, rs := range rulesets {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\t%s\n",
				rs.Name, rs.Version, rs.Environment, rs.Status, rs.RuleCount,
				rs.Owner, rs.Team, rs.CreatedAt.Format("2006-01-02"), strings.Join(rs.Tags, ","))
		}
	} else {
		fmt.Fprintln(w, "NAME\tVERSION\tENVIRONMENT\tSTATUS\tRULES\tCREATED")
		for _, rs := range rulesets {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\n",
				rs.Name, rs.Version, rs.Environment, rs.Status, rs.RuleCount,
				rs.CreatedAt.Format("2006-01-02"))
		}
	}

	return nil
}

func outputRulesetDetails(ruleset *runtime.StoredRuleset, format string, showRules bool) error {
	switch format {
	case "json":
		return json.NewEncoder(os.Stdout).Encode(ruleset)
	case "yaml":
		return yaml.NewEncoder(os.Stdout).Encode(ruleset)
	case "table":
		return outputRulesetDetailsTable(ruleset, showRules)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
}

func outputRulesetDetailsTable(ruleset *runtime.StoredRuleset, showRules bool) error {
	fmt.Printf("Ruleset: %s v%s\n", ruleset.Name, ruleset.Version)
	fmt.Printf("Environment: %s\n", ruleset.Environment)
	fmt.Printf("Status: %s\n", ruleset.Status)
	fmt.Printf("Owner: %s\n", ruleset.Owner)
	fmt.Printf("Team: %s\n", ruleset.Team)
	fmt.Printf("Created: %s\n", ruleset.CreatedAt.Format(time.RFC3339))
	fmt.Printf("Created By: %s\n", ruleset.CreatedBy)
	fmt.Printf("Git Commit: %s\n", ruleset.GitCommit)
	fmt.Printf("Schema Version: %s\n", ruleset.SchemaVersion)
	fmt.Printf("Tags: %s\n", strings.Join(ruleset.Tags, ", "))
	fmt.Printf("Description: %s\n", ruleset.Description)

	if ruleset.Ruleset != nil {
		fmt.Printf("Rules: %d\n", len(ruleset.Ruleset.Rules))
		fmt.Printf("Dependencies: %s\n", strings.Join(ruleset.Ruleset.Dependencies, ", "))
		fmt.Printf("Capabilities: %s\n", strings.Join(ruleset.Ruleset.Capabilities, ", "))

		if showRules {
			fmt.Println("\nRules:")
			for i, rule := range ruleset.Ruleset.Rules {
				fmt.Printf("  %d. %s (%s) - Priority: %d\n",
					i+1, rule.Name, rule.Type, rule.Priority)
				if rule.Description != "" {
					fmt.Printf("     %s\n", rule.Description)
				}
			}
		}
	}

	return nil
}

func outputAuditLogs(auditEntries []*runtime.AuditEntry, format string) error {
	switch format {
	case "json":
		return json.NewEncoder(os.Stdout).Encode(auditEntries)
	case "yaml":
		return yaml.NewEncoder(os.Stdout).Encode(auditEntries)
	case "table":
		return outputAuditLogsTable(auditEntries)
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
}

func outputAuditLogsTable(auditEntries []*runtime.AuditEntry) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer w.Flush()

	fmt.Fprintln(w, "TIMESTAMP\tACTION\tRESOURCE\tVERSION\tENVIRONMENT\tUSER\tRESULT")
	for _, entry := range auditEntries {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			entry.Timestamp.Format("2006-01-02 15:04:05"),
			entry.Action, entry.Resource, entry.Version, entry.Environment,
			entry.UserID, entry.Result)
	}

	return nil
}

func deployFromGit(ctx context.Context, gitRef, environment string, options *deploymentOptions) error {
	// Load rule manager
	storage, err := loadStorageBackend()
	if err != nil {
		return fmt.Errorf("failed to initialize storage: %w", err)
	}

	config := &runtime.RuleManagerConfig{
		GitRepository:  "https://github.com/company/effectus-rules.git", // Should be configurable
		GitBranch:      gitRef,
		RulesDirectory: "rules",
		Environments: []runtime.Environment{
			{Name: environment, Type: "staging"},
		},
	}

	ruleManager, err := runtime.NewRuleManager(storage, config)
	if err != nil {
		return fmt.Errorf("failed to create rule manager: %w", err)
	}

	// Convert timeout
	timeout, err := time.ParseDuration(options.timeout)
	if err != nil {
		return fmt.Errorf("invalid timeout: %w", err)
	}

	deployOptions := &runtime.DeploymentOptions{
		Strategy:       options.strategy,
		DryRun:         options.dryRun,
		Force:          options.force,
		RolloutPercent: options.rolloutPercent,
		HealthCheck:    options.healthCheck,
		Timeout:        timeout,
	}

	// Deploy from git
	result, err := ruleManager.DeployFromGit(ctx, environment, deployOptions)
	if err != nil {
		return fmt.Errorf("git deployment failed: %w", err)
	}

	fmt.Printf("✅ Successfully deployed %d rulesets from git (%s) to %s\n",
		result.Rulesets, result.CommitInfo.Hash[:8], environment)

	return nil
}
