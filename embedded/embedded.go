// Package embedded executes one immutable source bundle in-process.
package embedded

import (
	"context"
	"fmt"
	"strings"

	"github.com/josephjohncox/effectus/bundle"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/runtime"
	"github.com/josephjohncox/effectus/schema"
)

// Runtime owns one immutable generation and an Engine. Embedded execution is
// intentionally descriptor-based; Go callback executors are not supported.
type Runtime struct {
	engine           *runtime.Engine
	ruleset, version string
}

// Request identifies one logical operation. Keep Namespace and IdempotencyKey
// stable for retries. Facts must remain unchanged while Execute reads them.
// An empty WaitMode uses terminal waiting.
type Request struct {
	Namespace      string
	IdempotencyKey string
	Facts          map[string]any
	WaitMode       runtime.WaitMode
}

// Open compiles and resolves a source bundle once. Executable identity comes from the bundle.
// The runtime owns resources returned by resolvers, not the registry's resolver implementations.
// Its default ledger and outbox are in memory. It does not start a recovery worker.
func Open(ctx context.Context, source *bundle.SourceBundle, resolvers *invocation.Registry) (*Runtime, error) {
	if ctx == nil || source == nil || resolvers == nil {
		return nil, fmt.Errorf("embedded source bundle, resolver registry, and context are required")
	}
	generation, err := runtime.CompileGeneration(ctx, runtime.GenerationBuildConfig{Bundle: source, Resolvers: resolvers, Production: true})
	if err != nil {
		return nil, err
	}
	engine, err := runtime.NewEngine(generation)
	if err != nil {
		_ = generation.Close()
		return nil, err
	}
	if err := engine.ConfigureWorkflow(schema.NewInMemoryOutboxStore(), nil, schema.DispatcherOptions{Owner: "embedded-" + source.Name()}); err != nil {
		_ = engine.Close()
		return nil, err
	}
	return &Runtime{engine: engine, ruleset: source.Name(), version: source.Version()}, nil
}

// Execute uses the runtime's active generation and a non-nil context.
// It permits concurrent calls. Accepted-only results do not imply business completion.
// Terminal failures remain visible on replay through runtime.TerminalExecutionError.
func (r *Runtime) Execute(ctx context.Context, request Request) (runtime.ExecuteResult, error) {
	if r == nil || r.engine == nil {
		return runtime.ExecuteResult{}, fmt.Errorf("embedded runtime is not configured")
	}
	namespace := strings.TrimSpace(request.Namespace)
	key := strings.TrimSpace(request.IdempotencyKey)
	if namespace == "" || key == "" || request.Facts == nil {
		return runtime.ExecuteResult{}, fmt.Errorf("embedded namespace, idempotency key, and facts are required")
	}
	return r.engine.Execute(ctx, runtime.ExecuteRequest{Admission: &runtime.Admission{ExecutionID: schema.StableExecutionID(namespace, key, r.ruleset, r.version), AdmissionID: schema.StableAdmissionID(namespace, key, r.ruleset, r.version), TenantNamespace: namespace, Ruleset: r.ruleset, Version: r.version, Facts: request.Facts, ExpectedGenerationDigest: r.engine.ActiveGenerationDigest()}, WaitMode: request.WaitMode})
}

// Close drains entered calls and releases the owned engine and generation.
// Stop new callers first. Do not call Close from an executor using this runtime.
func (r *Runtime) Close() error {
	if r == nil {
		return nil
	}
	return r.engine.Close()
}

// Engine returns the borrowed engine. Configure it before the first execution.
// The runtime retains ownership. Use Runtime.Close to end its lifetime.
func (r *Runtime) Engine() *runtime.Engine {
	if r == nil {
		return nil
	}
	return r.engine
}
