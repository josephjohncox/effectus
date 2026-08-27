// cmd/effectusd/main.go
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/effectus/effectus-go/adapters"
	kafkaadapter "github.com/effectus/effectus-go/adapters/kafka"
	"github.com/effectus/effectus-go/internal/schemasources"
	"github.com/effectus/effectus-go/loader"
	"github.com/effectus/effectus-go/pathutil"
	effectusruntime "github.com/effectus/effectus-go/runtime"
	"github.com/effectus/effectus-go/schema"
	"github.com/effectus/effectus-go/schema/capability"
	"github.com/effectus/effectus-go/schema/types"
	"github.com/effectus/effectus-go/schema/verb"
	"github.com/effectus/effectus-go/unified"
)

type namespaceStrategyFlag struct {
	values map[string]pathutil.MergeStrategy
}

func (n *namespaceStrategyFlag) String() string {
	if n == nil || len(n.values) == 0 {
		return ""
	}
	parts := make([]string, 0, len(n.values))
	for namespace, strategy := range n.values {
		parts = append(parts, namespace+"="+string(strategy))
	}
	return strings.Join(parts, ",")
}

func (n *namespaceStrategyFlag) Set(value string) error {
	parts := strings.SplitN(value, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("expected namespace=strategy, got %q", value)
	}
	strategy, err := parseMergeStrategy(parts[1])
	if err != nil {
		return err
	}
	if n.values == nil {
		n.values = make(map[string]pathutil.MergeStrategy)
	}
	n.values[strings.TrimSpace(parts[0])] = strategy
	return nil
}

var (
	// Configuration flags
	configPath               = flag.String("config", "", "Path to YAML/JSON config file")
	bundleFile               = flag.String("bundle", "", "Path to bundle file")
	ociRef                   = flag.String("oci-ref", "", "OCI reference for bundle (e.g., ghcr.io/user/bundle:v1)")
	ociCacheDir              = flag.String("oci-cache-dir", "./bundles", "Writable directory for OCI bundle cache")
	ociSignatureVerifier     = flag.String("oci-signature-verifier", "", "Fixed executable used to verify an OCI reference and digest")
	pluginDir                = flag.String("plugin-dir", "", "Directory containing verb plugins")
	verbDir                  = flag.String("verb-dir", "", "Directory containing JSON verb specs")
	verbDuplicatePolicy      = flag.String("verb-duplicate-policy", "error", "Duplicate verb policy (error, replace, ignore)")
	verbOCIWarmup            = flag.Bool("verb-oci-warmup", false, "Warm OCI verb executors at startup")
	verbStrict               = flag.Bool("verb-strict", true, "Validate verb arguments and return values")
	extensionsDir            = flag.String("extensions-dir", "", "Directory containing extension manifests (*.verbs.json, *.schema.json)")
	extensionsOCI            = flag.String("extensions-oci", "", "OCI references for extension bundles (comma-separated)")
	extensionsReloadInterval = flag.Duration("extensions-reload-interval", 0, "Interval for reloading extension manifests (0 to disable)")
	schemaSourcesFile        = flag.String("schema-sources", "", "Path to schema sources config (YAML/JSON)")
	reloadInterval           = flag.Duration("reload-interval", 30*time.Second, "Interval for hot-reloading")
	shutdownTimeout          = flag.Duration("shutdown-timeout", 30*time.Second, "Deadline for graceful shutdown and queue drain")

	// Runtime flags
	sagaEnabled     = flag.Bool("saga", false, "Deprecated legacy saga mode (rejected by effectusd; use checked durable workflow runtime)")
	sagaStoreType   = flag.String("saga-store", "memory", "Saga store (memory, redis, postgres)")
	sagaRedisAddr   = flag.String("saga-redis-addr", "localhost:6379", "Redis address for saga store")
	sagaRedisPass   = flag.String("saga-redis-password", "", "Redis password for saga store")
	sagaRedisDB     = flag.Int("saga-redis-db", 0, "Redis DB for saga store")
	sagaRedisPrefix = flag.String("saga-redis-prefix", "", "Redis key prefix for saga store")
	sagaRedisTTL    = flag.Duration("saga-redis-ttl", 0, "TTL for saga keys (0 to disable)")
	sagaPgDSN       = flag.String("saga-postgres-dsn", "", "Postgres DSN for saga store")

	// Determinism
	fixedTime = flag.String("fixed-time", "", "Fixed time for deterministic evaluation (RFC3339 or RFC3339Nano)")

	// Monitoring flags
	metricsAddr = flag.String("metrics-addr", ":9090", "Address to expose metrics")

	// Fact source flags
	factSource            = flag.String("fact-source", "http", "Fact source (http, kafka)")
	kafkaBrokers          = flag.String("kafka-brokers", "localhost:9092", "Kafka brokers (comma-separated)")
	kafkaTopic            = flag.String("kafka-topic", "facts", "Kafka topic")
	kafkaConsumerGroup    = flag.String("kafka-consumer-group", "effectusd", "Kafka consumer group")
	kafkaClusterNamespace = flag.String("kafka-cluster-namespace", "default", "Stable Kafka cluster namespace used in delivery IDs")
	kafkaAckContract      = flag.String("kafka-ack-contract", "completed_processing", "Kafka acknowledgement contract (completed_processing, durable_acceptance)")
	kafkaMaxAttempts      = flag.Int("kafka-max-attempts", 3, "Kafka handler attempts before poison policy")
	kafkaRetryInitial     = flag.Duration("kafka-retry-initial", time.Second, "Kafka initial same-record retry delay")
	kafkaRetryMax         = flag.Duration("kafka-retry-max", 30*time.Second, "Kafka maximum same-record retry delay")
	kafkaPoisonPolicy     = flag.String("kafka-poison-policy", "halt", "Kafka poison policy (halt, skip, dlq)")
	kafkaDLQTopic         = flag.String("kafka-dlq-topic", "", "Kafka DLQ topic for the dlq poison policy")
	kafkaDLQMode          = flag.String("kafka-dlq-mode", string(kafkaadapter.DLQAtLeastOnceNonTransactional), "Kafka DLQ mode (at_least_once_non_transactional; DLQ publish and source offset are not atomic)")
	kafkaPoisonAudit      = flag.String("kafka-poison-audit", "", "Deprecated; Kafka poison state is stored in the PostgreSQL execution ledger")
	kafkaDeliveryLedger   = flag.String("kafka-delivery-ledger", "", "Deprecated; Kafka attempt state is stored in the PostgreSQL execution ledger")

	// HTTP and gRPC server flags
	httpAddr          = flag.String("http-addr", ":8080", "HTTP server address")
	grpcAddr          = flag.String("grpc-addr", "", "Generated gRPC execution service address (empty disables gRPC)")
	grpcTLSCert       = flag.String("grpc-tls-cert", "", "PEM certificate for the gRPC execution service")
	grpcTLSKey        = flag.String("grpc-tls-key", "", "PEM private key for the gRPC execution service")
	grpcAllowInsecure = flag.Bool("grpc-allow-insecure", false, "Explicitly allow plaintext gRPC transport")
	grpcMaxReceive    = flag.Int("grpc-max-receive-bytes", 4<<20, "Maximum gRPC request size")
	grpcMaxSend       = flag.Int("grpc-max-send-bytes", 4<<20, "Maximum gRPC response size")
	grpcMaxDuration   = flag.Duration("grpc-max-execution-duration", 30*time.Second, "Maximum gRPC execution duration")
	grpcMaxConcurrent = flag.Int("grpc-max-concurrent", 128, "Maximum concurrent gRPC executions")

	// API auth + rate limit flags
	apiAuthMode   = flag.String("api-auth", "token", "API auth mode (token, disabled)")
	apiToken      = flag.String("api-token", "", "Write token for /api endpoints (comma-separated)")
	apiReadToken  = flag.String("api-read-token", "", "Read-only token for /api endpoints (comma-separated)")
	apiACLFile    = flag.String("api-acl-file", "", "Path to API ACL file (YAML/JSON)")
	apiRateLimit  = flag.Int("api-rate-limit", 120, "API requests per minute per client (0 to disable)")
	apiRateBurst  = flag.Int("api-rate-burst", 60, "API burst size (0 to use rate limit)")
	rulesHotload  = flag.Bool("rules-hotload", false, "Enable /api/rules/validate and /api/rules/hotload")
	rulesHistory  = flag.Int("rules-history", 5, "Number of hotload bundles to keep in memory/on disk")
	rulesHistDir  = flag.String("rules-history-dir", "./out/rules_history", "Directory for bundle history snapshots")
	factsStore    = flag.String("facts-store", "file", "Facts store (file, memory)")
	factsPath     = flag.String("facts-path", "./data/facts.json", "Facts store path (file store)")
	factsMergeDef = flag.String("facts-merge-default", "last", "Default merge strategy (first, last, error)")
	factsCache    = flag.String("facts-cache-policy", "none", "Facts cache policy (none, lru)")
	factsCacheMax = flag.Int("facts-cache-max-universes", 0, "Max universes to keep in cache (0 for unlimited)")
	factsCacheNs  = flag.Int("facts-cache-max-namespaces", 0, "Max namespaces per universe to keep (0 for unlimited)")

	// Debug flags
	verbose = flag.Bool("verbose", false, "Enable verbose logging")
)

var factsMergeNs namespaceStrategyFlag
var schemaSources []adapters.SchemaSourceConfig

func main() {
	flag.Var(&factsMergeNs, "facts-merge-namespace", "Namespace-specific merge strategy (namespace=first|last|error)")
	flag.Parse()

	setFlags := map[string]bool{}
	flag.CommandLine.Visit(func(f *flag.Flag) {
		setFlags[f.Name] = true
	})

	for _, secretFlag := range []string{"api-token", "api-read-token", "saga-redis-password", "saga-postgres-dsn"} {
		if setFlags[secretFlag] {
			fmt.Fprintf(os.Stderr, "--%s is rejected because command arguments expose secrets; use environment variables or a protected config file\n", secretFlag)
			os.Exit(1)
		}
	}

	if strings.TrimSpace(*configPath) != "" {
		cfg, err := loadRuntimeConfig(*configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}
		if err := applyRuntimeConfig(cfg, setFlags); err != nil {
			fmt.Fprintf(os.Stderr, "Error applying config: %v\n", err)
			os.Exit(1)
		}
		if len(schemaSources) > 0 {
			baseDir := filepath.Dir(*configPath)
			for i := range schemaSources {
				if schemaSources[i].BaseDir == "" {
					schemaSources[i].BaseDir = baseDir
				}
			}
		}
	}

	// Secret environment variables take effect only when the corresponding
	// flag/config value is empty. This keeps chart-managed secrets out of the
	// process argument list.
	if *apiToken == "" {
		*apiToken = os.Getenv("EFFECTUS_API_TOKEN")
	}
	if *apiReadToken == "" {
		*apiReadToken = os.Getenv("EFFECTUS_API_READ_TOKEN")
	}
	if *sagaRedisPass == "" {
		*sagaRedisPass = os.Getenv("EFFECTUS_SAGA_REDIS_PASSWORD")
	}
	if *sagaPgDSN == "" {
		*sagaPgDSN = os.Getenv("EFFECTUS_SAGA_POSTGRES_DSN")
	}

	if strings.TrimSpace(*schemaSourcesFile) != "" {
		sources, err := schemasources.LoadFromFile(*schemaSourcesFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading schema sources: %v\n", err)
			os.Exit(1)
		}
		schemaSources = sources
	}

	if strings.TrimSpace(*fixedTime) != "" {
		parsed, err := parseFixedTime(*fixedTime)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid fixed time: %v\n", err)
			os.Exit(1)
		}
		schema.SetFixedTime(parsed)
		if *verbose {
			fmt.Printf("Deterministic time enabled: %s\n", parsed.Format(time.RFC3339Nano))
		}
	}

	if *bundleFile == "" && *ociRef == "" {
		fmt.Fprintln(os.Stderr, "Either -bundle or -oci-ref must be specified")
		flag.PrintDefaults()
		os.Exit(1)
	}
	if *bundleFile != "" && *ociRef != "" {
		fmt.Fprintln(os.Stderr, "Use either -bundle or -oci-ref, not both")
		os.Exit(1)
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		fmt.Printf("Received signal %v, shutting down...\n", sig)
		cancel()
	}()

	// Create type system
	typeSystem := types.NewTypeSystem()
	if len(schemaSources) > 0 {
		if *verbose {
			fmt.Printf("Loading %d schema source(s)\n", len(schemaSources))
		}
		if err := schemasources.Apply(context.Background(), typeSystem, schemaSources, *verbose); err != nil {
			fmt.Fprintf(os.Stderr, "Error loading schema sources: %v\n", err)
			os.Exit(1)
		}
	}

	// Load bundle
	var bundle *unified.Bundle
	var err error

	if *bundleFile != "" {
		if *verbose {
			fmt.Printf("Loading bundle from file: %s\n", *bundleFile)
		}
		bundle, err = unified.LoadBundle(*bundleFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading bundle: %v\n", err)
			os.Exit(1)
		}
	} else if *ociRef != "" {
		if strings.TrimSpace(*ociSignatureVerifier) == "" {
			fmt.Fprintln(os.Stderr, "OCI loading requires --oci-signature-verifier")
			os.Exit(1)
		}
		if *verbose {
			fmt.Printf("Pulling bundle from OCI registry: %s\n", *ociRef)
		}
		puller := unified.NewOCIBundlePullerWithPolicy(*ociCacheDir, daemonOCIVerificationPolicy())
		bundle, err = puller.Pull(*ociRef)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error pulling bundle: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Printf("Loaded bundle: %s v%s\n", bundle.Name, bundle.Version)

	// Load schemas from bundle
	if *verbose {
		fmt.Printf("Loading %d schema files from bundle\n", len(bundle.SchemaFiles))
	}

	// Create verb registry
	verbReg, err := newConfiguredVerbRegistry(typeSystem)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid verb configuration: %v\n", err)
		os.Exit(1)
	}
	extensionDirs := splitCommaList(*extensionsDir)
	if strings.TrimSpace(*pluginDir) != "" {
		fmt.Fprintln(os.Stderr, "--plugin-dir is not supported by production effectusd; use immutable invocation-aware extension targets")
		os.Exit(1)
	}
	for _, directory := range splitCommaList(*verbDir) {
		if directory != "" {
			extensionDirs = append(extensionDirs, directory)
		}
	}
	extensionOCIs := splitCommaList(*extensionsOCI)
	if err := loadVerbsAndExtensions(verbReg, extensionDirs, extensionOCIs); err != nil {
		fmt.Fprintf(os.Stderr, "Error loading verbs/extensions: %v\n", err)
		os.Exit(1)
	}

	// Verify verb hash
	if verbReg.Count() > 0 {
		currentVerbHash := verbReg.GetVerbHash()
		if currentVerbHash != bundle.VerbHash {
			fmt.Fprintf(os.Stderr, "Warning: verb hash mismatch\n")
			fmt.Fprintf(os.Stderr, "  Bundle hash: %s\n", bundle.VerbHash)
			fmt.Fprintf(os.Stderr, "  Current hash: %s\n", currentVerbHash)
			// In production, you might want to fail here
		}
	}

	preparedBundle, err := compileBundleRules(bundle, typeSystem, verbReg, *verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error compiling bundle rules: %v\n", err)
		os.Exit(1)
	}
	bundle = preparedBundle
	if bundle.ListSpec != nil || bundle.FlowSpec != nil {
		fmt.Fprintln(os.Stderr, "Bundle uses legacy callback-based execution, which effectusd does not permit; migrate workflows to canonical .eff or .effx sources.")
		os.Exit(1)
	}
	if *sagaEnabled {
		fmt.Fprintln(os.Stderr, "--saga is rejected: the legacy daemon executor does not use the V2 outbox. Use runtime.ExecuteWorkflowWithIdentity with a configured durable outbox.")
		os.Exit(1)
	}

	var sagaStore schema.SagaStore
	var capSystem *capability.CapabilitySystem

	mergeDefault, err := parseMergeStrategy(*factsMergeDef)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid facts merge strategy: %v\n", err)
		os.Exit(1)
	}
	storeConfig := factStoreConfig{
		defaultStrategy: mergeDefault,
		perNamespace:    factsMergeNs.values,
		cache: factCacheConfig{
			policy:        strings.ToLower(*factsCache),
			maxUniverses:  *factsCacheMax,
			maxNamespaces: *factsCacheNs,
		},
	}

	var store factStore
	switch strings.ToLower(*factsStore) {
	case "memory":
		store = newMemoryFactStore(storeConfig)
	case "file":
		fileStore, err := newFileFactStore(*factsPath, storeConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading fact store: %v\n", err)
			os.Exit(1)
		}
		store = fileStore
	default:
		fmt.Fprintf(os.Stderr, "Unknown facts store: %s\n", *factsStore)
		os.Exit(1)
	}

	auth, err := buildAPIAuth(*apiAuthMode, *apiToken, *apiReadToken)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error configuring API auth: %v\n", err)
		os.Exit(1)
	}
	acl, err := loadACL(*apiACLFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading API ACL: %v\n", err)
		os.Exit(1)
	}

	limiter := newRateLimiter(*apiRateLimit, *apiRateBurst)

	// Create a WaitGroup to synchronize goroutines
	var wg sync.WaitGroup

	factCh := make(chan factEnvelope, 32)
	history := newBundleHistory(*rulesHistory, *rulesHistDir)
	state := newServerState(bundle, factCh, store, storeConfig, auth, limiter, acl, typeSystem, schemaSources, verbReg, *rulesHotload, history, *sagaEnabled, sagaStore, capSystem)
	state.recordBundleHistory(bundle, "startup")

	if err := validateFactSource(); err != nil {
		fmt.Fprintf(os.Stderr, "Error configuring fact source: %v\n", err)
		os.Exit(1)
	}
	var kafkaSource *kafkaadapter.KafkaSource
	var kafkaHandler kafkaadapter.Handler
	var recoveryWorker *effectusruntime.RecoveryWorker
	var execution *effectusruntime.ExecutionRuntime
	var executionDB *sql.DB
	var grpcExecutionServer *effectusruntime.RulesetExecutionServer
	needsCheckedEngine := true // HTTP, Kafka, and gRPC share one checked durable engine.
	if needsCheckedEngine {
		var configureErr error
		execution, executionDB, configureErr = configureDaemonExecutionEngine(ctx, bundle, extensionDirs, extensionOCIs)
		if configureErr != nil {
			fmt.Fprintf(os.Stderr, "Error creating checked execution engine: %v\n", configureErr)
			os.Exit(1)
		}
		state.SetCheckedEngine(execution.Engine())
		recoveryWorker, err = newDaemonRecoveryWorker(execution, executionDB)
		if err != nil {
			_ = executionDB.Close()
			fmt.Fprintf(os.Stderr, "Error creating recovery worker: %v\n", err)
			os.Exit(1)
		}
	}
	if strings.EqualFold(strings.TrimSpace(*factSource), "kafka") {
		kafkaHandler, err = newDaemonKafkaHandler(bundle, execution)
		if err == nil {
			kafkaSource, err = configureDaemonKafkaSource(executionDB)
		}
		if err != nil {
			_ = executionDB.Close()
			fmt.Fprintf(os.Stderr, "Error creating Kafka fact source: %v\n", err)
			os.Exit(1)
		}
		state.SetKafkaSource(kafkaSource)
	}
	if strings.TrimSpace(*grpcAddr) != "" {
		grpcExecutionServer, err = configureDaemonGRPCServer(execution, bundle)
		if err == nil {
			state.SetGRPCServer(grpcExecutionServer)
		}
		if err != nil {
			_ = executionDB.Close()
			fmt.Fprintf(os.Stderr, "Error creating gRPC execution service: %v\n", err)
			os.Exit(1)
		}
	}
	if executionDB != nil {
		defer executionDB.Close()
	}
	if execution != nil {
		defer execution.Close()
	}

	httpServer, httpListener, err := newHTTPServer(*httpAddr, state)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error starting HTTP server: %v\n", err)
		os.Exit(1)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := serveHTTPServer(ctx, httpServer, httpListener); err != nil {
			fmt.Fprintf(os.Stderr, "HTTP server error: %v\n", err)
			cancel()
		}
	}()

	// Start metrics server
	if *metricsAddr != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			startMetricsServer(ctx, *metricsAddr)
		}()
	}

	// Add hot-reloading if OCI reference is provided
	if *ociRef != "" && *reloadInterval > 0 {
		fmt.Fprintln(os.Stderr, "--reload-interval cannot poll an immutable OCI digest; publish and deploy a new digest instead")
		cancel()
	}

	extReloadInterval := *extensionsReloadInterval
	if extReloadInterval == 0 && *reloadInterval > 0 && (len(extensionDirs) > 0 || len(extensionOCIs) > 0) {
		extReloadInterval = *reloadInterval
	}
	if extReloadInterval > 0 && (len(extensionDirs) > 0 || len(extensionOCIs) > 0) {
		if *verbose {
			fmt.Printf("Enabling extension reload every %s\n", extReloadInterval)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(extReloadInterval)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if *verbose {
						fmt.Println("Reloading extension manifests...")
					}
					var reloadErr error
					if execution != nil {
						reloadErr = execution.HotReload(ctx)
					} else {
						reloadErr = reloadVerbsAndExtensions(state, extensionDirs, extensionOCIs)
					}
					if reloadErr != nil {
						fmt.Fprintf(os.Stderr, "Error reloading extensions: %v\n", reloadErr)
					}
				}
			}
		}()
	}

	schemaReloadInterval := *extensionsReloadInterval
	if schemaReloadInterval == 0 && *reloadInterval > 0 && len(schemaSources) > 0 {
		schemaReloadInterval = *reloadInterval
	}
	if schemaReloadInterval > 0 && len(schemaSources) > 0 {
		if *verbose {
			fmt.Printf("Enabling schema source reload every %s\n", schemaReloadInterval)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(schemaReloadInterval)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if *verbose {
						fmt.Println("Reloading schema sources...")
					}
					if err := reloadSchemaSources(ctx, state, schemaSources, *verbose); err != nil {
						fmt.Fprintf(os.Stderr, "Error reloading schema sources: %v\n", err)
					}
				}
			}
		}()
	}

	state.SetPhase(phaseRunning)
	if recoveryWorker != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := recoveryWorker.Run(ctx); err != nil && ctx.Err() == nil {
				fmt.Fprintf(os.Stderr, "Execution recovery worker error: %v\n", err)
				cancel()
			}
		}()
	}
	if grpcExecutionServer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stop := make(chan struct{})
			go func() {
				select {
				case <-ctx.Done():
					grpcExecutionServer.Stop()
				case <-stop:
				}
			}()
			err := grpcExecutionServer.Start()
			close(stop)
			if err != nil && ctx.Err() == nil {
				fmt.Fprintf(os.Stderr, "gRPC execution service error: %v\n", err)
				cancel()
			}
		}()
	}
	if kafkaSource != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := kafkaSource.Run(ctx, kafkaFactHandler{delegate: kafkaHandler}); err != nil && ctx.Err() == nil {
				fmt.Fprintf(os.Stderr, "Kafka source error: %v\n", err)
				cancel()
			}
		}()
	}

	// Main processing loop
	for {
		select {
		case <-ctx.Done():
			state.SetPhase(phaseDraining)
			fmt.Println("Shutting down, stopping admission...")
			shutdownCtx, stopShutdown := context.WithTimeout(context.WithoutCancel(ctx), *shutdownTimeout)
			defer stopShutdown()
			workersDone := make(chan struct{})
			go func() {
				wg.Wait()
				close(workersDone)
			}()
			select {
			case <-workersDone:
			case <-shutdownCtx.Done():
				fmt.Printf("Shutdown deadline reached: %v\n", shutdownCtx.Err())
				state.SetPhase(phaseStopped)
				return
			}
			for {
				select {
				case receivedFacts := <-factCh:
					if err := processFactEnvelope(shutdownCtx, state, receivedFacts); err != nil {
						fmt.Printf("Drain error: %v\n", err)
					}
				case <-shutdownCtx.Done():
					state.SetPhase(phaseStopped)
					return
				default:
					state.SetPhase(phaseStopped)
					return
				}
			}
		case receivedFacts := <-factCh:
			if err := processFactEnvelope(ctx, state, receivedFacts); err != nil {
				fmt.Printf("Execution error: %v\n", err)
			}
		}
	}
}

func processFactEnvelope(ctx context.Context, state *serverState, receivedFacts factEnvelope) error {
	return processFactEnvelopeOnGeneration(ctx, state, receivedFacts, state.generationSnapshot())
}

func processFactEnvelopeOnGeneration(ctx context.Context, state *serverState, receivedFacts factEnvelope, generation *runtimeGeneration) error {
	if generation == nil {
		return fmt.Errorf("runtime generation is required")
	}
	if receivedFacts.GenerationDigest != "" && receivedFacts.GenerationDigest != generation.bundleDigest {
		return fmt.Errorf("runtime generation digest changed before processing")
	}
	if state.factStore != nil {
		if err := state.factStore.Update(receivedFacts.Universe, receivedFacts.Facts); err != nil {
			return fmt.Errorf("persist facts: %w", err)
		}
	}
	if err := state.executeFactsOnGeneration(ctx, receivedFacts, generation); err != nil {
		return fmt.Errorf("execute facts: %w", err)
	}
	return nil
}

func reloadSchemaSources(ctx context.Context, state *serverState, sources []adapters.SchemaSourceConfig, verbose bool) error {
	generation := state.generationSnapshot()
	candidate := types.NewTypeSystem()
	if err := schemasources.Apply(ctx, candidate, sources, verbose); err != nil {
		return err
	}
	bundle, err := compileBundleRules(generation.bundle, candidate, generation.verbs, verbose)
	if err != nil {
		return fmt.Errorf("compile rules against schema candidate: %w", err)
	}
	return state.ActivateGeneration(bundle, candidate, generation.verbs, generation.id)
}

func newConfiguredVerbRegistry(typeSystem *types.TypeSystem) (*verb.Registry, error) {
	registry := verb.NewRegistry(typeSystem)
	if err := registry.SetDuplicatePolicy(*verbDuplicatePolicy); err != nil {
		return nil, err
	}
	strict := *verbStrict
	registry.SetStrictArgs(&strict)
	registry.SetStrictReturn(&strict)
	return registry, nil
}

func reloadVerbsAndExtensions(state *serverState, extensionDirs []string, extensionOCIs []string) error {
	generation := state.generationSnapshot()
	candidate, err := newConfiguredVerbRegistry(generation.schemaTypes)
	if err != nil {
		return err
	}
	if err := loadVerbsAndExtensions(candidate, extensionDirs, extensionOCIs); err != nil {
		return err
	}
	bundle, err := compileBundleRules(generation.bundle, generation.schemaTypes, candidate, false)
	if err != nil {
		return fmt.Errorf("compile rules against verb candidate: %w", err)
	}
	return state.ActivateGeneration(bundle, generation.schemaTypes, candidate, generation.id)
}

func loadVerbsAndExtensions(verbReg *verb.Registry, extensionDirs []string, extensionOCIs []string) error {
	if verbReg == nil {
		return nil
	}

	verbReg.Reset()

	if *verbDir != "" && *pluginDir != "" {
		return fmt.Errorf("use either -verb-dir or -plugin-dir, not both")
	}

	if *verbDir != "" {
		for _, dir := range splitCommaList(*verbDir) {
			if dir == "" {
				continue
			}
			if *verbose {
				fmt.Printf("Loading verb specs from directory: %s\n", dir)
			}
			if err := verbReg.RegisterDirectory(dir); err != nil {
				return fmt.Errorf("loading verb specs from %s: %w", dir, err)
			}
		}
	}

	if *pluginDir != "" {
		if *verbose {
			fmt.Printf("Loading verb plugins from directory: %s\n", *pluginDir)
		}
		if err := verbReg.LoadPlugins(*pluginDir); err != nil {
			return fmt.Errorf("loading verb plugins: %w", err)
		}
	}

	hasExtensions := len(extensionDirs) > 0 || len(extensionOCIs) > 0
	if hasExtensions {
		if *verbose {
			fmt.Printf("Loading extensions from %d dirs and %d OCI bundle(s)\n", len(extensionDirs), len(extensionOCIs))
		}

		em := loader.NewExtensionManager()
		for _, dir := range extensionDirs {
			if dir == "" {
				continue
			}
			loaders, err := loader.LoadFromDirectory(dir)
			if err != nil {
				return fmt.Errorf("loading extensions from %s: %w", dir, err)
			}
			for _, l := range loaders {
				em.AddLoader(l)
			}
		}

		for i, ref := range extensionOCIs {
			if ref == "" {
				continue
			}
			if strings.TrimSpace(*ociSignatureVerifier) == "" {
				return fmt.Errorf("OCI extensions require --oci-signature-verifier")
			}
			name := fmt.Sprintf("oci-%d", i+1)
			em.AddLoader(loader.NewOCIBundleLoaderWithPolicy(name, ref, daemonOCIVerificationPolicy()))
		}

		registry := schema.NewRegistry()
		if err := schema.LoadExtensionsIntoRegistries(em, registry, verbReg); err != nil {
			return fmt.Errorf("loading extension manifests: %w", err)
		}
	}
	setVerbSources(verbReg)
	if *verbOCIWarmup {
		if err := warmupOCIExecutors(context.Background(), verbReg); err != nil {
			return err
		}
	}
	instrumentVerbRegistry(verbReg)
	return nil
}

func splitCommaList(value string) []string {
	parts := strings.Split(value, ",")
	results := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		results = append(results, trimmed)
	}
	return results
}

func parseFixedTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed, nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, nil
	}
	return time.Time{}, fmt.Errorf("expected RFC3339/RFC3339Nano timestamp")
}

func validateFactSource() error {
	switch strings.ToLower(strings.TrimSpace(*factSource)) {
	case "", "http":
		return nil
	case "kafka":
		if err := kafkaadapter.ValidateConfig(daemonKafkaConfig()); err != nil {
			return err
		}
		if strings.TrimSpace(*sagaPgDSN) == "" {
			return fmt.Errorf("Kafka requires --saga-postgres-dsn for durable engine admission")
		}
		return nil
	default:
		return fmt.Errorf("unsupported fact source %q", *factSource)
	}
}
