package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"

	"github.com/josephjohncox/effectus/compiler"
	effectusv1 "github.com/josephjohncox/effectus/gen/effectus/v1"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
	"github.com/josephjohncox/effectus/schema"
	"github.com/josephjohncox/effectus/schema/ledger"
)

// ArtifactResolver reconstructs invocation-aware executor instances from an
// immutable artifact manifest. Callback-only implementations are not valid
// durable resolvers.
type ArtifactResolver interface {
	ResolveArtifact(context.Context, ledger.ExecutionArtifact, *ir.Checked) (*compiler.CompiledUnit, error)
}

type ArtifactResolverFunc func(context.Context, ledger.ExecutionArtifact, *ir.Checked) (*compiler.CompiledUnit, error)

func (function ArtifactResolverFunc) ResolveArtifact(ctx context.Context, artifact ledger.ExecutionArtifact, checked *ir.Checked) (*compiler.CompiledUnit, error) {
	return function(ctx, artifact, checked)
}

func buildDurableAdmission(ctx context.Context, unit *compiler.CompiledUnit, admission *Admission, requestHash string) (schema.DurableAdmission, map[string]struct{}, map[string]any, error) {
	if unit == nil || unit.CheckedIR == nil {
		return schema.DurableAdmission{}, nil, nil, fmt.Errorf("checked compiled unit is required")
	}
	artifact, err := executionArtifactForUnit(unit)
	if err != nil {
		return schema.DurableAdmission{}, nil, nil, err
	}
	effectiveFacts := cloneWorkflowFacts(unit.InitialData)
	mergePolicy := admission.MergePolicy
	if mergePolicy == "" {
		mergePolicy = "merge"
	}
	switch mergePolicy {
	case "merge":
		mergeWorkflowFactOverrides(effectiveFacts, admission.Facts)
	case "replace":
		effectiveFacts = make(map[string]interface{}, len(admission.Facts))
		mergeWorkflowFactOverrides(effectiveFacts, admission.Facts)
	default:
		return schema.DurableAdmission{}, nil, nil, fmt.Errorf("unsupported fact merge policy %q", mergePolicy)
	}
	if err := validateAdmissionFactTypes(unit.IREnvironment, effectiveFacts); err != nil {
		return schema.DurableAdmission{}, nil, nil, err
	}
	factsJSON, _, err := schema.CanonicalJSON(effectiveFacts)
	if err != nil {
		return schema.DurableAdmission{}, nil, nil, err
	}
	inputFactsJSON, _, err := schema.CanonicalJSON(admission.Facts)
	if err != nil {
		return schema.DurableAdmission{}, nil, nil, err
	}
	identity := admission.AdmissionID
	if identity == "" {
		identity = admission.ExecutionID
	}
	record := schema.ExecutionRecord{
		ExecutionID: admission.ExecutionID, AdmissionIdentity: identity, RequestHash: requestHash,
		Ruleset: admission.Ruleset, Version: admission.Version, TenantNamespace: admission.TenantNamespace,
		MergePolicy: mergePolicy, GenerationDigest: artifact.GenerationDigest, EffectiveFacts: factsJSON,
	}
	request := schema.DurableAdmission{
		Artifact: artifact, Execution: record,
		FactApplication: schema.FactApplication{ExecutionID: admission.ExecutionID, FactEventID: identity, MergePolicy: mergePolicy, Facts: inputFactsJSON, AppliedRevision: 1},
	}
	selected := make(map[string]struct{})
	for _, plan := range unit.CheckedIR.CloneArtifact().Plans {
		if err := ctx.Err(); err != nil {
			return schema.DurableAdmission{}, nil, nil, err
		}
		matches, err := evaluateCheckedPredicate(plan.Predicate.Expression, effectiveFacts, unit)
		if err != nil {
			return schema.DurableAdmission{}, nil, nil, fmt.Errorf("evaluate plan %q: %w", plan.Id, err)
		}
		if !matches {
			continue
		}
		selected[plan.Id] = struct{}{}
		sagaID := schema.StableSagaID(admission.ExecutionID, plan.Id)
		request.Plans = append(request.Plans, schema.ExecutionPlanRecord{ExecutionID: admission.ExecutionID, PlanID: plan.Id, SagaID: sagaID, Ordinal: len(request.Plans), State: "selected"})
		request.Sagas = append(request.Sagas, schema.CreateSagaRequest{Namespace: admission.TenantNamespace, SagaID: sagaID, ExecutionID: admission.ExecutionID, PlanID: plan.Id, PlanDigest: unit.CheckedIR.Digest(), Serial: true})
		if len(plan.Steps) != 0 {
			stepRequest, err := durableInitialStep(plan, effectiveFacts, sagaID)
			if err != nil {
				return schema.DurableAdmission{}, nil, nil, err
			}
			request.InitialSteps = append(request.InitialSteps, stepRequest)
		}
	}
	return request, selected, effectiveFacts, nil
}

func validateAdmissionFactTypes(environment ir.Environment, facts map[string]any) error {
	for path, typeName := range environment.Facts {
		value, ok := lookupWorkflowFact(facts, path)
		if !ok {
			continue
		}
		if err := validateAdmissionValue(environment, typeName, value); err != nil {
			return fmt.Errorf("fact %q: %w", path, err)
		}
	}
	return nil
}
func validateAdmissionValue(environment ir.Environment, typeName string, value any) error {
	normalized := strings.ToLower(strings.TrimSpace(typeName))
	valid := false
	switch normalized {
	case "string":
		_, valid = value.(string)
	case "bool", "boolean":
		_, valid = value.(bool)
	case "int", "integer":
		switch typed := value.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			valid = true
		case json.Number:
			_, err := typed.Int64()
			valid = err == nil
		case float64:
			valid = typed == math.Trunc(typed)
		}
	case "float", "double", "number":
		switch value.(type) {
		case float32, float64, int, int32, int64, json.Number:
			valid = true
		}
	case "bytes":
		_, valid = value.([]byte)
	case "any", "unknown":
		return fmt.Errorf("open type %q is not accepted at durable admission", typeName)
	default:
		definition, ok := environment.Types[typeName]
		if !ok {
			return fmt.Errorf("unknown checked type %q", typeName)
		}
		if definition.Kind == ir.TypeKindObject {
			object, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("got %T, want object %s", value, typeName)
			}
			for _, required := range definition.RequiredFields {
				if _, exists := object[required]; !exists {
					return fmt.Errorf("missing required field %q", required)
				}
			}
			for field, item := range object {
				fieldType, exists := definition.Fields[field]
				if !exists {
					return fmt.Errorf("unknown field %q", field)
				}
				if err := validateAdmissionValue(environment, fieldType, item); err != nil {
					return fmt.Errorf("field %q: %w", field, err)
				}
			}
			return nil
		}
	}
	if !valid {
		return fmt.Errorf("got %T, want %s", value, typeName)
	}
	return nil
}

func durableInitialStep(plan *effectusv1.Plan, facts map[string]any, sagaID string) (schema.EnqueueStepRequest, error) {
	step := plan.Steps[0]
	arguments := make(map[string]any, len(step.Arguments))
	for _, argument := range step.Arguments {
		value, err := resolveCheckedWorkflowValue(argument.Value, facts, nil)
		if err != nil {
			return schema.EnqueueStepRequest{}, fmt.Errorf("resolve plan %q first step argument %q: %w", plan.Id, argument.Name, err)
		}
		arguments[argument.Name] = value
	}
	request := schema.EnqueueStepRequest{SagaID: sagaID, EffectID: step.Id, Sequence: 1, Verb: step.Verb, ContractHash: step.ContractHash, Arguments: arguments}
	if step.Compensation != nil {
		request.CompensationVerb = step.Compensation.InverseVerb
		request.CompensationContract = step.Compensation.InverseContractHash
		request.CompensationArguments = arguments
	}
	if step.FencingRequirement == effectusv1.FencingRequirement_FENCING_REQUIREMENT_REQUIRED {
		request.Fencing = []schema.FencingRequirement{{Authority: "checked-contract", Resource: step.Verb}}
	}
	return request, nil
}

func executionArtifactForUnit(unit *compiler.CompiledUnit) (schema.ExecutionArtifact, error) {
	environment, err := json.Marshal(unit.IREnvironment)
	if err != nil {
		return schema.ExecutionArtifact{}, err
	}
	type executorEntry struct {
		Name, Type, ConfigType, ImplementationType string
		ResolverDescriptor                         any `json:"resolver_descriptor,omitempty"`
	}
	executors := make([]executorEntry, 0, len(unit.VerbSpecs))
	names := make([]string, 0, len(unit.VerbSpecs))
	for name := range unit.VerbSpecs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		verb := unit.VerbSpecs[name]
		entry := executorEntry{Name: name}
		if verb != nil {
			entry.Type = string(verb.ExecutorType)
			entry.ConfigType = fmt.Sprintf("%T", verb.ExecutorConfig)
			if local, ok := verb.ExecutorConfig.(*compiler.LocalExecutorConfig); ok && local != nil && local.Implementation != nil {
				entry.ImplementationType = reflect.TypeOf(local.Implementation).String()
				if provider, ok := any(local.Implementation).(invocation.ResolverDescriptorProvider); ok {
					entry.ResolverDescriptor = provider.InvocationResolverDescriptor()
				}
			}
		}
		executors = append(executors, entry)
	}
	executorManifest, err := json.Marshal(executors)
	if err != nil {
		return schema.ExecutionArtifact{}, err
	}
	functionNames := make([]string, 0, len(unit.Functions))
	for name := range unit.Functions {
		functionNames = append(functionNames, name)
	}
	sort.Strings(functionNames)
	functions := make(map[string]any, len(functionNames))
	for _, name := range functionNames {
		compiled := unit.Functions[name]
		if compiled == nil {
			continue
		}
		if compiled.ResolverDescriptor == nil && compiled.Implementation != nil {
			return schema.ExecutionArtifact{}, fmt.Errorf("function %q has no immutable resolver descriptor", name)
		}
		functions[name] = map[string]any{"implementation_type": fmt.Sprintf("%T", compiled.Implementation), "resolver_descriptor": compiled.ResolverDescriptor}
	}
	initialData, err := json.Marshal(unit.InitialData)
	if err != nil {
		return schema.ExecutionArtifact{}, fmt.Errorf("marshal immutable initial data: %w", err)
	}
	functionManifest, err := json.Marshal(struct {
		Functions   map[string]any  `json:"functions"`
		InitialData json.RawMessage `json:"initial_data"`
	}{Functions: functions, InitialData: initialData})
	if err != nil {
		return schema.ExecutionArtifact{}, err
	}
	compilerMetadata, err := json.Marshal(unit.CheckedIR.CloneArtifact().Compiler)
	if err != nil {
		return schema.ExecutionArtifact{}, err
	}
	sourceDigest := unit.CheckedIR.Digest()
	manifest := struct {
		IRDigest, EnvironmentDigest, SourceDigest string
		Executors                                 json.RawMessage
		Functions                                 json.RawMessage
		InitialData                               json.RawMessage
	}{
		IRDigest: unit.CheckedIR.Digest(), SourceDigest: sourceDigest, Executors: executorManifest, Functions: functionManifest, InitialData: initialData,
	}
	manifest.EnvironmentDigest, err = ir.EnvironmentDigest(unit.IREnvironment)
	if err != nil {
		return schema.ExecutionArtifact{}, err
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return schema.ExecutionArtifact{}, err
	}
	digest := sha256.Sum256(manifestJSON)
	return schema.ExecutionArtifact{
		GenerationDigest: hex.EncodeToString(digest[:]), IRDigest: unit.CheckedIR.Digest(), IRBytes: unit.CheckedIR.Marshal(),
		Environment: environment, ExecutorManifest: executorManifest, FunctionManifest: functionManifest,
		SourceDigest: sourceDigest, CompilerMetadata: compilerMetadata,
	}, nil
}

func decodeArtifactEnvironment(artifact schema.ExecutionArtifact) (ir.Environment, error) {
	var environment ir.Environment
	if err := json.Unmarshal(artifact.Environment, &environment); err != nil {
		return ir.Environment{}, fmt.Errorf("decode artifact environment: %w", err)
	}
	return environment, nil
}
