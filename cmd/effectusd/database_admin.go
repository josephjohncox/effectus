package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/effectus/effectus-go/schema"
)

type databaseSettings struct {
	MaxOpen     int
	MaxIdle     int
	MaxLifetime time.Duration
	MaxIdleTime time.Duration
}

func databaseSettingsFromFlags() databaseSettings {
	return databaseSettings{
		MaxOpen: *databaseMaxOpen, MaxIdle: *databaseMaxIdle,
		MaxLifetime: *databaseMaxLifetime, MaxIdleTime: *databaseMaxIdleTime,
	}
}

func validateDatabaseSettings(settings databaseSettings) error {
	if settings.MaxOpen < 1 {
		return fmt.Errorf("max-open must be at least 1")
	}
	if settings.MaxIdle < 0 || settings.MaxIdle > settings.MaxOpen {
		return fmt.Errorf("max-idle must be between 0 and max-open")
	}
	if settings.MaxLifetime < 0 || settings.MaxIdleTime < 0 {
		return fmt.Errorf("connection lifetime and idle time cannot be negative")
	}
	return nil
}

func openDaemonDatabase() (*sql.DB, error) {
	if strings.TrimSpace(*sagaPgDSN) == "" {
		return nil, fmt.Errorf("EFFECTUS_SAGA_POSTGRES_DSN or protected saga.postgres.dsn is required")
	}
	settings := databaseSettingsFromFlags()
	if err := validateDatabaseSettings(settings); err != nil {
		return nil, err
	}
	db, err := sql.Open("postgres", *sagaPgDSN)
	if err != nil {
		return nil, err
	}
	// Apply limits before the first network operation so startup and workers use
	// the same per-pod connection budget.
	db.SetMaxOpenConns(settings.MaxOpen)
	db.SetMaxIdleConns(settings.MaxIdle)
	db.SetConnMaxLifetime(settings.MaxLifetime)
	db.SetConnMaxIdleTime(settings.MaxIdleTime)
	return db, nil
}

func rejectCheckedRuntimeMutation(hotload bool, bundleReload, extensionReload time.Duration) error {
	if hotload {
		return fmt.Errorf("rule activation is disabled because the checked engine is immutable; use /api/rules/validate and redeploy")
	}
	if bundleReload > 0 {
		return fmt.Errorf("--reload-interval is disabled because checked engine references cannot be swapped atomically; redeploy")
	}
	if extensionReload > 0 {
		return fmt.Errorf("--extensions-reload-interval is disabled because checked engine references cannot be swapped atomically; redeploy")
	}
	return nil
}

func runDatabaseAdminCommand(ctx context.Context) (bool, error) {
	mode := strings.ToLower(strings.TrimSpace(*databaseMigrations))
	if mode != "validate" && mode != "apply" && mode != "legacy-apply" {
		return true, fmt.Errorf("database-migrations must be validate, apply, or legacy-apply")
	}
	pruneRequested := strings.TrimSpace(*adminPruneBefore) != ""
	if mode != "apply" && !pruneRequested {
		return false, nil
	}
	if mode == "apply" && pruneRequested {
		return true, fmt.Errorf("migration apply and pruning must be separate operations")
	}
	db, err := openDaemonDatabase()
	if err != nil {
		return true, err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return true, fmt.Errorf("connect durable database: %w", err)
	}
	if mode == "apply" {
		if err := schema.MigrateSagaV2(ctx, db); err != nil {
			return true, err
		}
		fmt.Println("Durable database migrations applied")
		return true, nil
	}
	if err := schema.ValidateSagaV2(ctx, db); err != nil {
		return true, err
	}
	cutoff, err := time.Parse(time.RFC3339, strings.TrimSpace(*adminPruneBefore))
	if err != nil {
		return true, fmt.Errorf("admin-prune-before must be RFC3339: %w", err)
	}
	if !*adminPruneDryRun && !*adminPruneBackupVerified {
		return true, fmt.Errorf("destructive pruning requires --admin-prune-backup-verified after a restore-verified backup")
	}
	report, err := schema.PruneTerminalRecords(ctx, db, schema.PruneOptions{
		Before: cutoff, BatchSize: *adminPruneBatchSize, DryRun: *adminPruneDryRun,
	})
	if err != nil {
		return true, err
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		return true, err
	}
	fmt.Printf("effectusd_prune dry_run=%t cutoff=%s batch_size=%d rows=%s\n", *adminPruneDryRun, cutoff.Format(time.RFC3339), *adminPruneBatchSize, encoded)
	pruneMetrics := []struct {
		table string
		rows  int64
	}{
		{"executions", report.Executions}, {"execution_plans", report.ExecutionPlans},
		{"fact_applications", report.FactApplications}, {"fact_snapshots", report.FactSnapshots},
		{"saga_instances", report.SagaInstances}, {"saga_steps", report.SagaSteps},
		{"saga_outbox", report.SagaOutbox}, {"saga_attempts", report.SagaAttempts},
		{"rule_generations", report.RuleGenerations}, {"artifacts", report.Artifacts},
		{"kafka_deliveries", report.KafkaDeliveries},
	}
	for _, metric := range pruneMetrics {
		fmt.Printf("effectusd_prune_rows{table=%q,dry_run=%q} %d\n", metric.table, fmt.Sprint(*adminPruneDryRun), metric.rows)
	}
	return true, nil
}
