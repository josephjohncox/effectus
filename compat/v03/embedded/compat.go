// Package embedded preserves the v0.3 embedded vocabulary during migration.
package embedded

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/josephjohncox/effectus/bundle"
	"github.com/josephjohncox/effectus/compat/v03/invocation"
	current "github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
	"github.com/josephjohncox/effectus/runtime"
	"github.com/josephjohncox/effectus/schema"
)

// HandlerFunc is the frozen v0.3 callback shape.
type HandlerFunc func(context.Context, invocation.Request) invocation.Outcome

type Resource struct {
	Name         string
	Capabilities []string
}
type Verb struct {
	Name         string
	Description  string
	ArgTypes     map[string]string
	RequiredArgs []string
	ReturnType   string
	InverseVerb  string
	Capabilities []string
	Resources    []Resource
	Handler      HandlerFunc
}
type Request struct {
	Namespace      string
	IdempotencyKey string
	Facts          map[string]any
	WaitMode       runtime.WaitMode
}

type Builder struct {
	name, version string
	facts         map[string]string
	sources       []bundle.Source
	verbs         []Verb
}

func New(name, version string) *Builder {
	return &Builder{name: name, version: version, facts: map[string]string{}}
}
func (b *Builder) AddFact(name string, value any) *Builder {
	if b != nil && strings.TrimSpace(name) != "" {
		b.facts[name] = legacyType(value)
	}
	return b
}
func legacyType(value any) string {
	switch value.(type) {
	case bool:
		return "bool"
	case int, int8, int16, int32, int64:
		return "int"
	case float32, float64:
		return "float"
	default:
		return "string"
	}
}
func (b *Builder) AddSource(path string, source []byte) *Builder {
	if b != nil {
		b.sources = append(b.sources, bundle.Source{Path: path, Content: string(source)})
	}
	return b
}
func (b *Builder) AddVerb(verb Verb) *Builder {
	if b != nil {
		b.verbs = append(b.verbs, verb)
	}
	return b
}

// Build adapts v0.3 callbacks inside this compatibility package. The result is
// an immutable checked generation; callbacks are never exposed to current APIs.
func (b *Builder) Build(ctx context.Context) (*Runtime, error) {
	if b == nil || ctx == nil || strings.TrimSpace(b.name) == "" || strings.TrimSpace(b.version) == "" {
		return nil, fmt.Errorf("compat/v03 embedded name, version, and context are required")
	}
	contracts := make(map[string]ir.VerbContract, len(b.verbs))
	descriptors := make(map[string]current.Descriptor, len(b.verbs))
	handlers := make(map[string]HandlerFunc, len(b.verbs))
	for _, verb := range b.verbs {
		if strings.TrimSpace(verb.Name) == "" || verb.Handler == nil {
			return nil, fmt.Errorf("compat/v03 embedded verb name and handler are required")
		}
		descriptor, err := current.NewDescriptor(current.DescriptorSpec{Type: current.DescriptorEmbedded, ResolverID: "compat/v03/callback", Reference: verb.Name})
		if err != nil {
			return nil, err
		}
		contracts[verb.Name] = ir.VerbContract{Arguments: verb.ArgTypes, RequiredArgs: verb.RequiredArgs, ResultType: verb.ReturnType}
		descriptors[verb.Name] = descriptor
		handlers[verb.Name] = verb.Handler
	}
	source, err := bundle.New(bundle.Spec{Name: b.name, Version: b.version, Sources: b.sources, Environment: ir.Environment{Facts: b.facts, Verbs: contracts}, Executors: descriptors})
	if err != nil {
		return nil, err
	}
	resolvers, err := current.NewRegistry([]current.ResolverRegistration{{ID: "compat/v03/callback", Resolver: current.ResolverFunc(func(_ context.Context, descriptor current.Descriptor) (current.Executor, io.Closer, error) {
		handler := handlers[descriptor.Reference()]
		if handler == nil {
			return nil, nil, fmt.Errorf("compat/v03 callback %q is unavailable", descriptor.Reference())
		}
		return callbackExecutor{handler}, nil, nil
	})}})
	if err != nil {
		return nil, err
	}
	generation, err := runtime.CompileGeneration(ctx, runtime.GenerationBuildConfig{Bundle: source, Resolvers: resolvers})
	if err != nil {
		return nil, err
	}
	engine, err := runtime.NewEngine(generation)
	if err != nil {
		_ = generation.Close()
		return nil, err
	}
	if err := engine.ConfigureWorkflow(schema.NewInMemoryOutboxStore(), nil, schema.DispatcherOptions{Owner: "compat-v03-" + b.name}); err != nil {
		_ = engine.Close()
		return nil, err
	}
	return &Runtime{engine: engine, ruleset: b.name, version: b.version}, nil
}

type callbackExecutor struct{ handler HandlerFunc }

func (e callbackExecutor) Invoke(ctx context.Context, request current.Request) current.Outcome {
	return e.handler(ctx, request)
}

type Runtime struct {
	engine           *runtime.Engine
	ruleset, version string
}

func (r *Runtime) Execute(ctx context.Context, request Request) (runtime.ExecuteResult, error) {
	if r == nil || r.engine == nil || strings.TrimSpace(request.Namespace) == "" || strings.TrimSpace(request.IdempotencyKey) == "" || request.Facts == nil {
		return runtime.ExecuteResult{}, fmt.Errorf("compat/v03 embedded namespace, idempotency key, and facts are required")
	}
	return r.engine.Execute(ctx, runtime.ExecuteRequest{Admission: &runtime.Admission{ExecutionID: schema.StableExecutionID(request.Namespace, request.IdempotencyKey, r.ruleset, r.version), AdmissionID: schema.StableAdmissionID(request.Namespace, request.IdempotencyKey, r.ruleset, r.version), TenantNamespace: request.Namespace, Ruleset: r.ruleset, Version: r.version, Facts: request.Facts, ExpectedGenerationDigest: r.engine.ActiveGenerationDigest()}, WaitMode: request.WaitMode})
}
func (r *Runtime) Close() error {
	if r == nil {
		return nil
	}
	return r.engine.Close()
}
func Success(result any) invocation.Outcome {
	return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: result}
}
func Retryable(err error) invocation.Outcome {
	return invocation.Outcome{Class: invocation.OutcomeRetryableKnownNotCommitted, Err: err}
}
func Permanent(err error) invocation.Outcome {
	return invocation.Outcome{Class: invocation.OutcomePermanentFailure, Err: err}
}
func Unknown(err error) invocation.Outcome {
	return invocation.Outcome{Class: invocation.OutcomeUnknown, Err: err}
}
