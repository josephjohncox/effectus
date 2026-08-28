package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	kafkaadapter "github.com/effectus/effectus-go/adapters/kafka"
	"github.com/effectus/effectus-go/loader"
	effectusruntime "github.com/effectus/effectus-go/runtime"
	"github.com/effectus/effectus-go/schema"
	"github.com/effectus/effectus-go/schema/fencing"
	"github.com/effectus/effectus-go/unified"
	_ "github.com/lib/pq"
)

func daemonOCIVerificationPolicy() loader.OCIVerificationPolicy {
	return loader.OCIVerificationPolicy{RequireSignature: true, Verifier: loader.CommandOCISignatureVerifier{Path: strings.TrimSpace(*ociSignatureVerifier)}}
}

type kafkaFactHandler struct{ delegate kafkaadapter.Handler }

func (handler kafkaFactHandler) Handle(ctx context.Context, delivery kafkaadapter.Delivery) (kafkaadapter.HandleResult, error) {
	if handler.delegate == nil {
		return kafkaadapter.HandleResult{}, fmt.Errorf("Kafka engine handler is not configured")
	}
	return handler.delegate.Handle(ctx, delivery)
}

func configureDaemonExecutionEngine(ctx context.Context, bundle *unified.Bundle, extensionDirs, extensionOCIs []string) (*effectusruntime.ExecutionRuntime, *sql.DB, error) {
	if strings.TrimSpace(*postgresDSN) == "" {
		return nil, nil, fmt.Errorf("checked transport execution requires EFFECTUS_POSTGRES_DSN or protected database.dsn configuration")
	}
	execution := effectusruntime.NewExecutionRuntime()
	metadata := effectusruntime.GenerationMetadata{Ruleset: "default", Version: "active"}
	if bundle != nil {
		metadata.Ruleset = bundle.Name
		metadata.Version = bundle.Version
		digest, err := unified.BundleDigest(bundle)
		if err != nil {
			return nil, nil, fmt.Errorf("compute bundle generation metadata: %w", err)
		}
		metadata.BundleDigest = digest
	}
	if err := execution.ConfigureGenerationMetadata(metadata); err != nil {
		return nil, nil, err
	}
	for _, directory := range extensionDirs {
		loaders, err := loader.LoadFromDirectory(directory)
		if err != nil {
			return nil, nil, err
		}
		for _, extensionLoader := range loaders {
			execution.RegisterExtensionLoader(extensionLoader)
		}
	}
	for index, ref := range extensionOCIs {
		if strings.TrimSpace(*ociSignatureVerifier) == "" {
			return nil, nil, fmt.Errorf("OCI extensions require --oci-signature-verifier")
		}
		execution.RegisterExtensionLoader(loader.NewOCIBundleLoaderWithPolicy(fmt.Sprintf("kafka-oci-%d", index+1), ref, daemonOCIVerificationPolicy()))
	}
	if bundle != nil {
		for index, source := range bundle.RuleSources {
			path := source.Path
			if path == "" {
				path = fmt.Sprintf("bundle-rule-%d.effx", index+1)
			}
			lower := strings.ToLower(path)
			if !strings.HasSuffix(lower, ".eff") && !strings.HasSuffix(lower, ".effx") {
				format := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(source.Format)), ".")
				if format != "eff" && format != "effx" {
					continue
				}
				path += "." + format
			}
			execution.RegisterExtensionLoader(loader.NewStaticSourceLoader(fmt.Sprintf("bundle-rule-%d", index+1), path, []byte(source.Content)))
		}
	}
	// Extension directories can contain the canonical source even when the
	// release bundle has no embedded RuleSources.
	if err := execution.CompileAndValidate(ctx); err != nil {
		return nil, nil, err
	}
	if execution.GetRuntimeInfo().PlanCount == 0 {
		return nil, nil, fmt.Errorf("checked transport execution requires at least one canonical .eff or .effx plan")
	}
	db, err := openDaemonDatabase()
	if err != nil {
		return nil, nil, err
	}
	closeOnError := func(err error) (*effectusruntime.ExecutionRuntime, *sql.DB, error) {
		_ = db.Close()
		return nil, nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		return closeOnError(fmt.Errorf("connect Kafka execution ledger: %w", err))
	}
	switch strings.ToLower(strings.TrimSpace(*databaseMigrations)) {
	case "validate":
		if err := schema.ValidateSagaV2(ctx, db); err != nil {
			return closeOnError(fmt.Errorf("validate durable database migrations: %w", err))
		}
	case "legacy-apply":
		fmt.Fprintln(os.Stderr, "Warning: --database-migrations=legacy-apply grants DDL to the application; use the migration Job and validate mode")
		if err := schema.MigrateSagaV2(ctx, db); err != nil {
			return closeOnError(fmt.Errorf("apply durable database migrations: %w", err))
		}
	default:
		return closeOnError(fmt.Errorf("database migration mode %q cannot start the daemon", *databaseMigrations))
	}
	setMetricsDatabase(db)
	store, err := schema.NewPostgresOutboxStore(db)
	if err != nil {
		return closeOnError(err)
	}
	fencingProvider, err := fencing.NewPostgresProvider(db)
	if err != nil {
		return closeOnError(fmt.Errorf("configure durable fencing: %w", err))
	}
	if err := execution.ConfigureDurableWorkflowExecution(store, fencingProvider, schema.DispatcherOptions{Owner: "effectusd", RequireDurableFencing: true}); err != nil {
		return closeOnError(err)
	}
	if err := execution.ConfigureExecutionLedger(store, effectusruntime.NewManifestArtifactResolver()); err != nil {
		return closeOnError(err)
	}
	return execution, db, nil
}

func validateDatabasePoolConfig() error {
	if *dbMaxOpen <= 0 || *dbMaxIdle < 0 || *dbMaxIdle > *dbMaxOpen || *dbConnLifetime < 0 || *dbConnIdleTime < 0 {
		return fmt.Errorf("max-open must be positive, max-idle must be between zero and max-open, and durations must not be negative")
	}
	return nil
}

func runDatabaseMaintenance(ctx context.Context) error {
	if strings.TrimSpace(*postgresDSN) == "" {
		return fmt.Errorf("EFFECTUS_POSTGRES_DSN or database.dsn is required")
	}
	db, err := openDaemonDatabase()
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return err
	}
	if *migrateOnly {
		return schema.MigrateSagaV2(ctx, db)
	}
	if err := schema.ValidateSagaV2(ctx, db); err != nil {
		return err
	}
	result, err := schema.PruneTerminalRecords(ctx, db, schema.PruneOptions{Retention: *maintenanceRetention, BatchSize: *maintenanceBatch, DryRun: *maintenanceDryRun})
	if err != nil {
		return err
	}
	operation := "deleted"
	if *maintenanceDryRun {
		operation = "eligible"
	}
	fmt.Printf("maintenance dry_run=%t executions=%d sagas=%d kafka_deliveries=%d\n", *maintenanceDryRun, result.Executions, result.Sagas, result.KafkaDeliveries)
	fmt.Printf("effectusd_maintenance_records_total{kind=\"execution\",operation=\"%s\"} %d\n", operation, result.Executions)
	fmt.Printf("effectusd_maintenance_records_total{kind=\"saga\",operation=\"%s\"} %d\n", operation, result.Sagas)
	fmt.Printf("effectusd_maintenance_records_total{kind=\"kafka_delivery\",operation=\"%s\"} %d\n", operation, result.KafkaDeliveries)
	fmt.Println("effectusd_maintenance_error_total 0")
	return nil
}

func decodeKafkaFactEnvelope(data []byte) (factEnvelope, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var input struct {
		Universe  string                 `json:"universe"`
		Facts     map[string]interface{} `json:"facts"`
		Received  time.Time              `json:"received_at"`
		Namespace string                 `json:"namespace"`
	}
	if err := decoder.Decode(&input); err != nil {
		return factEnvelope{}, fmt.Errorf("decode Kafka fact envelope: %w", err)
	}
	var extra any
	if err := requireConfigEOF(decoder.Decode(&extra)); err != nil {
		return factEnvelope{}, fmt.Errorf("decode Kafka fact envelope: %w", err)
	}
	if len(input.Facts) == 0 {
		return factEnvelope{}, fmt.Errorf("decode Kafka fact envelope: facts are required")
	}
	return factEnvelope{
		Universe: input.Universe, Facts: input.Facts, Received: input.Received, Namespace: input.Namespace,
	}, nil
}

func daemonKafkaConfig() *kafkaadapter.Config {
	return &kafkaadapter.Config{
		SourceID: "effectusd", ClusterNamespace: strings.TrimSpace(*kafkaClusterNamespace),
		Brokers: splitCommaList(*kafkaBrokers), Topic: strings.TrimSpace(*kafkaTopic),
		ConsumerGroup: strings.TrimSpace(*kafkaConsumerGroup),
		AckContract:   kafkaadapter.AckContract(strings.TrimSpace(*kafkaAckContract)),
		MaxAttempts:   *kafkaMaxAttempts, InitialBackoff: *kafkaRetryInitial, MaxBackoff: *kafkaRetryMax,
		PoisonPolicy:    kafkaadapter.PoisonPolicy(strings.TrimSpace(*kafkaPoisonPolicy)),
		DLQTopic:        strings.TrimSpace(*kafkaDLQTopic),
		DLQDeliveryMode: kafkaadapter.DLQDeliveryMode(strings.TrimSpace(*kafkaDLQMode)),
	}
}

type postgresKafkaDeliveryLedger struct{ db *sql.DB }

func (ledger *postgresKafkaDeliveryLedger) Attempts(ctx context.Context, deliveryID string) (int, error) {
	var failures int
	err := ledger.db.QueryRowContext(ctx, `SELECT failures FROM effectus_kafka_deliveries WHERE delivery_id = $1`, deliveryID).Scan(&failures)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return failures, err
}

func (ledger *postgresKafkaDeliveryLedger) RecordFailure(ctx context.Context, deliveryID string) (int, error) {
	var failures int
	err := ledger.db.QueryRowContext(ctx, `
		INSERT INTO effectus_kafka_deliveries (delivery_id, failures)
		VALUES ($1, 1)
		ON CONFLICT (delivery_id) DO UPDATE
		SET failures = effectus_kafka_deliveries.failures + 1, updated_at = now()
		RETURNING failures
	`, deliveryID).Scan(&failures)
	return failures, err
}

func (ledger *postgresKafkaDeliveryLedger) ClearAttempts(ctx context.Context, deliveryID string) error {
	_, err := ledger.db.ExecContext(ctx, `DELETE FROM effectus_kafka_deliveries WHERE delivery_id = $1`, deliveryID)
	return err
}

func (ledger *postgresKafkaDeliveryLedger) AcknowledgePoison(ctx context.Context, disposition kafkaadapter.PoisonDisposition) error {
	_, err := ledger.db.ExecContext(ctx, `
		INSERT INTO effectus_kafka_deliveries
		    (delivery_id, failures, poison_acknowledged, poison_policy, poison_error, topic, partition_id, offset_id)
		VALUES ($1, $2, true, $3, $4, $5, $6, $7)
		ON CONFLICT (delivery_id) DO UPDATE
		SET poison_acknowledged = true, poison_policy = EXCLUDED.poison_policy,
		    poison_error = EXCLUDED.poison_error, topic = EXCLUDED.topic,
		    partition_id = EXCLUDED.partition_id, offset_id = EXCLUDED.offset_id, updated_at = now()
	`, disposition.DeliveryID, disposition.Attempts, disposition.Policy, disposition.Error,
		disposition.Message.Topic, disposition.Message.Partition, disposition.Message.Offset)
	return err
}

func (ledger *postgresKafkaDeliveryLedger) PoisonAcknowledged(ctx context.Context, deliveryID string) (bool, error) {
	var acknowledged bool
	err := ledger.db.QueryRowContext(ctx, `SELECT poison_acknowledged FROM effectus_kafka_deliveries WHERE delivery_id = $1`, deliveryID).Scan(&acknowledged)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return acknowledged, err
}

type filePoisonAcknowledger struct {
	path         string
	mu           sync.Mutex
	loaded       bool
	attempts     map[string]int
	acknowledged map[string]bool
}

type kafkaLedgerEntry struct {
	Kind       string                    `json:"kind"`
	DeliveryID string                    `json:"delivery_id"`
	Attempt    int                       `json:"attempt,omitempty"`
	Policy     kafkaadapter.PoisonPolicy `json:"policy,omitempty"`
	Attempts   int                       `json:"attempts,omitempty"`
	Error      string                    `json:"error,omitempty"`
	Topic      string                    `json:"topic,omitempty"`
	Partition  int                       `json:"partition,omitempty"`
	Offset     int64                     `json:"offset,omitempty"`
	RecordedAt time.Time                 `json:"recorded_at"`
}

func (acknowledger *filePoisonAcknowledger) AcknowledgePoison(ctx context.Context, disposition kafkaadapter.PoisonDisposition) error {
	acknowledger.mu.Lock()
	defer acknowledger.mu.Unlock()
	if err := acknowledger.loadLocked(); err != nil {
		return err
	}
	if acknowledger.acknowledged[disposition.DeliveryID] {
		return nil
	}
	entry := kafkaLedgerEntry{
		Kind: "poison", DeliveryID: disposition.DeliveryID, Policy: disposition.Policy,
		Attempts: disposition.Attempts, Error: disposition.Error,
		Topic: disposition.Message.Topic, Partition: disposition.Message.Partition,
		Offset: disposition.Message.Offset, RecordedAt: time.Now().UTC(),
	}
	if err := acknowledger.appendLocked(ctx, entry); err != nil {
		return err
	}
	acknowledger.acknowledged[disposition.DeliveryID] = true
	return nil
}

func (acknowledger *filePoisonAcknowledger) PoisonAcknowledged(ctx context.Context, deliveryID string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	acknowledger.mu.Lock()
	defer acknowledger.mu.Unlock()
	if err := acknowledger.loadLocked(); err != nil {
		return false, err
	}
	return acknowledger.acknowledged[deliveryID], nil
}

func (acknowledger *filePoisonAcknowledger) Attempts(ctx context.Context, deliveryID string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	acknowledger.mu.Lock()
	defer acknowledger.mu.Unlock()
	if err := acknowledger.loadLocked(); err != nil {
		return 0, err
	}
	return acknowledger.attempts[deliveryID], nil
}

func (acknowledger *filePoisonAcknowledger) RecordFailure(ctx context.Context, deliveryID string) (int, error) {
	acknowledger.mu.Lock()
	defer acknowledger.mu.Unlock()
	if err := acknowledger.loadLocked(); err != nil {
		return 0, err
	}
	next := acknowledger.attempts[deliveryID] + 1
	if err := acknowledger.appendLocked(ctx, kafkaLedgerEntry{
		Kind: "failure", DeliveryID: deliveryID, Attempt: next, RecordedAt: time.Now().UTC(),
	}); err != nil {
		return 0, err
	}
	acknowledger.attempts[deliveryID] = next
	return next, nil
}

func (acknowledger *filePoisonAcknowledger) ClearAttempts(ctx context.Context, deliveryID string) error {
	acknowledger.mu.Lock()
	defer acknowledger.mu.Unlock()
	if err := acknowledger.loadLocked(); err != nil {
		return err
	}
	if err := acknowledger.appendLocked(ctx, kafkaLedgerEntry{
		Kind: "clear", DeliveryID: deliveryID, RecordedAt: time.Now().UTC(),
	}); err != nil {
		return err
	}
	delete(acknowledger.attempts, deliveryID)
	delete(acknowledger.acknowledged, deliveryID)
	return nil
}

func (acknowledger *filePoisonAcknowledger) loadLocked() error {
	if acknowledger.loaded {
		return nil
	}
	acknowledger.attempts = make(map[string]int)
	acknowledger.acknowledged = make(map[string]bool)
	file, err := os.Open(acknowledger.path)
	if errors.Is(err, os.ErrNotExist) {
		acknowledger.loaded = true
		return nil
	}
	if err != nil {
		return fmt.Errorf("open Kafka delivery ledger: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	for scanner.Scan() {
		var entry kafkaLedgerEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			return fmt.Errorf("decode Kafka delivery ledger: %w", err)
		}
		switch entry.Kind {
		case "failure", "attempt":
			if entry.Attempt > acknowledger.attempts[entry.DeliveryID] {
				acknowledger.attempts[entry.DeliveryID] = entry.Attempt
			}
		case "clear":
			delete(acknowledger.attempts, entry.DeliveryID)
			delete(acknowledger.acknowledged, entry.DeliveryID)
		case "poison", "":
			acknowledger.acknowledged[entry.DeliveryID] = true
		default:
			return fmt.Errorf("unknown Kafka delivery ledger entry kind %q", entry.Kind)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read Kafka delivery ledger: %w", err)
	}
	acknowledger.loaded = true
	return nil
}

func (acknowledger *filePoisonAcknowledger) appendLocked(ctx context.Context, entry kafkaLedgerEntry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.OpenFile(acknowledger.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open Kafka delivery ledger: %w", err)
	}
	payload, err := json.Marshal(entry)
	if err == nil {
		payload = append(payload, '\n')
		_, err = file.Write(payload)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return fmt.Errorf("persist Kafka delivery ledger: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("close Kafka delivery ledger: %w", closeErr)
	}
	directory, err := os.Open(filepath.Dir(acknowledger.path))
	if err != nil {
		return fmt.Errorf("open Kafka delivery ledger directory: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync Kafka delivery ledger directory: %w", err)
	}
	return nil
}

func newDaemonRecoveryWorker(execution *effectusruntime.ExecutionRuntime, db *sql.DB) (*effectusruntime.RecoveryWorker, error) {
	if execution == nil || db == nil {
		return nil, fmt.Errorf("Kafka recovery runtime and database are required")
	}
	store, err := schema.NewPostgresOutboxStore(db)
	if err != nil {
		return nil, err
	}
	return &effectusruntime.RecoveryWorker{Engine: execution.Engine(), Store: store, Owner: "effectusd-kafka-recovery", BatchSize: 32, LeaseDuration: 30 * time.Second, PollInterval: 250 * time.Millisecond}, nil
}

func newDaemonKafkaHandler(bundle *unified.Bundle, execution *effectusruntime.ExecutionRuntime) (kafkaadapter.Handler, error) {
	if bundle == nil || execution == nil {
		return nil, fmt.Errorf("Kafka checked runtime and bundle are required")
	}
	wait := effectusruntime.WaitTerminal
	if kafkaadapter.AckContract(strings.TrimSpace(*kafkaAckContract)) == kafkaadapter.AckAfterDurableAcceptance {
		wait = effectusruntime.WaitAccepted
	}
	return kafkaadapter.NewEngineHandler(kafkaadapter.EngineHandlerConfig{
		Ruleset: bundle.Name, Version: bundle.Version, DefaultTenant: "default", WaitMode: wait,
	}, execution.Engine())
}

func configureDaemonKafkaSource(db *sql.DB) (*kafkaadapter.KafkaSource, error) {
	if db == nil {
		return nil, fmt.Errorf("Kafka durable delivery ledger database is required")
	}
	source, err := kafkaadapter.NewKafkaSource(daemonKafkaConfig())
	if err != nil {
		return nil, err
	}
	ledger := &postgresKafkaDeliveryLedger{db: db}
	if err := source.SetAttemptTracker(ledger); err != nil {
		return nil, err
	}
	if !strings.EqualFold(*kafkaPoisonPolicy, string(kafkaadapter.PoisonHalt)) {
		if err := source.SetPoisonAcknowledger(ledger); err != nil {
			return nil, err
		}
	}
	return source, nil
}
