package runtime

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/effectus/effectus-go/common"
	"github.com/effectus/effectus-go/compiler"
	"github.com/effectus/effectus-go/loader"
	"github.com/effectus/effectus-go/schema/verb"
	"github.com/segmentio/kafka-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/structpb"
)

// ExecutionRuntime orchestrates the complete flow from extension loading to execution
type ExecutionRuntime struct {
	extensionManager *loader.ExtensionManager
	compiler         *compiler.ExtensionCompiler
	compiledUnit     *compiler.CompiledUnit
	executors        map[compiler.ExecutorType]ExecutorFactory
	mu               sync.RWMutex
	state            RuntimeState
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
)

// NewExecutionRuntime creates a new execution runtime
func NewExecutionRuntime() *ExecutionRuntime {
	runtime := &ExecutionRuntime{
		extensionManager: loader.NewExtensionManager(),
		compiler:         compiler.NewExtensionCompiler(),
		executors:        make(map[compiler.ExecutorType]ExecutorFactory),
		state:            StateInitializing,
	}

	// Register default executor factories
	runtime.RegisterExecutorFactory(compiler.ExecutorLocal, &LocalExecutorFactory{})
	runtime.RegisterExecutorFactory(compiler.ExecutorHTTP, &HTTPExecutorFactory{})
	runtime.RegisterExecutorFactory(compiler.ExecutorGRPC, &GRPCExecutorFactory{})
	runtime.RegisterExecutorFactory(compiler.ExecutorMessage, &MessageExecutorFactory{})
	runtime.RegisterExecutorFactory(compiler.ExecutorMock, &MockExecutorFactory{})

	return runtime
}

// RegisterExtensionLoader adds an extension loader to the runtime
func (er *ExecutionRuntime) RegisterExtensionLoader(loader loader.Loader) {
	er.extensionManager.AddLoader(loader)
}

// RegisterExecutorFactory registers a factory for creating executors
func (er *ExecutionRuntime) RegisterExecutorFactory(executorType compiler.ExecutorType, factory ExecutorFactory) {
	er.executors[executorType] = factory
}

// CompileAndValidate loads extensions, compiles them, and validates everything
func (er *ExecutionRuntime) CompileAndValidate(ctx context.Context) error {
	er.mu.Lock()
	defer er.mu.Unlock()

	er.state = StateLoading

	// Compile extensions
	er.state = StateCompiling
	result, err := er.compiler.Compile(ctx, er.extensionManager)
	if err != nil {
		er.state = StateFailed
		return fmt.Errorf("compilation failed: %w", err)
	}

	if !result.Success {
		er.state = StateFailed
		return fmt.Errorf("compilation errors: %v", result.Errors)
	}

	// Store compiled unit
	er.compiledUnit = result.CompiledUnit
	er.state = StateReady

	// Log warnings if any
	if len(result.Warnings) > 0 {
		for _, warning := range result.Warnings {
			log.Printf("Warning: %s in %s: %s", warning.Type, warning.Location, warning.Message)
		}
	}

	log.Printf("Runtime compiled successfully with %d verbs, %d functions",
		len(er.compiledUnit.VerbSpecs),
		len(er.compiledUnit.Functions))

	return nil
}

// ExecuteVerb executes a specific verb with the given arguments
func (er *ExecutionRuntime) ExecuteVerb(ctx context.Context, verbName string, args map[string]interface{}) (interface{}, error) {
	er.mu.RLock()
	defer er.mu.RUnlock()

	if er.state != StateReady {
		return nil, fmt.Errorf("runtime not ready (state: %s)", er.state)
	}

	verbSpec, exists := er.compiledUnit.VerbSpecs[verbName]
	if !exists {
		return nil, fmt.Errorf("verb not found: %s", verbName)
	}

	// Validate arguments (strict mode only)
	if err := common.ValidateVerbArgs(verbSpec.Spec, args, runtimeVerbRegistry{specs: er.compiledUnit.VerbSpecs}); err != nil {
		return nil, fmt.Errorf("argument validation failed: %w", err)
	}

	// Create executor
	executorFactory, exists := er.executors[verbSpec.ExecutorType]
	if !exists {
		return nil, fmt.Errorf("no executor factory for type: %s", verbSpec.ExecutorType)
	}

	executor, err := executorFactory.CreateExecutor(verbSpec.ExecutorConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create executor: %w", err)
	}

	// Execute verb
	result, err := executor.Execute(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("verb execution failed: %w", err)
	}

	if err := common.ValidateVerbReturn(verbSpec.Spec, result, runtimeVerbRegistry{specs: er.compiledUnit.VerbSpecs}); err != nil {
		return nil, fmt.Errorf("return validation failed: %w", err)
	}

	return result, nil
}

// ExecuteWorkflow executes a complete workflow using the execution plan
func (er *ExecutionRuntime) ExecuteWorkflow(ctx context.Context, facts map[string]interface{}) error {
	er.mu.RLock()
	defer er.mu.RUnlock()

	if er.state != StateReady {
		return fmt.Errorf("runtime not ready (state: %s)", er.state)
	}

	if er.compiledUnit.ExecutionPlan == nil {
		return fmt.Errorf("no execution plan available")
	}

	er.state = StateExecuting
	defer func() { er.state = StateReady }()

	// Execute each phase in the execution plan
	for i, phase := range er.compiledUnit.ExecutionPlan.Phases {
		log.Printf("Executing phase %d: %s", i+1, phase.Name)

		if err := er.executePhase(ctx, phase, facts); err != nil {
			return fmt.Errorf("phase %s failed: %w", phase.Name, err)
		}
	}

	return nil
}

// GetRuntimeInfo returns information about the current runtime state
func (er *ExecutionRuntime) GetRuntimeInfo() *RuntimeInfo {
	er.mu.RLock()
	defer er.mu.RUnlock()

	info := &RuntimeInfo{
		State:       er.state,
		LoaderCount: len(er.extensionManager.GetLoaders()),
	}

	if er.compiledUnit != nil {
		info.VerbCount = len(er.compiledUnit.VerbSpecs)
		info.FunctionCount = len(er.compiledUnit.Functions)
		info.Dependencies = er.compiledUnit.Dependencies
		info.Capabilities = er.compiledUnit.Capabilities
	}

	return info
}

// HotReload reloads and recompiles all extensions
func (er *ExecutionRuntime) HotReload(ctx context.Context) error {
	log.Println("Starting hot reload...")

	er.mu.Lock()
	er.state = StateCompiling
	er.mu.Unlock()

	result, err := er.compiler.Compile(ctx, er.extensionManager)
	if err != nil {
		er.mu.Lock()
		er.state = StateReady
		er.mu.Unlock()
		return fmt.Errorf("hot reload failed: %w", err)
	}
	if !result.Success {
		er.mu.Lock()
		er.state = StateReady
		er.mu.Unlock()
		return fmt.Errorf("hot reload failed: %v", result.Errors)
	}

	er.mu.Lock()
	er.compiledUnit = result.CompiledUnit
	er.state = StateReady
	er.mu.Unlock()

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
	State         RuntimeState `json:"state"`
	LoaderCount   int          `json:"loaderCount"`
	VerbCount     int          `json:"verbCount"`
	FunctionCount int          `json:"functionCount"`
	Dependencies  []string     `json:"dependencies"`
	Capabilities  []string     `json:"capabilities"`
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
	client := &http.Client{Timeout: timeout}
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

	conn, err := gef.getConn(grpcConfig)
	if err != nil {
		return nil, err
	}
	return &GRPCExecutor{config: grpcConfig, conn: conn}, nil
}

func (gef *GRPCExecutorFactory) getConn(config *compiler.GRPCExecutorConfig) (*grpc.ClientConn, error) {
	gef.mu.Lock()
	defer gef.mu.Unlock()

	if gef.conns == nil {
		gef.conns = make(map[string]*grpc.ClientConn)
	}

	if conn, ok := gef.conns[config.Address]; ok {
		return conn, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if config.UseTLS {
		opts = []grpc.DialOption{grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{}))}
	}

	conn, err := grpc.DialContext(ctx, config.Address, opts...)
	if err != nil {
		return nil, fmt.Errorf("dial gRPC %s: %w", config.Address, err)
	}

	gef.conns[config.Address] = conn
	return conn, nil
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

		body, _ := io.ReadAll(resp.Body)
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
	config *compiler.GRPCExecutorConfig
	conn   *grpc.ClientConn
}

func (ge *GRPCExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if ge.conn == nil {
		return nil, fmt.Errorf("gRPC connection is nil")
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

	var resp structpb.Struct
	result, err := executeWithRetry(callCtx, ge.config.RetryPolicy, func() (interface{}, error) {
		if err := grpc.Invoke(callCtx, ge.config.Method, req, &resp, ge.conn); err != nil {
			return nil, err
		}
		return resp.AsMap(), nil
	})
	return result, err
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
		body, _ := io.ReadAll(resp.Body)
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
		client := &http.Client{Timeout: timeout}
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
