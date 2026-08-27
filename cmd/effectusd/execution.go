package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/effectus/effectus-go"
	"github.com/effectus/effectus-go/common"
	"github.com/effectus/effectus-go/compiler"
	"github.com/effectus/effectus-go/pathutil"
	"github.com/effectus/effectus-go/schema"
	"github.com/effectus/effectus-go/schema/capability"
	"github.com/effectus/effectus-go/schema/types"
	"github.com/effectus/effectus-go/schema/verb"
	"github.com/effectus/effectus-go/unified"
)

const requestIDContextKey = "request_id"

type typeSystemSchema struct {
	ts *types.TypeSystem
}

func (s *typeSystemSchema) ValidatePath(path string) bool {
	if s == nil || s.ts == nil || strings.TrimSpace(path) == "" {
		return false
	}
	_, err := s.ts.GetFactType(path)
	return err == nil
}

type runtimeFacts struct {
	data     map[string]interface{}
	provider *pathutil.RegistryFactProvider
	schema   effectus.SchemaInfo
}

func newRuntimeFacts(data map[string]interface{}, schema effectus.SchemaInfo) *runtimeFacts {
	if data == nil {
		data = map[string]interface{}{}
	}
	return &runtimeFacts{
		data:     data,
		provider: pathutil.NewRegistryFactProviderFromMap(data),
		schema:   schema,
	}
}

func (f *runtimeFacts) Get(path string) (interface{}, bool) {
	if f == nil {
		return nil, false
	}
	if path == "" {
		if f.data == nil {
			return nil, false
		}
		return f.data, true
	}
	if f.provider == nil {
		return nil, false
	}
	return f.provider.Get(path)
}

func (f *runtimeFacts) Schema() effectus.SchemaInfo {
	if f == nil {
		return nil
	}
	return f.schema
}

type commonFactsAdapter struct {
	facts effectus.Facts
}

func (fa *commonFactsAdapter) Get(path string) (interface{}, bool) {
	if fa == nil || fa.facts == nil {
		return nil, false
	}
	return fa.facts.Get(path)
}

func (fa *commonFactsAdapter) GetWithContext(path string) (interface{}, *common.ResolutionResult) {
	value, exists := fa.Get(path)
	return value, &common.ResolutionResult{
		Exists: exists,
		Path:   path,
		Value:  value,
	}
}

func (fa *commonFactsAdapter) HasPath(path string) bool {
	_, ok := fa.Get(path)
	return ok
}

func (fa *commonFactsAdapter) Schema() common.SchemaInfo {
	if fa == nil || fa.facts == nil {
		return nil
	}
	return &commonSchemaAdapter{schema: fa.facts.Schema()}
}

type commonSchemaAdapter struct {
	schema effectus.SchemaInfo
}

func (sa *commonSchemaAdapter) ValidatePath(path string) bool {
	if sa == nil || sa.schema == nil {
		return false
	}
	return sa.schema.ValidatePath(path)
}

func (sa *commonSchemaAdapter) GetPathType(path string) *types.Type {
	if sa == nil || sa.schema == nil {
		return nil
	}
	if sa.schema.ValidatePath(path) {
		return &types.Type{Name: "unknown"}
	}
	return nil
}

func (sa *commonSchemaAdapter) RegisterPathType(string, *types.Type) {}

func compileBundleRules(bundle *unified.Bundle, baseTS *types.TypeSystem, verbReg *verb.Registry, verbose bool) (*unified.Bundle, error) {
	if bundle == nil {
		return nil, fmt.Errorf("bundle not loaded")
	}
	if len(bundle.RuleSources) == 0 {
		if verbose {
			fmt.Println("Bundle has no embedded rule sources; skipping runtime compilation")
		}
		return bundle, nil
	}

	ts := buildHotloadTypeSystem(baseTS, bundle, verbReg)
	facts := newHotloadFacts(ts)

	comp := compiler.NewCompiler()
	compTS := comp.GetTypeSystem()
	if compTS != nil {
		compTS.MergeTypeSystem(ts)
	}

	prepared, cleanup, err := prepareRuleSources(bundle.RuleSources)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	spec, err := comp.ParseAndCompileProgram(collectTempPaths(prepared), facts)
	if err != nil {
		return nil, err
	}

	next := *bundle
	next.ListSpec = spec.ListSpec()
	next.FlowSpec = spec.FlowSpec()
	next.Rules = unified.SummarizeRules(next.ListSpec)
	next.Flows = unified.SummarizeFlows(next.FlowSpec)
	next.RequiredFacts = spec.RequiredFacts()

	return &next, nil
}

func configuredBundleCopy(bundle *unified.Bundle, verbReg *verb.Registry, sagaEnabled bool, sagaStore schema.SagaStore, capSystem *capability.CapabilitySystem) *unified.Bundle {
	if bundle == nil {
		return nil
	}
	next := *bundle
	if bundle.ListSpec != nil {
		listSpec := *bundle.ListSpec
		next.ListSpec = &listSpec
	}
	if bundle.FlowSpec != nil {
		flowSpec := *bundle.FlowSpec
		next.FlowSpec = &flowSpec
	}
	configureBundleExecution(&next, verbReg, sagaEnabled, sagaStore, capSystem)
	return &next
}

func configureBundleExecution(bundle *unified.Bundle, verbReg *verb.Registry, sagaEnabled bool, sagaStore schema.SagaStore, capSystem *capability.CapabilitySystem) {
	if bundle == nil {
		return
	}
	if bundle.ListSpec != nil {
		bundle.ListSpec.VerbRegistry = verbReg
		bundle.ListSpec.SagaEnabled = sagaEnabled
		bundle.ListSpec.SagaStore = sagaStore
		bundle.ListSpec.CapSystem = capSystem
	}
	if bundle.FlowSpec != nil {
		bundle.FlowSpec.VerbRegistry = verbReg
		bundle.FlowSpec.SagaEnabled = sagaEnabled
		bundle.FlowSpec.SagaStore = sagaStore
		bundle.FlowSpec.CapSystem = capSystem
	}
}

func (s *serverState) ExecuteFacts(ctx context.Context, env factEnvelope) error {
	if s == nil {
		return fmt.Errorf("runtime not initialized")
	}
	return s.executeFactsOnGeneration(ctx, env, s.generationSnapshot())
}

func (s *serverState) executeFactsOnGeneration(ctx context.Context, env factEnvelope, generation *runtimeGeneration) error {
	if generation == nil {
		return fmt.Errorf("runtime generation is required")
	}
	if env.Universe == "" {
		env.Universe = "default"
	}

	bundle := generation.bundle
	executionTypes := generation.execTypes
	verbRegistry := generation.verbs
	if bundle == nil {
		return fmt.Errorf("bundle not loaded")
	}

	factsData := env.Facts
	if s.factStore != nil {
		if snapshot, ok := s.factStore.Snapshot(env.Universe); ok {
			factsData = snapshot
		}
	}
	if len(factsData) == 0 {
		return fmt.Errorf("facts are required")
	}

	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Value(requestIDContextKey) == nil {
		requestID := env.ExecutionID
		if requestID == "" {
			requestID = fmt.Sprintf("%s-%d", env.Universe, time.Now().UnixNano())
		}
		ctx = context.WithValue(ctx, requestIDContextKey, requestID)
	}

	if executionTypes == nil {
		return fmt.Errorf("runtime type system not initialized")
	}

	facts := newRuntimeFacts(factsData, &typeSystemSchema{ts: executionTypes})
	var executor effectus.Executor
	if verbRegistry != nil {
		executor = common.NewExecutorAdapter(verbRegistry, &commonFactsAdapter{facts: facts})
	}

	if bundle.ListSpec != nil {
		recordListExecution()
		if err := bundle.ListSpec.Execute(ctx, facts, executor); err != nil {
			recordExecutionFailure()
			return err
		}
	}
	if bundle.FlowSpec != nil {
		recordFlowExecution()
		if err := bundle.FlowSpec.Execute(ctx, facts, executor); err != nil {
			recordExecutionFailure()
			return err
		}
	}

	return nil
}

type instrumentedExecutor struct {
	name  string
	inner verb.Executor
}

func (ie *instrumentedExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	recordVerbExecution()
	if ie.inner == nil {
		recordVerbFailure()
		return nil, fmt.Errorf("verb executor missing for %s", ie.name)
	}
	result, err := ie.inner.Execute(ctx, args)
	if err != nil {
		recordVerbFailure()
	}
	return result, err
}

func instrumentVerbRegistry(reg *verb.Registry) {
	if reg == nil {
		return
	}
	for _, spec := range reg.GetAllVerbs() {
		if spec == nil || spec.Executor == nil {
			continue
		}
		if _, ok := spec.Executor.(*instrumentedExecutor); ok {
			continue
		}
		spec.Executor = &instrumentedExecutor{name: spec.Name, inner: spec.Executor}
	}
}

type warmupExecutor interface {
	Warmup(ctx context.Context) error
}

func unwrapExecutor(exec verb.Executor) verb.Executor {
	if wrapped, ok := exec.(*instrumentedExecutor); ok {
		return wrapped.inner
	}
	return exec
}

func setVerbSources(reg *verb.Registry) {
	if reg == nil {
		return
	}
	for _, spec := range reg.GetAllVerbs() {
		if spec == nil {
			continue
		}
		if info, ok := reg.GetVerbSource(spec.Name); ok && info.Type != "" && info.Type != verb.SourceUnknown {
			continue
		}
		reg.SetVerbSource(spec.Name, inferSourceFromExecutor(spec.Executor))
	}
}

func inferSourceFromExecutor(exec verb.Executor) verb.SourceInfo {
	if exec == nil {
		return verb.SourceInfo{Type: verb.SourceUnknown}
	}
	exec = unwrapExecutor(exec)
	if exec == nil {
		return verb.SourceInfo{Type: verb.SourceUnknown}
	}
	if provider, ok := exec.(verb.SourceProvider); ok {
		info := provider.SourceInfo()
		if info.Type != "" {
			return info
		}
	}
	return verb.SourceInfo{Type: verb.SourceInternal}
}

func warmupOCIExecutors(ctx context.Context, reg *verb.Registry) error {
	if reg == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var failures []string
	for _, spec := range reg.GetAllVerbs() {
		if spec == nil || spec.Executor == nil {
			continue
		}
		exec := unwrapExecutor(spec.Executor)
		provider, ok := exec.(verb.SourceProvider)
		if !ok {
			continue
		}
		info := provider.SourceInfo()
		if info.Type != verb.SourceOCI {
			continue
		}
		warm, ok := exec.(warmupExecutor)
		if !ok {
			continue
		}
		if err := warm.Warmup(ctx); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", spec.Name, err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("oci warmup failed: %s", strings.Join(failures, "; "))
	}
	return nil
}
