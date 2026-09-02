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
type Request struct {
	Namespace      string
	IdempotencyKey string
	Facts          map[string]any
	WaitMode       runtime.WaitMode
}

// Open compiles and resolves a source bundle once. The supplied registry owns
// only resolver implementations; all executable identity remains in bundle.
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
func (r *Runtime) Close() error {
	if r == nil {
		return nil
	}
	return r.engine.Close()
}
func (r *Runtime) Engine() *runtime.Engine {
	if r == nil {
		return nil
	}
	return r.engine
}
