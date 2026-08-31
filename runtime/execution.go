package runtime

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/josephjohncox/effectus/compiler"
	"github.com/josephjohncox/effectus/loader"
	"github.com/josephjohncox/effectus/schema"
	"github.com/josephjohncox/effectus/schema/fencing"
	"github.com/josephjohncox/effectus/schema/ledger"
	"github.com/josephjohncox/effectus/schema/verb"
	"github.com/josephjohncox/effectus/schema/workflow"
	"github.com/segmentio/kafka-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

// ExecutionRuntime orchestrates the complete flow from extension loading to execution
type ExecutionRuntime struct {
	extensionManager     *loader.ExtensionManager
	compiler             *compiler.ExtensionCompiler
	activeGeneration     *ExecutionGeneration
	generationMetadata   GenerationMetadata
	executors            map[compiler.ExecutorType]ExecutorFactory
	workflowStore        workflow.OutboxStore
	workflowFencing      fencing.Provider
	workflowOptions      schema.DispatcherOptions
	engine               *Engine
	allowLegacyExecution bool
	mu                   sync.RWMutex
	compileMu            sync.Mutex
	executionMu          sync.RWMutex
	closeOnce            sync.Once
	closeErr             error
	state                RuntimeState
}

// GenerationMetadata is presentation metadata published with executable state.
type GenerationMetadata struct {
	Ruleset      string `json:"ruleset"`
	Version      string `json:"version"`
	BundleDigest string `json:"bundle_digest,omitempty"`
}

// CompileOptions controls how a checked generation is published.
type CompileOptions struct {
	// DiscardInitialData retains inferred fact types but removes loader values
	// before publication. Use this for type samples that are not runtime defaults.
	DiscardInitialData bool
}

// ExecutionGeneration is the sole published production generation. Its unit
// and extension snapshot are immutable and are retired together.
type ExecutionGeneration struct {
	unit             *compiler.CompiledUnit
	GenerationDigest string `json:"generation_digest"`
	IRDigest         string `json:"ir_digest"`
	GenerationMetadata
	PublishedAt time.Time `json:"published_at"`
}

// RuntimeState represents the current state of the runtime
type RuntimeState string

const (
	StateInitializing RuntimeState = "initializing"
	StateLoading      RuntimeState = "loading"
	StateCompiling    RuntimeState = "compiling"
	StateReady        RuntimeState = "ready"
	StateExecuting    RuntimeState = "executing"
	StateFailed       RuntimeState = "failed"
	StateClosing      RuntimeState = "closing"
	StateClosed       RuntimeState = "closed"
)

// NewExecutionRuntime creates a new execution runtime
func NewExecutionRuntime() *ExecutionRuntime {
	runtime := &ExecutionRuntime{
		extensionManager: loader.NewExtensionManager(),
		compiler:         compiler.NewExtensionCompiler(),
		executors:        make(map[compiler.ExecutorType]ExecutorFactory),
		state:            StateInitializing,
	}
	runtime.engine = newRuntimeEngine(runtime)

	// Checked extension compilation intentionally emits only local bindings.
	// Non-local factories remain available as compatibility APIs, but callers
	// must register them explicitly so unreachable transports are not enabled.
	runtime.RegisterExecutorFactory(compiler.ExecutorLocal, &LocalExecutorFactory{})

	return runtime
}

// Close releases executor connection pools. It is safe to call more than once.
func (er *ExecutionRuntime) Close() error {
	if er == nil {
		return nil
	}
	er.closeOnce.Do(func() {
		// Exclude all Engine.Execute calls. Calls that arrived before Close finish;
		// later calls observe StateClosed and fail before touching a snapshot.
		er.executionMu.Lock()
		defer er.executionMu.Unlock()
		er.mu.Lock()
		er.state = StateClosing
		factories := make([]ExecutorFactory, 0, len(er.executors))
		for _, factory := range er.executors {
			factories = append(factories, factory)
		}
		engine := er.engine
		generation := er.activeGeneration
		er.activeGeneration = nil
		er.mu.Unlock()

		if engine != nil {
			engine.mu.Lock()
			executions := make([]*engineExecution, 0, len(engine.executions))
			for _, execution := range engine.executions {
				executions = append(executions, execution)
			}
			engine.mu.Unlock()
			for _, execution := range executions {
				execution.mu.Lock()
				execution.releaseSnapshot()
				execution.mu.Unlock()
			}
		}
		if generation != nil && generation.unit != nil && generation.unit.ExtensionSnapshot != nil {
			er.closeErr = errors.Join(er.closeErr, generation.unit.ExtensionSnapshot.Retire())
		}
		for _, factory := range factories {
			if closer, ok := factory.(interface{ Close() error }); ok {
				er.closeErr = errors.Join(er.closeErr, closer.Close())
			}
		}
		er.mu.Lock()
		er.state = StateClosed
		er.mu.Unlock()
	})
	return er.closeErr
}

// RegisterExtensionLoader adds an extension loader to the runtime.
func (er *ExecutionRuntime) RegisterExtensionLoader(extensionLoader loader.Loader) {
	er.extensionManager.AddLoader(extensionLoader)
}

// ConfigureGenerationMetadata freezes bundle metadata into the next executable
// publication. Metadata cannot change independently after publication.
func (er *ExecutionRuntime) ConfigureGenerationMetadata(metadata GenerationMetadata) error {
	if er == nil {
		return fmt.Errorf("execution runtime is required")
	}
	er.mu.Lock()
	defer er.mu.Unlock()
	if er.activeGeneration != nil {
		return fmt.Errorf("generation metadata cannot change after publication")
	}
	metadata.Ruleset = strings.TrimSpace(metadata.Ruleset)
	metadata.Version = strings.TrimSpace(metadata.Version)
	if metadata.Ruleset == "" {
		metadata.Ruleset = "default"
	}
	if metadata.Version == "" {
		metadata.Version = "active"
	}
	er.generationMetadata = metadata
	return nil
}

// ActiveGeneration returns a copy of the one production publication view.
func (er *ExecutionRuntime) ActiveGeneration() *ExecutionGeneration {
	if er == nil {
		return nil
	}
	er.mu.RLock()
	defer er.mu.RUnlock()
	if er.activeGeneration == nil {
		return nil
	}
	copy := *er.activeGeneration
	copy.unit = nil
	return &copy
}

func (er *ExecutionRuntime) publishGeneration(unit *compiler.CompiledUnit, snapshot *loader.ExtensionSnapshot, expectedDigest string) error {
	if unit == nil || unit.CheckedIR == nil || snapshot == nil || unit.ExtensionSnapshot != snapshot {
		return fmt.Errorf("complete checked unit and executor snapshot are required")
	}
	if err := materializeExecutorDescriptors(unit, snapshot); err != nil {
		return err
	}
	artifact, err := executionArtifactForUnit(unit)
	if err != nil {
		return fmt.Errorf("build generation identity: %w", err)
	}
	er.mu.Lock()
	if er.state == StateClosing || er.state == StateClosed {
		er.mu.Unlock()
		return fmt.Errorf("runtime is closed")
	}
	currentDigest := ""
	if er.activeGeneration != nil {
		currentDigest = er.activeGeneration.GenerationDigest
	}
	if currentDigest != expectedDigest {
		er.mu.Unlock()
		return fmt.Errorf("generation publication conflict: expected %q, active %q", expectedDigest, currentDigest)
	}
	metadata := er.generationMetadata
	if metadata.Ruleset == "" {
		metadata.Ruleset = "default"
	}
	if metadata.Version == "" {
		metadata.Version = "active"
	}
	previous := er.activeGeneration
	er.activeGeneration = &ExecutionGeneration{
		unit: unit, GenerationDigest: artifact.GenerationDigest, IRDigest: artifact.IRDigest,
		GenerationMetadata: metadata, PublishedAt: time.Now().UTC(),
	}
	er.state = StateReady
	er.mu.Unlock()
	if previous != nil && previous.unit != nil && previous.unit.ExtensionSnapshot != nil && previous.unit.ExtensionSnapshot != snapshot {
		// Publication has committed. A previous generation cleanup failure must
		// never be reported as candidate rejection because callers would retire
		// the now-active snapshot. Cleanup is best-effort and independently
		// observable through the runtime log.
		if err := previous.unit.ExtensionSnapshot.Retire(); err != nil {
			log.Printf("retire previous execution generation %s: %v", previous.GenerationDigest, err)
		}
	}
	return nil
}

func materializeExecutorDescriptors(unit *compiler.CompiledUnit, snapshot *loader.ExtensionSnapshot) error {
	for name, compiled := range unit.VerbSpecs {
		if compiled == nil || compiled.ExecutorDescriptor == nil {
			continue
		}
		descriptor := *compiled.ExecutorDescriptor
		var implementation loader.VerbExecutor
		var err error
		switch strings.ToLower(strings.TrimSpace(descriptor.Type)) {
		case "http":
			implementation, err = loader.NewHTTPExecutor(descriptor.Config)
		case "grpc":
			implementation, err = loader.NewGRPCExecutor(descriptor.Config)
		case "stream", "message":
			implementation, err = loader.NewStreamExecutor(descriptor.Config)
		case "oci":
			implementation, err = loader.NewOCIExecutor(name, descriptor.Config)
		case "mock":
			implementation = &loader.MockExecutor{Name: "runtime:" + name}
		case "noop":
			implementation = &loader.NoOpExecutor{}
		default:
			err = fmt.Errorf("unsupported executor descriptor type %q", descriptor.Type)
		}
		if err != nil {
			return fmt.Errorf("construct runtime executor %q: %w", name, err)
		}
		compiled.Spec.Executor = implementation
		compiled.ExecutorType = compiler.ExecutorLocal
		compiled.ExecutorConfig = &compiler.LocalExecutorConfig{Implementation: implementation}
		if closer, ok := implementation.(io.Closer); ok {
			if err := snapshot.AttachCloser(closer); err != nil {
				_ = closer.Close()
				return fmt.Errorf("attach runtime executor %q: %w", name, err)
			}
		}
	}
	return nil
}

// RegisterExecutorFactory registers a factory for creating executors.
// Deprecated: descriptor-backed checked execution is constructed by runtime publication.
func (er *ExecutionRuntime) RegisterExecutorFactory(executorType compiler.ExecutorType, factory ExecutorFactory) {
	er.executors[executorType] = factory
}

// CompileAndValidate loads extensions, compiles them, and validates everything.
func (er *ExecutionRuntime) CompileAndValidate(ctx context.Context) error {
	return er.CompileAndValidateWithOptions(ctx, CompileOptions{})
}

// CompileAndValidateWithOptions loads, checks, and publishes one generation
// with the requested publication policy.
func (er *ExecutionRuntime) CompileAndValidateWithOptions(ctx context.Context, options CompileOptions) error {
	er.compileMu.Lock()
	defer er.compileMu.Unlock()

	er.mu.Lock()
	hasActiveGeneration := er.state == StateReady && er.activeGeneration != nil
	expected := ""
	if er.activeGeneration != nil {
		expected = er.activeGeneration.GenerationDigest
	}
	if !hasActiveGeneration {
		er.state = StateCompiling
	}
	er.mu.Unlock()

	snapshot, err := er.extensionManager.Stage(ctx, loader.StageOptions{})
	if err != nil {
		er.markInitialCompilationFailed(hasActiveGeneration)
		return fmt.Errorf("extension staging failed: %w", err)
	}
	result, err := er.compiler.CompileSnapshot(ctx, snapshot)
	if err != nil {
		_ = snapshot.Retire()
		er.markInitialCompilationFailed(hasActiveGeneration)
		return fmt.Errorf("compilation failed: %w", err)
	}
	if !result.Success {
		_ = snapshot.Retire()
		er.markInitialCompilationFailed(hasActiveGeneration)
		return fmt.Errorf("compilation errors: %v", result.Errors)
	}
	if options.DiscardInitialData {
		result.CompiledUnit.InitialData = make(map[string]interface{})
	}
	if err := er.publishGeneration(result.CompiledUnit, snapshot, expected); err != nil {
		_ = snapshot.Retire()
		er.markInitialCompilationFailed(hasActiveGeneration)
		return fmt.Errorf("publish compiled generation: %w", err)
	}

	for _, warning := range result.Warnings {
		log.Printf("Warning: %s in %s: %s", warning.Type, warning.Location, warning.Message)
	}
	log.Printf("Runtime compiled successfully with %d verbs, %d functions",
		len(result.CompiledUnit.VerbSpecs), len(result.CompiledUnit.Functions))
	return nil
}

func (er *ExecutionRuntime) markInitialCompilationFailed(hasActiveGeneration bool) {
	if hasActiveGeneration {
		return
	}
	er.mu.Lock()
	er.state = StateFailed
	er.mu.Unlock()
}

// ExecuteVerb executes a specific verb with the given arguments
func (er *ExecutionRuntime) ExecuteVerb(ctx context.Context, verbName string, args map[string]interface{}) (interface{}, error) {
	er.mu.RLock()
	state, generation, allowed := er.state, er.activeGeneration, er.allowLegacyExecution
	er.mu.RUnlock()
	var unit *compiler.CompiledUnit
	if generation != nil {
		unit = generation.unit
	}
	if state != StateReady || unit == nil {
		return nil, fmt.Errorf("runtime not ready (state: %s)", state)
	}
	if !allowed {
		return nil, fmt.Errorf("direct verb execution is legacy compatibility only; use Engine.Execute")
	}
	return er.executeVerbOnUnit(ctx, unit, verbName, args)
}

// EnableLegacyExecutionForCompatibility permits unrestricted Go continuations.
// It must not be enabled by production deployments because callback-only
// executors cannot be reconstructed or guaranteed to preserve invocation metadata.
func (er *ExecutionRuntime) EnableLegacyExecutionForCompatibility() {
	if er == nil {
		return
	}
	er.mu.Lock()
	er.allowLegacyExecution = true
	er.mu.Unlock()
}

// ConfigureDurableWorkflowExecution installs the mandatory outbox boundary for
// checked DURABLE_* workflows. Configure this before execution or hot reload.
func (er *ExecutionRuntime) ConfigureDurableWorkflowExecution(store workflow.OutboxStore, provider fencing.Provider, options schema.DispatcherOptions) error {
	if store == nil {
		return fmt.Errorf("durable workflow outbox store is required")
	}
	if strings.TrimSpace(options.Owner) == "" {
		return fmt.Errorf("durable workflow dispatcher owner is required")
	}
	er.mu.Lock()
	er.workflowStore = store
	er.workflowFencing = provider
	er.workflowOptions = options
	er.mu.Unlock()
	if durableLedger, ok := store.(ledger.ExecutionLedger); ok {
		return er.engine.ConfigureLedger(durableLedger, nil)
	}
	return nil
}

// ConfigureExecutionLedger installs durable admission/recovery persistence and
// an immutable resolver for generations loaded after restart.
func (er *ExecutionRuntime) ConfigureExecutionLedger(durableLedger ledger.ExecutionLedger, resolver ArtifactResolver) error {
	if er == nil {
		return fmt.Errorf("execution runtime is required")
	}
	return er.Engine().ConfigureLedger(durableLedger, resolver)
}

// ExecuteWorkflow is retained as a fail-closed compatibility method. Durable
// recovery requires a caller-supplied stable execution identity.
func (er *ExecutionRuntime) ExecuteWorkflow(ctx context.Context, facts map[string]interface{}) error {
	return fmt.Errorf("checked durable workflow requires ExecuteWorkflowWithIdentity")
}

// Engine returns the shared checked execution API.
func (er *ExecutionRuntime) Engine() *Engine {
	engine, _ := NewEngine(er)
	return engine
}

// ExecuteWorkflowWithIdentity is a compatibility facade over Engine.Execute.
func (er *ExecutionRuntime) ExecuteWorkflowWithIdentity(ctx context.Context, namespace, executionID string, facts map[string]interface{}) error {
	if er == nil || er.engine == nil {
		return fmt.Errorf("runtime engine is unavailable")
	}
	_, err := er.engine.Execute(ctx, ExecuteRequest{Admission: &Admission{
		ExecutionID: executionID, TenantNamespace: namespace, Ruleset: "default", Version: "active", Facts: facts,
	}, WaitMode: WaitTerminal})
	return err
}

// GetRuntimeInfo returns information about the current runtime state
func (er *ExecutionRuntime) GetRuntimeInfo() *RuntimeInfo {
	er.mu.RLock()
	defer er.mu.RUnlock()

	info := &RuntimeInfo{State: er.state, LoaderCount: len(er.extensionManager.GetLoaders())}
	generation := er.activeGeneration
	if generation != nil && generation.unit != nil {
		info.GenerationDigest = generation.GenerationDigest
		info.IRDigest = generation.IRDigest
		info.Ruleset = generation.Ruleset
		info.Version = generation.Version
		info.BundleDigest = generation.BundleDigest
		info.PublishedAt = generation.PublishedAt
		info.VerbCount = len(generation.unit.VerbSpecs)
		info.FunctionCount = len(generation.unit.Functions)
		if generation.unit.CheckedIR != nil {
			info.PlanCount = len(generation.unit.CheckedIR.CloneArtifact().Plans)
		}
		info.Dependencies = generation.unit.Dependencies
		info.Capabilities = generation.unit.Capabilities
	}
	return info
}

// HotReload reloads and recompiles all extensions
func (er *ExecutionRuntime) HotReload(ctx context.Context) error {
	er.compileMu.Lock()
	defer er.compileMu.Unlock()
	log.Println("Starting hot reload...")
	er.mu.RLock()
	expected := ""
	if er.activeGeneration != nil {
		expected = er.activeGeneration.GenerationDigest
	}
	er.mu.RUnlock()

	snapshot, err := er.extensionManager.Stage(ctx, loader.StageOptions{})
	if err != nil {
		return fmt.Errorf("hot reload staging failed: %w", err)
	}
	result, err := er.compiler.CompileSnapshot(ctx, snapshot)
	if err != nil {
		_ = snapshot.Retire()
		return fmt.Errorf("hot reload failed: %w", err)
	}
	if !result.Success {
		_ = snapshot.Retire()
		return fmt.Errorf("hot reload failed: %v", result.Errors)
	}
	if err := er.publishGeneration(result.CompiledUnit, snapshot, expected); err != nil {
		_ = snapshot.Retire()
		return fmt.Errorf("hot reload publish failed: %w", err)
	}

	if len(result.Warnings) > 0 {
		for _, warning := range result.Warnings {
			log.Printf("Warning: %s in %s: %s", warning.Type, warning.Location, warning.Message)
		}
	}

	log.Println("Hot reload completed successfully")
	return nil
}

// Helper methods

type runtimeVerbRegistry struct {
	specs map[string]*compiler.CompiledVerbSpec
}

func (r runtimeVerbRegistry) GetVerb(name string) (*verb.Spec, bool) {
	if r.specs == nil {
		return nil, false
	}
	compiled, ok := r.specs[name]
	if !ok || compiled == nil {
		return nil, false
	}
	return compiled.Spec, true
}

func (er *ExecutionRuntime) executePhase(ctx context.Context, phase compiler.ExecutionPhase, facts map[string]interface{}) error {
	// Set timeout if specified
	if phase.Timeout != "" {
		timeout, err := time.ParseDuration(phase.Timeout)
		if err != nil {
			return fmt.Errorf("invalid timeout: %w", err)
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	if phase.Parallel {
		return er.executeVerbsParallel(ctx, phase.Verbs, facts, phase.ErrorPolicy)
	} else {
		return er.executeVerbsSequential(ctx, phase.Verbs, facts, phase.ErrorPolicy)
	}
}

func (er *ExecutionRuntime) executeVerbsSequential(ctx context.Context, verbs []string, facts map[string]interface{}, policy compiler.ErrorPolicy) error {
	for _, verbName := range verbs {
		if err := er.executeVerbWithFacts(ctx, verbName, facts); err != nil {
			switch policy {
			case compiler.ErrorPolicyFail:
				return err
			case compiler.ErrorPolicyContinue:
				log.Printf("Verb %s failed, continuing: %v", verbName, err)
			case compiler.ErrorPolicyRetry:
				// Simple retry logic
				if retryErr := er.executeVerbWithFacts(ctx, verbName, facts); retryErr != nil {
					return fmt.Errorf("verb %s failed after retry: %w", verbName, retryErr)
				}
			}
		}
	}
	return nil
}

func (er *ExecutionRuntime) executeVerbsParallel(ctx context.Context, verbs []string, facts map[string]interface{}, policy compiler.ErrorPolicy) error {
	var wg sync.WaitGroup
	errChan := make(chan error, len(verbs))

	for _, verbName := range verbs {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			if err := er.executeVerbWithFacts(ctx, name, facts); err != nil {
				errChan <- fmt.Errorf("verb %s: %w", name, err)
			}
		}(verbName)
	}

	wg.Wait()
	close(errChan)

	// Collect errors
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 && policy == compiler.ErrorPolicyFail {
		return fmt.Errorf("parallel execution failed: %v", errors)
	}

	return nil
}

func (er *ExecutionRuntime) executeVerbWithFacts(ctx context.Context, verbName string, facts map[string]interface{}) error {
	// Execute verb using facts as arguments
	// In a real implementation, this would extract the appropriate arguments from facts
	_, err := er.ExecuteVerb(ctx, verbName, facts)
	return err
}

// Supporting types

// RuntimeInfo provides information about the runtime state
type RuntimeInfo struct {
	State            RuntimeState `json:"state"`
	GenerationDigest string       `json:"generationDigest,omitempty"`
	IRDigest         string       `json:"irDigest,omitempty"`
	Ruleset          string       `json:"ruleset,omitempty"`
	Version          string       `json:"version,omitempty"`
	BundleDigest     string       `json:"bundleDigest,omitempty"`
	PublishedAt      time.Time    `json:"publishedAt,omitempty"`
	LoaderCount      int          `json:"loaderCount"`
	VerbCount        int          `json:"verbCount"`
	FunctionCount    int          `json:"functionCount"`
	PlanCount        int          `json:"planCount"`
	Dependencies     []string     `json:"dependencies"`
	Capabilities     []string     `json:"capabilities"`
}

// ExecutorFactory creates executors for different types
type ExecutorFactory interface {
	CreateExecutor(config compiler.ExecutorConfig) (VerbExecutor, error)
}

// VerbExecutor defines the interface for executing verbs
type VerbExecutor interface {
	Execute(ctx context.Context, args map[string]interface{}) (interface{}, error)
}

// Executor factory implementations
type LocalExecutorFactory struct{}
type HTTPExecutorFactory struct{}
type GRPCExecutorFactory struct {
	mu    sync.Mutex
	conns map[string]*grpc.ClientConn
}
type MessageExecutorFactory struct{}
type MockExecutorFactory struct{}

func (lef *LocalExecutorFactory) CreateExecutor(config compiler.ExecutorConfig) (VerbExecutor, error) {
	localConfig, ok := config.(*compiler.LocalExecutorConfig)
	if !ok {
		return nil, fmt.Errorf("invalid config type for local executor")
	}
	return &LocalExecutorAdapter{impl: localConfig.Implementation}, nil
}

func (hef *HTTPExecutorFactory) CreateExecutor(config compiler.ExecutorConfig) (VerbExecutor, error) {
	httpConfig, ok := config.(*compiler.HTTPExecutorConfig)
	if !ok {
		return nil, fmt.Errorf("invalid config type for HTTP executor")
	}
	if err := httpConfig.Validate(); err != nil {
		return nil, err
	}
	timeout := parseDuration(httpConfig.Timeout, 10*time.Second)
	client := (loader.OutboundNetworkPolicy{AllowPrivate: httpConfig.AllowPrivateNetwork}).HTTPClient(timeout, httpConfig.Headers)
	return &HTTPExecutor{config: httpConfig, client: client}, nil
}

func (gef *GRPCExecutorFactory) CreateExecutor(config compiler.ExecutorConfig) (VerbExecutor, error) {
	grpcConfig, ok := config.(*compiler.GRPCExecutorConfig)
	if !ok {
		return nil, fmt.Errorf("invalid config type for gRPC executor")
	}
	if err := grpcConfig.Validate(); err != nil {
		return nil, err
	}

	if _, err := gef.getConn(grpcConfig); err != nil {
		return nil, err
	}
	return &GRPCExecutor{config: grpcConfig, factory: gef}, nil
}

func (gef *GRPCExecutorFactory) getConn(config *compiler.GRPCExecutorConfig) (*grpc.ClientConn, error) {
	gef.mu.Lock()
	defer gef.mu.Unlock()

	if gef.conns == nil {
		gef.conns = make(map[string]*grpc.ClientConn)
	}

	key := grpcConnectionKey(config)
	if conn, ok := gef.conns[key]; ok {
		if conn.GetState() != connectivity.Shutdown {
			return conn, nil
		}
		_ = conn.Close()
		delete(gef.conns, key)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var transport credentials.TransportCredentials
	if config.UseTLS {
		serverName := config.ServerName
		if serverName == "" {
			serverName, _, _ = net.SplitHostPort(config.Address)
		}
		transport = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName})
	} else if config.Insecure {
		transport = insecure.NewCredentials()
	} else {
		return nil, fmt.Errorf("gRPC transport security is not configured")
	}
	conn, err := grpc.DialContext(ctx, config.Address, grpc.WithTransportCredentials(transport), grpc.WithBlock())
	if err != nil {
		return nil, fmt.Errorf("dial gRPC %s: %w", config.Address, err)
	}

	gef.conns[key] = conn
	return conn, nil
}

func grpcConnectionKey(config *compiler.GRPCExecutorConfig) string {
	return fmt.Sprintf("%s|tls=%t|insecure=%t|server=%s", config.Address, config.UseTLS, config.Insecure, config.ServerName)
}
func (gef *GRPCExecutorFactory) invalidate(config *compiler.GRPCExecutorConfig, expected *grpc.ClientConn) {
	gef.mu.Lock()
	defer gef.mu.Unlock()
	key := grpcConnectionKey(config)
	if current := gef.conns[key]; current == expected {
		delete(gef.conns, key)
		_ = current.Close()
	}
}
func (gef *GRPCExecutorFactory) Close() error {
	gef.mu.Lock()
	defer gef.mu.Unlock()
	var result error
	for key, conn := range gef.conns {
		result = errors.Join(result, conn.Close())
		delete(gef.conns, key)
	}
	return result
}

func (mef *MessageExecutorFactory) CreateExecutor(config compiler.ExecutorConfig) (VerbExecutor, error) {
	messageConfig, ok := config.(*compiler.MessageExecutorConfig)
	if !ok {
		return nil, fmt.Errorf("invalid config type for message executor")
	}
	if err := messageConfig.Validate(); err != nil {
		return nil, err
	}
	executor, err := newMessageExecutor(messageConfig)
	if err != nil {
		return nil, err
	}
	return executor, nil
}

func (mef *MockExecutorFactory) CreateExecutor(config compiler.ExecutorConfig) (VerbExecutor, error) {
	return &MockExecutor{}, nil
}

// Executor implementations
type LocalExecutorAdapter struct {
	impl loader.VerbExecutor
}

func (lea *LocalExecutorAdapter) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return lea.impl.Execute(ctx, args)
}

type HTTPExecutor struct {
	config *compiler.HTTPExecutorConfig
	client *http.Client
}

func (he *HTTPExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if he.client == nil || he.client.Timeout <= 0 {
		return nil, fmt.Errorf("HTTP executor requires a finite timeout")
	}
	payload, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("encode args: %w", err)
	}

	return executeWithRetry(ctx, he.config.RetryPolicy, func() (interface{}, error) {
		req, err := http.NewRequestWithContext(ctx, strings.ToUpper(he.config.Method), he.config.URL, bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		for key, value := range he.config.Headers {
			req.Header.Set(key, value)
		}

		resp, err := he.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		const maxRuntimeHTTPResponse = 1 << 20
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxRuntimeHTTPResponse+1))
		if err != nil {
			return nil, fmt.Errorf("read HTTP response: %w", err)
		}
		if len(body) > maxRuntimeHTTPResponse {
			return nil, fmt.Errorf("HTTP response exceeds %d bytes", maxRuntimeHTTPResponse)
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return nil, fmt.Errorf("http status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		if len(body) == 0 {
			return true, nil
		}

		var decoded interface{}
		if err := json.Unmarshal(body, &decoded); err == nil {
			return decoded, nil
		}

		return strings.TrimSpace(string(body)), nil
	})
}

type GRPCExecutor struct {
	config  *compiler.GRPCExecutorConfig
	factory *GRPCExecutorFactory
}

func (ge *GRPCExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if ge.factory == nil {
		return nil, fmt.Errorf("gRPC connection factory is nil")
	}
	req, err := structpb.NewStruct(args)
	if err != nil {
		return nil, fmt.Errorf("encode args: %w", err)
	}

	callCtx := ctx
	if ge.config.Timeout != "" {
		timeout := parseDuration(ge.config.Timeout, 10*time.Second)
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	if len(ge.config.Metadata) > 0 {
		md := metadata.New(ge.config.Metadata)
		callCtx = metadata.NewOutgoingContext(callCtx, md)
	}

	result, err := executeWithRetry(callCtx, ge.config.RetryPolicy, func() (interface{}, error) {
		conn, err := ge.factory.getConn(ge.config)
		if err != nil {
			return nil, grpcInvocationError{err: err, retrySafe: ge.config.RetrySafe}
		}
		var resp structpb.Struct
		if err := grpc.Invoke(callCtx, ge.config.Method, req, &resp, conn); err != nil {
			if status.Code(err) == codes.Unavailable || conn.GetState() == connectivity.Shutdown {
				ge.factory.invalidate(ge.config, conn)
			}
			return nil, grpcInvocationError{err: err, retrySafe: ge.config.RetrySafe && grpcRetryEligible(callCtx, err)}
		}
		return resp.AsMap(), nil
	})
	return result, err
}

type grpcInvocationError struct {
	err       error
	retrySafe bool
}

func (failure grpcInvocationError) Error() string   { return failure.err.Error() }
func (failure grpcInvocationError) Unwrap() error   { return failure.err }
func (failure grpcInvocationError) Retryable() bool { return failure.retrySafe }
func grpcRetryEligible(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	switch status.Code(err) {
	case codes.Unavailable, codes.ResourceExhausted, codes.Aborted:
		return true
	case codes.DeadlineExceeded:
		return ctx.Err() == nil
	default:
		return false
	}
}

type MessageExecutor struct {
	config    *compiler.MessageExecutorConfig
	publisher messagePublisher
}

func (me *MessageExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if me.publisher == nil {
		return nil, fmt.Errorf("message publisher not configured")
	}

	payload, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("encode args: %w", err)
	}

	if _, err := executeWithRetry(ctx, me.config.RetryPolicy, func() (interface{}, error) {
		return nil, me.publisher.Publish(ctx, payload)
	}); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"status": "queued",
		"target": messageTarget(me.config),
	}, nil
}

type MockExecutor struct{}

func (me *MockExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return map[string]interface{}{
		"status": "mock_success",
		"args":   args,
	}, nil
}

type messagePublisher interface {
	Publish(ctx context.Context, payload []byte) error
}

type httpPublisher struct {
	url     string
	headers map[string]string
	client  *http.Client
}

func (hp *httpPublisher) Publish(ctx context.Context, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hp.url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range hp.headers {
		req.Header.Set(key, value)
	}

	resp, err := hp.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
		if len(body) > 1<<20 {
			return fmt.Errorf("publisher status %d response exceeds %d bytes", resp.StatusCode, 1<<20)
		}
		return fmt.Errorf("publisher status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

type kafkaPublisher struct {
	writer *kafka.Writer
	key    []byte
}

type stdoutPublisher struct{}

func (sp *stdoutPublisher) Publish(ctx context.Context, payload []byte) error {
	fmt.Printf("verb.stream %s\n", strings.TrimSpace(string(payload)))
	return nil
}

func (kp *kafkaPublisher) Publish(ctx context.Context, payload []byte) error {
	return kp.writer.WriteMessages(ctx, kafka.Message{
		Key:   kp.key,
		Value: payload,
		Time:  time.Now(),
	})
}

func newMessageExecutor(config *compiler.MessageExecutorConfig) (*MessageExecutor, error) {
	timeout := parseDuration(config.Timeout, 10*time.Second)

	switch strings.ToLower(strings.TrimSpace(config.Publisher)) {
	case "http":
		client := (loader.OutboundNetworkPolicy{AllowPrivate: config.AllowPrivateNetwork}).HTTPClient(timeout, config.Headers)
		return &MessageExecutor{
			config: config,
			publisher: &httpPublisher{
				url:     config.URL,
				headers: config.Headers,
				client:  client,
			},
		}, nil
	case "kafka":
		writer := kafka.NewWriter(kafka.WriterConfig{
			Brokers:  config.Brokers,
			Topic:    config.Topic,
			Balancer: &kafka.LeastBytes{},
		})
		return &MessageExecutor{
			config: config,
			publisher: &kafkaPublisher{
				writer: writer,
				key:    []byte(config.RoutingKey),
			},
		}, nil
	case "stdout":
		return &MessageExecutor{
			config:    config,
			publisher: &stdoutPublisher{},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported publisher: %s", config.Publisher)
	}
}

func messageTarget(config *compiler.MessageExecutorConfig) string {
	if config == nil {
		return ""
	}
	if config.Publisher == "http" {
		return config.URL
	}
	if config.Publisher == "kafka" {
		return config.Topic
	}
	if config.Publisher == "stdout" {
		return "stdout"
	}
	if config.Topic != "" {
		return config.Topic
	}
	if config.Queue != "" {
		return config.Queue
	}
	return ""
}

func executeWithRetry(ctx context.Context, policy *compiler.RetryPolicy, fn func() (interface{}, error)) (interface{}, error) {
	if policy == nil || policy.MaxRetries <= 0 {
		return fn()
	}

	delay := parseDuration(policy.InitialDelay, 250*time.Millisecond)
	if delay <= 0 {
		delay = 250 * time.Millisecond
	}

	maxDelay := parseDuration(policy.MaxDelay, 5*time.Second)
	if maxDelay <= 0 {
		maxDelay = 5 * time.Second
	}

	backoff := policy.BackoffFactor
	if backoff <= 0 {
		backoff = 2
	}

	var lastErr error
	for attempt := 0; attempt <= policy.MaxRetries; attempt++ {
		result, err := fn()
		if err == nil {
			return result, nil
		}
		lastErr = err
		if attempt == policy.MaxRetries {
			break
		}
		var retryable interface{ Retryable() bool }
		if !errors.As(err, &retryable) || !retryable.Retryable() {
			break
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}

		delay = time.Duration(float64(delay) * backoff)
		if delay > maxDelay {
			delay = maxDelay
		}
	}

	return nil, lastErr
}

func parseDuration(raw string, fallback time.Duration) time.Duration {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return parsed
}
