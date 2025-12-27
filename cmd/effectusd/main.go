// cmd/effectusd/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/effectus/effectus-go/pathutil"
	"github.com/effectus/effectus-go/schema/types"
	"github.com/effectus/effectus-go/schema/verb"
	"github.com/effectus/effectus-go/unified"
)

// Placeholder types to simulate the eval package until it's properly implemented
type sagaStoreInterface interface{}
type listExecutor struct {
	verbRegistry *verb.Registry
	sagaEnabled  bool
	sagaStore    sagaStoreInterface
}

func newListExecutor(verbRegistry *verb.Registry, options ...func(*listExecutor)) *listExecutor {
	executor := &listExecutor{
		verbRegistry: verbRegistry,
	}

	// Apply options
	for _, option := range options {
		option(executor)
	}

	return executor
}

func withSaga(store sagaStoreInterface) func(*listExecutor) {
	return func(e *listExecutor) {
		e.sagaStore = store
		e.sagaEnabled = true
	}
}

func newMemorySagaStore() sagaStoreInterface {
	return &struct{}{}
}

func newRedisSagaStore(opts map[string]string) sagaStoreInterface {
	return &struct{}{}
}

func newPostgresSagaStore(opts map[string]string) sagaStoreInterface {
	return &struct{}{}
}

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
	bundleFile     = flag.String("bundle", "", "Path to bundle file")
	ociRef         = flag.String("oci-ref", "", "OCI reference for bundle (e.g., ghcr.io/user/bundle:v1)")
	pluginDir      = flag.String("plugin-dir", "", "Directory containing verb plugins")
	reloadInterval = flag.Duration("reload-interval", 30*time.Second, "Interval for hot-reloading")

	// Runtime flags
	sagaEnabled   = flag.Bool("saga", false, "Enable saga-style compensation")
	sagaStoreType = flag.String("saga-store", "memory", "Saga store (memory, redis, postgres)")

	// Monitoring flags
	metricsAddr = flag.String("metrics-addr", ":9090", "Address to expose metrics")
	pprofAddr   = flag.String("pprof-addr", ":6060", "Address to expose pprof")

	// Fact source flags
	factSource   = flag.String("fact-source", "http", "Fact source (http, kafka)")
	kafkaBrokers = flag.String("kafka-brokers", "localhost:9092", "Kafka brokers")
	kafkaTopic   = flag.String("kafka-topic", "facts", "Kafka topic")

	// HTTP server flags
	httpAddr = flag.String("http-addr", ":8080", "HTTP server address")

	// API auth + rate limit flags
	apiAuthMode   = flag.String("api-auth", "token", "API auth mode (token, disabled)")
	apiToken      = flag.String("api-token", "", "Write token for /api endpoints (comma-separated)")
	apiReadToken  = flag.String("api-read-token", "", "Read-only token for /api endpoints (comma-separated)")
	apiACLFile    = flag.String("api-acl-file", "", "Path to API ACL file (YAML/JSON)")
	apiRateLimit  = flag.Int("api-rate-limit", 120, "API requests per minute per client (0 to disable)")
	apiRateBurst  = flag.Int("api-rate-burst", 60, "API burst size (0 to use rate limit)")
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

func main() {
	flag.Var(&factsMergeNs, "facts-merge-namespace", "Namespace-specific merge strategy (namespace=first|last|error)")
	flag.Parse()

	if *bundleFile == "" && *ociRef == "" {
		fmt.Fprintln(os.Stderr, "Either -bundle or -oci-ref must be specified")
		flag.PrintDefaults()
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
		if *verbose {
			fmt.Printf("Pulling bundle from OCI registry: %s\n", *ociRef)
		}
		puller := unified.NewOCIBundlePuller("./bundles")
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
	verbReg := verb.NewRegistry(typeSystem)

	// Load verb plugins
	if *pluginDir != "" {
		if *verbose {
			fmt.Printf("Loading verb plugins from directory: %s\n", *pluginDir)
		}
		if err := verbReg.LoadPlugins(*pluginDir); err != nil {
			fmt.Fprintf(os.Stderr, "Error loading verb plugins: %v\n", err)
			os.Exit(1)
		}
	}

	// Verify verb hash
	currentVerbHash := verbReg.GetVerbHash()
	if currentVerbHash != bundle.VerbHash {
		fmt.Fprintf(os.Stderr, "Warning: verb hash mismatch\n")
		fmt.Fprintf(os.Stderr, "  Bundle hash: %s\n", bundle.VerbHash)
		fmt.Fprintf(os.Stderr, "  Current hash: %s\n", currentVerbHash)
		// In production, you might want to fail here
	}

	// Create executor options
	var execOpts []func(*listExecutor)

	// Add saga if enabled
	if *sagaEnabled {
		if *verbose {
			fmt.Printf("Enabling saga with store: %s\n", *sagaStoreType)
		}

		var store sagaStoreInterface
		switch *sagaStoreType {
		case "memory":
			store = newMemorySagaStore()
		case "redis":
			// In a real implementation, these options would be configurable
			redisOpts := map[string]string{
				"addr": "localhost:6379",
			}
			store = newRedisSagaStore(redisOpts)
		case "postgres":
			// In a real implementation, these options would be configurable
			pgOpts := map[string]string{
				"connString": "postgres://user:password@localhost:5432/effectus",
			}
			store = newPostgresSagaStore(pgOpts)
		default:
			fmt.Fprintf(os.Stderr, "Unknown saga store: %s\n", *sagaStoreType)
			os.Exit(1)
		}

		execOpts = append(execOpts, withSaga(store))
	}

	// Create executor and use it
	executor := newListExecutor(verbReg, execOpts...)
	_ = executor // Use the executor variable to avoid unused variable warning

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

	auth, generatedToken, err := buildAPIAuth(*apiAuthMode, *apiToken, *apiReadToken)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error configuring API auth: %v\n", err)
		os.Exit(1)
	}
	if generatedToken != "" {
		fmt.Printf("Generated API token: %s\n", generatedToken)
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
	state := newServerState(bundle, factCh, store, auth, limiter, acl)

	// Start fact source (non-HTTP)
	wg.Add(1)
	go func() {
		defer wg.Done()
		startFactSource(ctx, state)
	}()

	// Start HTTP server for API
	wg.Add(1)
	go func() {
		defer wg.Done()
		startHTTPServer(ctx, *httpAddr, state)
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
		if *verbose {
			fmt.Printf("Enabling hot-reloading every %s\n", *reloadInterval)
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(*reloadInterval)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if *verbose {
						fmt.Println("Checking for bundle updates...")
					}

					puller := unified.NewOCIBundlePuller("./bundles")
					newBundle, err := puller.Pull(*ociRef)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Error pulling bundle update: %v\n", err)
						continue
					}

					if newBundle.Version != bundle.Version {
						fmt.Printf("Updated bundle from %s to %s\n", bundle.Version, newBundle.Version)
						bundle = newBundle
						state.SetBundle(newBundle)
					}
				}
			}
		}()
	}

	// Main processing loop
	for {
		select {
		case <-ctx.Done():
			fmt.Println("Shutting down, waiting for goroutines to finish...")
			wg.Wait()
			return
		case receivedFacts := <-factCh:
			bundle = state.Bundle()
			// Process facts with rules
			if bundle.ListSpec != nil {
				for _, rule := range bundle.ListSpec.Rules {
					// Process the received facts with each rule
					fmt.Printf("Executing rule: %s with facts (universe=%s)\n", rule.Name, receivedFacts.Universe)
					_ = receivedFacts // Use the facts variable to avoid unused variable warning
				}
			}

			// Process facts with flows (if implemented)
			if bundle.FlowSpec != nil {
				// TODO: Implement flow execution
				if *verbose {
					fmt.Printf("Flow execution not implemented yet (found %d flows)\n",
						len(bundle.FlowSpec.Flows))
				}
			}
		}
	}
}

// startFactSource starts the appropriate fact source
func startFactSource(ctx context.Context, state *serverState) {
	if *factSource == "kafka" {
		startKafkaConsumer(ctx, state)
		return
	}
}

// startKafkaConsumer starts a Kafka consumer for facts
func startKafkaConsumer(ctx context.Context, state *serverState) {
	// Placeholder for Kafka consumer implementation
	fmt.Println("Starting Kafka consumer...")
	fmt.Printf("Brokers: %s\n", *kafkaBrokers)
	fmt.Printf("Topic: %s\n", *kafkaTopic)

	// In a real implementation, this would connect to Kafka
	// and read messages into the factCh
	<-ctx.Done()
	fmt.Println("Stopping Kafka consumer...")
}

// startMetricsServer starts the metrics server
func startMetricsServer(ctx context.Context, addr string) {
	fmt.Printf("Starting metrics server on %s\n", addr)

	// In a real implementation, you would initialize the metrics server here
	// For now, just a placeholder
	<-ctx.Done()
	fmt.Println("Shutting down metrics server")
}
