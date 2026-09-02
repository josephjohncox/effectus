// Command effectusd runs one immutable source bundle through one checked generation.
package main

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/josephjohncox/effectus/bundle"
	kafka "github.com/josephjohncox/effectus/internal/daemon/kafka"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/runtime"
	"github.com/josephjohncox/effectus/schema"
	"github.com/josephjohncox/effectus/schema/fencing"
	_ "github.com/lib/pq"
)

var (
	bundleFile   = flag.String("bundle", "", "Path to effectus.source-bundle.v1 JSON")
	ociRef       = flag.String("oci-ref", "", "Digest-pinned OCI source bundle")
	ociVerifier  = flag.String("oci-signature-verifier", "", "Verifier executable; receives reference and digest")
	postgresDSN  = flag.String("postgres-dsn", "", "PostgreSQL DSN (or EFFECTUS_POSTGRES_DSN)")
	migrations   = flag.String("database-migrations", "validate", "validate or apply")
	migrateOnly  = flag.Bool("migrate-only", false, "Apply PostgreSQL migrations and exit")
	httpAddr     = flag.String("http-addr", ":8080", "HTTP listen address (empty disables HTTP)")
	grpcAddr     = flag.String("grpc-addr", "", "gRPC execution listen address")
	grpcCert     = flag.String("grpc-tls-cert", "", "gRPC TLS certificate PEM")
	grpcKey      = flag.String("grpc-tls-key", "", "gRPC TLS private-key PEM")
	grpcInsecure = flag.Bool("grpc-allow-insecure", false, "Allow plaintext gRPC")
	factSource   = flag.String("fact-source", "http", "Fact source: http or kafka")
	kafkaBrokers = flag.String("kafka-brokers", "localhost:9092", "Kafka brokers")
	kafkaTopic   = flag.String("kafka-topic", "facts", "Kafka topic")
	kafkaGroup   = flag.String("kafka-consumer-group", "effectusd", "Kafka consumer group")
	kafkaAck     = flag.String("kafka-ack-contract", "completed_processing", "Kafka acknowledgement contract")
)

type daemon struct {
	engine     *runtime.Engine
	generation *runtime.Generation
	db         *sql.DB
	recovery   *runtime.RecoveryWorker
}
type executeBody struct {
	Namespace string         `json:"namespace"`
	Universe  string         `json:"universe,omitempty"`
	Facts     map[string]any `json:"facts"`
}

const maxHTTPBodyBytes = 1 << 20

func main() {
	flag.Parse()
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "effectusd:", err)
		os.Exit(1)
	}
}
func run() error {
	if *postgresDSN == "" {
		*postgresDSN = os.Getenv("EFFECTUS_POSTGRES_DSN")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *migrateOnly || (*bundleFile == "" && *ociRef == "" && (strings.EqualFold(*migrations, "apply") || strings.EqualFold(*migrations, "validate"))) {
		return migrate(ctx, *migrateOnly || strings.EqualFold(*migrations, "apply"))
	}
	d, err := openDaemon(ctx)
	if err != nil {
		return err
	}
	defer d.close()

	needsToken := *httpAddr != "" || *grpcAddr != ""
	apiToken := strings.TrimSpace(os.Getenv("EFFECTUS_API_TOKEN"))
	if needsToken && apiToken == "" {
		return errors.New("EFFECTUS_API_TOKEN is required when HTTP or gRPC is enabled")
	}
	serviceErrors := make(chan error, 3)
	reportServiceError := func(name string, err error) {
		if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, http.ErrServerClosed) {
			return
		}
		select {
		case serviceErrors <- fmt.Errorf("%s: %w", name, err):
		default:
		}
	}
	if *httpAddr != "" {
		server := &http.Server{Addr: *httpAddr, Handler: d.httpHandler(apiToken), ReadHeaderTimeout: 10 * time.Second}
		go func() { reportServiceError("HTTP server", server.ListenAndServe()) }()
	}
	if *grpcAddr != "" {
		options, err := grpcServerOptions(d.generation, apiToken)
		if err != nil {
			return err
		}
		server, err := runtime.NewRulesetExecutionServerWithOptions(d.engine, *grpcAddr, options)
		if err != nil {
			return err
		}
		go func() { <-ctx.Done(); server.Stop() }()
		go func() { reportServiceError("gRPC server", server.Start()) }()
	}
	if strings.EqualFold(*factSource, "kafka") {
		source, handler, err := d.kafkaSource()
		if err != nil {
			return err
		}
		go func() { reportServiceError("Kafka consumer", source.Run(ctx, handler)) }()
	} else if !strings.EqualFold(*factSource, "http") {
		return fmt.Errorf("fact-source must be http or kafka")
	}
	go func() { reportServiceError("recovery worker", d.recovery.Run(ctx)) }()
	select {
	case err := <-serviceErrors:
		return err
	case <-ctx.Done():
		return nil
	}
}

func grpcServerOptions(generation *runtime.Generation, token string) (runtime.RulesetExecutionServerOptions, error) {
	authenticator, err := runtime.NewBearerTokenAuthenticator(token)
	if err != nil {
		return runtime.RulesetExecutionServerOptions{}, fmt.Errorf("configure gRPC authentication: %w", err)
	}
	options := runtime.RulesetExecutionServerOptions{RulesetName: generation.Ruleset(), Version: generation.Version(), Authenticator: authenticator, AllowInsecureTransport: *grpcInsecure}
	if *grpcInsecure {
		if *grpcCert != "" || *grpcKey != "" {
			return runtime.RulesetExecutionServerOptions{}, errors.New("gRPC TLS certificate and key cannot be set with --grpc-allow-insecure")
		}
		return options, nil
	}
	if *grpcCert == "" || *grpcKey == "" {
		return runtime.RulesetExecutionServerOptions{}, errors.New("--grpc-tls-cert and --grpc-tls-key are required unless --grpc-allow-insecure is set")
	}
	certificate, err := tls.LoadX509KeyPair(*grpcCert, *grpcKey)
	if err != nil {
		return runtime.RulesetExecutionServerOptions{}, fmt.Errorf("load gRPC TLS certificate: %w", err)
	}
	options.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}
	return options, nil
}

func migrate(ctx context.Context, apply bool) error {
	db, err := openDatabase()
	if err != nil {
		return err
	}
	defer db.Close()
	if apply {
		return schema.MigrateSagaV2(ctx, db)
	}
	return schema.ValidateSagaV2(ctx, db)
}
func openDaemon(ctx context.Context) (*daemon, error) {
	source, err := loadSourceBundle(ctx)
	if err != nil {
		return nil, err
	}
	registry, err := invocation.NewRegistry([]invocation.ResolverRegistration{{ID: invocation.HTTPResolverID, Resolver: invocation.HTTPResolver{}}})
	if err != nil {
		return nil, err
	}
	generation, err := runtime.CompileGeneration(ctx, runtime.GenerationBuildConfig{Bundle: source, Resolvers: registry, Production: true})
	if err != nil {
		return nil, fmt.Errorf("compile checked generation: %w", err)
	}
	engine, err := runtime.NewEngine(generation)
	if err != nil {
		_ = generation.Close()
		return nil, err
	}
	db, err := openDatabase()
	if err != nil {
		_ = engine.Close()
		return nil, err
	}
	closeErr := func(err error) (*daemon, error) { _ = db.Close(); _ = engine.Close(); return nil, err }
	if err := db.PingContext(ctx); err != nil {
		return closeErr(err)
	}
	if strings.EqualFold(*migrations, "apply") {
		if err := schema.MigrateSagaV2(ctx, db); err != nil {
			return closeErr(err)
		}
	} else if strings.EqualFold(*migrations, "validate") {
		if err := schema.ValidateSagaV2(ctx, db); err != nil {
			return closeErr(err)
		}
	} else {
		return closeErr(fmt.Errorf("database-migrations must be validate or apply"))
	}
	store, err := schema.NewPostgresOutboxStore(db)
	if err != nil {
		return closeErr(err)
	}
	provider, err := fencing.NewPostgresProvider(db)
	if err != nil {
		return closeErr(err)
	}
	if err := engine.ConfigureWorkflow(store, provider, schema.DispatcherOptions{Owner: "effectusd", RequireDurableFencing: true}); err != nil {
		return closeErr(err)
	}
	if err := engine.ConfigureLedger(store, runtime.NewManifestArtifactResolver(registry)); err != nil {
		return closeErr(err)
	}
	return &daemon{engine: engine, generation: generation, db: db, recovery: &runtime.RecoveryWorker{Engine: engine, Store: store, Owner: "effectusd-recovery", BatchSize: 32, LeaseDuration: 30 * time.Second, PollInterval: time.Second}}, nil
}
func (d *daemon) close() {
	if d == nil {
		return
	}
	if d.engine != nil {
		_ = d.engine.Close()
	}
	if d.db != nil {
		_ = d.db.Close()
	}
}
func openDatabase() (*sql.DB, error) {
	if strings.TrimSpace(*postgresDSN) == "" {
		return nil, errors.New("EFFECTUS_POSTGRES_DSN or --postgres-dsn is required")
	}
	return sql.Open("postgres", *postgresDSN)
}
func loadSourceBundle(ctx context.Context) (*bundle.SourceBundle, error) {
	if (*bundleFile == "") == (*ociRef == "") {
		return nil, fmt.Errorf("set exactly one of --bundle or --oci-ref")
	}
	if *bundleFile != "" {
		data, err := os.ReadFile(*bundleFile)
		if err != nil {
			return nil, err
		}
		return bundle.Parse(data)
	}
	if *ociVerifier == "" {
		return nil, fmt.Errorf("--oci-signature-verifier is required for OCI bundles")
	}
	return bundle.PullOCI(ctx, *ociRef, func(verifyContext context.Context, reference, digest string) error {
		command := exec.CommandContext(verifyContext, *ociVerifier, reference, digest)
		output, err := command.CombinedOutput()
		if err != nil {
			return fmt.Errorf("verify OCI bundle: %w: %s", err, strings.TrimSpace(string(output)))
		}
		return nil
	})
}
func (d *daemon) httpHandler(token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, d.engine.GenerationView()) })
	protected := http.NewServeMux()
	protected.HandleFunc("/v1/status", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, d.engine.GenerationView()) })
	protected.HandleFunc("/v1/dry-run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Facts map[string]any `json:"facts"`
		}
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, err)
			return
		}
		result, err := d.engine.DryRun(r.Context(), body.Facts)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	protected.HandleFunc("/v1/execute", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.ContentLength > maxHTTPBodyBytes {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body exceeds 1 MiB"})
			return
		}
		idempotencyKey := strings.TrimSpace(r.Header.Get(invocation.HeaderIdempotencyKey))
		if idempotencyKey == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": invocation.HeaderIdempotencyKey + " header is required"})
			return
		}
		var body executeBody
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, err)
			return
		}
		if body.Namespace == "" {
			body.Namespace = body.Universe
		}
		view := d.engine.GenerationView()
		expectedGeneration := strings.Trim(strings.TrimSpace(r.Header.Get("If-Match")), `"`)
		if expectedGeneration == "" {
			expectedGeneration = view.GenerationDigest
		}
		result, err := d.engine.Execute(r.Context(), runtime.ExecuteRequest{Admission: &runtime.Admission{ExecutionID: schema.StableExecutionID(body.Namespace, idempotencyKey, view.Ruleset, view.Version), AdmissionID: schema.StableAdmissionID(body.Namespace, idempotencyKey, view.Ruleset, view.Version), TenantNamespace: body.Namespace, Ruleset: view.Ruleset, Version: view.Version, Facts: body.Facts, ExpectedGenerationDigest: expectedGeneration}, WaitMode: runtime.WaitAccepted})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, result)
	})
	mux.Handle("/v1/", httpTokenMiddleware(token, protected))
	return mux
}

func httpTokenMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.Header.Values("Authorization")
		if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") || !constantTimeTokenEqual(strings.TrimPrefix(values[0], "Bearer "), token) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="effectusd"`)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication failed"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func constantTimeTokenEqual(candidate, expected string) bool {
	if candidate == "" || expected == "" || len(candidate) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(expected)) == 1
}

func decodeJSON(r *http.Request, value any) error {
	de := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	de.DisallowUnknownFields()
	if err := de.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := de.Decode(&extra); err == nil {
		return errors.New("multiple JSON values")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, runtime.ErrIdentityConflict) || errors.Is(err, runtime.ErrGenerationMismatch) {
		status = http.StatusConflict
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
func (d *daemon) kafkaSource() (*kafka.KafkaSource, kafka.Handler, error) {
	config := &kafka.Config{SourceID: "effectusd", ClusterNamespace: "default", Brokers: strings.Split(*kafkaBrokers, ","), Topic: *kafkaTopic, ConsumerGroup: *kafkaGroup, AckContract: kafka.AckContract(*kafkaAck), MaxAttempts: 3, InitialBackoff: time.Second, MaxBackoff: 30 * time.Second, PoisonPolicy: kafka.PoisonHalt}
	source, err := kafka.NewKafkaSource(config)
	if err != nil {
		return nil, nil, err
	}
	tracker, err := kafka.NewPostgresAttemptTracker(d.db)
	if err != nil {
		return nil, nil, err
	}
	if err := source.SetAttemptTracker(tracker); err != nil {
		return nil, nil, err
	}
	waitMode, err := kafka.WaitModeForAckContract(config.AckContract)
	if err != nil {
		return nil, nil, err
	}
	handler, err := kafka.NewEngineHandler(kafka.EngineHandlerConfig{Ruleset: d.generation.Ruleset(), Version: d.generation.Version(), WaitMode: waitMode}, d.engine)
	return source, handler, err
}
