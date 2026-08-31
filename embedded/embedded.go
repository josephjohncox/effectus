// Package embedded provides a small checked-runtime facade for Go applications.
//
// The default stores are process-local. Use effectusd when executions must
// survive application or host restarts.
package embedded

import (
	"context"
	"fmt"
	"strings"

	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/loader"
	effectusruntime "github.com/josephjohncox/effectus/runtime"
	"github.com/josephjohncox/effectus/schema"
)

// HandlerFunc executes one checked verb with Effectus invocation metadata.
type HandlerFunc func(context.Context, invocation.Request) invocation.Outcome

// Resource declares the capability required for one business resource.
type Resource struct {
	Name         string
	Capabilities []string
}

// Verb declares one in-process business operation.
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

// Request is one embedded execution admission.
type Request struct {
	Namespace      string
	IdempotencyKey string
	Facts          map[string]any
	WaitMode       effectusruntime.WaitMode
}

// Builder collects checked sources, fact declarations, and business verbs.
type Builder struct {
	ruleset string
	version string
	facts   map[string]any
	sources []source
	verbs   []Verb
}

type source struct {
	path string
	data []byte
}

// New creates an embedded runtime builder.
func New(ruleset, version string) *Builder {
	return &Builder{
		ruleset: strings.TrimSpace(ruleset),
		version: strings.TrimSpace(version),
		facts:   make(map[string]any),
	}
}

// AddFact declares a fact path and a representative value for type checking.
func (builder *Builder) AddFact(path string, sample any) *Builder {
	if builder != nil {
		builder.facts[strings.TrimSpace(path)] = sample
	}
	return builder
}

// AddSource adds one in-memory .eff or .effx source file.
func (builder *Builder) AddSource(path string, data []byte) *Builder {
	if builder != nil {
		builder.sources = append(builder.sources, source{
			path: strings.TrimSpace(path),
			data: append([]byte(nil), data...),
		})
	}
	return builder
}

// AddVerb registers one invocation-aware in-process business operation.
func (builder *Builder) AddVerb(verb Verb) *Builder {
	if builder != nil {
		builder.verbs = append(builder.verbs, cloneVerb(verb))
	}
	return builder
}

// Build checks all declarations and publishes one immutable generation.
func (builder *Builder) Build(ctx context.Context) (*Runtime, error) {
	if ctx == nil {
		return nil, fmt.Errorf("embedded build context is nil")
	}
	if builder == nil {
		return nil, fmt.Errorf("embedded builder is nil")
	}
	if builder.ruleset == "" {
		return nil, fmt.Errorf("embedded ruleset is required")
	}
	if builder.version == "" {
		return nil, fmt.Errorf("embedded ruleset version is required")
	}
	if len(builder.sources) == 0 {
		return nil, fmt.Errorf("embedded runtime requires at least one source")
	}
	if len(builder.verbs) == 0 {
		return nil, fmt.Errorf("embedded runtime requires at least one verb")
	}

	runtime := effectusruntime.NewExecutionRuntime()
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = runtime.Close()
		}
	}()
	if err := runtime.ConfigureGenerationMetadata(effectusruntime.GenerationMetadata{
		Ruleset: builder.ruleset,
		Version: builder.version,
	}); err != nil {
		return nil, err
	}

	schemaLoader := loader.NewStaticSchemaLoader(builder.ruleset + "-facts")
	for path, sample := range builder.facts {
		if path == "" {
			return nil, fmt.Errorf("embedded fact path is required")
		}
		schemaLoader.AddData(path, sample)
	}
	runtime.RegisterExtensionLoader(schemaLoader)

	definitions := make([]loader.VerbDefinition, 0, len(builder.verbs))
	seenVerbs := make(map[string]struct{}, len(builder.verbs))
	for _, verb := range builder.verbs {
		if err := validateVerb(verb); err != nil {
			return nil, err
		}
		if _, exists := seenVerbs[verb.Name]; exists {
			return nil, fmt.Errorf("embedded verb %q is registered more than once", verb.Name)
		}
		seenVerbs[verb.Name] = struct{}{}
		definitions = append(definitions, loader.VerbDefinition{
			Spec: staticVerbSpec{verb: verb},
			Executor: &localExecutor{
				handlerID: builder.ruleset + "/" + builder.version + "/" + verb.Name,
				verb:      verb.Name,
				handler:   verb.Handler,
			},
		})
	}
	runtime.RegisterExtensionLoader(loader.NewStaticVerbLoader(builder.ruleset+"-verbs", definitions))
	for index, source := range builder.sources {
		if source.path == "" {
			return nil, fmt.Errorf("embedded source %d path is required", index)
		}
		if len(source.data) == 0 {
			return nil, fmt.Errorf("embedded source %q is empty", source.path)
		}
		runtime.RegisterExtensionLoader(loader.NewStaticSourceLoader(
			fmt.Sprintf("%s-source-%d", builder.ruleset, index), source.path, source.data,
		))
	}
	if err := runtime.CompileAndValidate(ctx); err != nil {
		return nil, fmt.Errorf("compile embedded ruleset: %w", err)
	}
	if err := runtime.ConfigureDurableWorkflowExecution(
		schema.NewInMemoryOutboxStore(), nil,
		schema.DispatcherOptions{Owner: "embedded-" + builder.ruleset},
	); err != nil {
		return nil, fmt.Errorf("configure embedded workflow execution: %w", err)
	}

	closeOnError = false
	return &Runtime{
		runtime: runtime,
		ruleset: builder.ruleset,
		version: builder.version,
	}, nil
}

// Runtime owns one checked embedded generation.
type Runtime struct {
	runtime *effectusruntime.ExecutionRuntime
	ruleset string
	version string
}

// Execute admits or replays one request through runtime.Engine.
func (runtime *Runtime) Execute(ctx context.Context, request Request) (effectusruntime.ExecuteResult, error) {
	if runtime == nil || runtime.runtime == nil {
		return effectusruntime.ExecuteResult{}, fmt.Errorf("embedded runtime is not configured")
	}
	if ctx == nil {
		return effectusruntime.ExecuteResult{}, fmt.Errorf("embedded execute context is nil")
	}
	namespace := strings.TrimSpace(request.Namespace)
	if namespace == "" {
		return effectusruntime.ExecuteResult{}, fmt.Errorf("embedded namespace is required")
	}
	key := strings.TrimSpace(request.IdempotencyKey)
	if key == "" {
		return effectusruntime.ExecuteResult{}, fmt.Errorf("embedded idempotency key is required")
	}
	if request.Facts == nil {
		return effectusruntime.ExecuteResult{}, fmt.Errorf("embedded facts are required")
	}
	executionID := schema.StableExecutionID(namespace, key, runtime.ruleset, runtime.version)
	admissionID := schema.StableAdmissionID(namespace, key, runtime.ruleset, runtime.version)
	return runtime.runtime.Engine().Execute(ctx, effectusruntime.ExecuteRequest{
		Admission: &effectusruntime.Admission{
			ExecutionID:              executionID,
			AdmissionID:              admissionID,
			TenantNamespace:          namespace,
			Ruleset:                  runtime.ruleset,
			Version:                  runtime.version,
			Facts:                    cloneMap(request.Facts),
			ExpectedGenerationDigest: runtime.runtime.Engine().ActiveGenerationDigest(),
		},
		WaitMode: request.WaitMode,
	})
}

// Close releases resources owned by the embedded runtime.
func (runtime *Runtime) Close() error {
	if runtime == nil || runtime.runtime == nil {
		return nil
	}
	return runtime.runtime.Close()
}

// Success returns a successful business outcome.
func Success(result any) invocation.Outcome {
	return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: result}
}

// Retryable returns a known-not-committed retryable outcome.
func Retryable(err error) invocation.Outcome {
	return invocation.Outcome{Class: invocation.OutcomeRetryableKnownNotCommitted, Err: err}
}

// Permanent returns a permanent business failure.
func Permanent(err error) invocation.Outcome {
	return invocation.Outcome{Class: invocation.OutcomePermanentFailure, Err: err}
}

// Unknown returns an outcome whose commit state is not known.
func Unknown(err error) invocation.Outcome {
	return invocation.Outcome{Class: invocation.OutcomeUnknown, Err: err}
}

type staticVerbSpec struct {
	verb Verb
}

func (spec staticVerbSpec) GetName() string        { return spec.verb.Name }
func (spec staticVerbSpec) GetDescription() string { return spec.verb.Description }
func (spec staticVerbSpec) GetCapabilities() []string {
	return append([]string(nil), spec.verb.Capabilities...)
}
func (spec staticVerbSpec) GetArgTypes() map[string]string {
	return cloneStringMap(spec.verb.ArgTypes)
}
func (spec staticVerbSpec) GetRequiredArgs() []string {
	return append([]string(nil), spec.verb.RequiredArgs...)
}
func (spec staticVerbSpec) GetReturnType() string  { return spec.verb.ReturnType }
func (spec staticVerbSpec) GetInverseVerb() string { return spec.verb.InverseVerb }
func (spec staticVerbSpec) GetResources() []loader.ResourceSpec {
	resources := make([]loader.ResourceSpec, 0, len(spec.verb.Resources))
	for _, resource := range spec.verb.Resources {
		resources = append(resources, staticResourceSpec{resource: resource})
	}
	return resources
}

type staticResourceSpec struct {
	resource Resource
}

func (spec staticResourceSpec) GetResource() string { return spec.resource.Name }
func (spec staticResourceSpec) GetCapabilities() []string {
	return append([]string(nil), spec.resource.Capabilities...)
}

type localExecutor struct {
	handlerID string
	verb      string
	handler   HandlerFunc
}

func (executor *localExecutor) Execute(ctx context.Context, args map[string]any) (any, error) {
	outcome := executor.Invoke(ctx, invocation.Request{Verb: executor.verb, Arguments: cloneMap(args)})
	if err := invocation.ValidateOutcome(outcome); err != nil {
		return nil, err
	}
	if outcome.Class != invocation.OutcomeSuccess {
		return nil, outcome.Err
	}
	return outcome.Result, nil
}

func (executor *localExecutor) Invoke(ctx context.Context, request invocation.Request) invocation.Outcome {
	if executor == nil || executor.handler == nil {
		return Permanent(fmt.Errorf("embedded handler is not configured"))
	}
	if request.Verb == "" {
		request.Verb = executor.verb
	}
	outcome := executor.handler(ctx, request)
	if err := invocation.ValidateOutcome(outcome); err != nil {
		return Permanent(fmt.Errorf("embedded handler %q returned an invalid outcome: %w", executor.verb, err))
	}
	return outcome
}

func (executor *localExecutor) InvocationResolverDescriptor() any {
	return map[string]any{"type": "embedded", "handler_id": executor.handlerID}
}

func validateVerb(verb Verb) error {
	verb.Name = strings.TrimSpace(verb.Name)
	if verb.Name == "" {
		return fmt.Errorf("embedded verb name is required")
	}
	if verb.Handler == nil {
		return fmt.Errorf("embedded verb %q handler is required", verb.Name)
	}
	if len(verb.ArgTypes) == 0 {
		return fmt.Errorf("embedded verb %q argument types are required", verb.Name)
	}
	if strings.TrimSpace(verb.ReturnType) == "" {
		return fmt.Errorf("embedded verb %q return type is required", verb.Name)
	}
	return nil
}

func cloneVerb(verb Verb) Verb {
	verb.Name = strings.TrimSpace(verb.Name)
	verb.ArgTypes = cloneStringMap(verb.ArgTypes)
	verb.RequiredArgs = append([]string(nil), verb.RequiredArgs...)
	verb.Capabilities = append([]string(nil), verb.Capabilities...)
	verb.Resources = append([]Resource(nil), verb.Resources...)
	for index := range verb.Resources {
		verb.Resources[index].Capabilities = append([]string(nil), verb.Resources[index].Capabilities...)
	}
	return verb
}

func cloneMap(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func cloneStringMap(input map[string]string) map[string]string {
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
