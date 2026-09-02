package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	effectusv1 "github.com/josephjohncox/effectus/gen/effectus/v1"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
	"github.com/josephjohncox/effectus/schema"
	"github.com/josephjohncox/effectus/schema/ledger"
)

// ArtifactResolver reconstructs a callback-free Generation from one immutable
// durable artifact. It intentionally has no compiler or loader surface.
type ArtifactResolver interface {
	ResolveGeneration(context.Context, ledger.ExecutionArtifact) (*Generation, error)
}
type ArtifactResolverFunc func(context.Context, ledger.ExecutionArtifact) (*Generation, error)

func (f ArtifactResolverFunc) ResolveGeneration(ctx context.Context, artifact ledger.ExecutionArtifact) (*Generation, error) {
	return f(ctx, artifact)
}

func buildDurableAdmission(ctx context.Context, generation *Generation, admission *Admission, requestHash string) (schema.DurableAdmission, map[string]struct{}, map[string]any, error) {
	if generation == nil || generation.Checked() == nil {
		return schema.DurableAdmission{}, nil, nil, fmt.Errorf("checked generation is required")
	}
	artifact, err := executionArtifactForGeneration(generation)
	if err != nil {
		return schema.DurableAdmission{}, nil, nil, err
	}
	effectiveFacts := make(map[string]any)
	mergePolicy := admission.MergePolicy
	if mergePolicy == "" {
		mergePolicy = "merge"
	}
	switch mergePolicy {
	case "merge", "replace":
		mergeWorkflowFactOverrides(effectiveFacts, admission.Facts)
	default:
		return schema.DurableAdmission{}, nil, nil, fmt.Errorf("unsupported fact merge policy %q", mergePolicy)
	}
	if err := validateAdmissionFactTypes(generation.Environment(), effectiveFacts); err != nil {
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
	record := schema.ExecutionRecord{ExecutionID: admission.ExecutionID, AdmissionIdentity: identity, RequestHash: requestHash, Ruleset: admission.Ruleset, Version: admission.Version, TenantNamespace: admission.TenantNamespace, MergePolicy: mergePolicy, GenerationDigest: artifact.GenerationDigest, EffectiveFacts: factsJSON}
	request := schema.DurableAdmission{Artifact: artifact, Execution: record, FactApplication: schema.FactApplication{ExecutionID: admission.ExecutionID, FactEventID: identity, MergePolicy: mergePolicy, Facts: inputFactsJSON, AppliedRevision: 1}}
	selected := map[string]struct{}{}
	for _, plan := range generation.Checked().CloneArtifact().Plans {
		if err := ctx.Err(); err != nil {
			return schema.DurableAdmission{}, nil, nil, err
		}
		matches, err := evaluateCheckedPredicate(plan.Predicate.Expression, effectiveFacts, generation)
		if err != nil {
			return schema.DurableAdmission{}, nil, nil, fmt.Errorf("evaluate plan %q: %w", plan.Id, err)
		}
		if !matches {
			continue
		}
		selected[plan.Id] = struct{}{}
		sagaID := schema.StableSagaID(admission.ExecutionID, plan.Id)
		request.Plans = append(request.Plans, schema.ExecutionPlanRecord{ExecutionID: admission.ExecutionID, PlanID: plan.Id, SagaID: sagaID, Ordinal: len(request.Plans), State: "selected"})
		request.Sagas = append(request.Sagas, schema.CreateSagaRequest{Namespace: admission.TenantNamespace, SagaID: sagaID, ExecutionID: admission.ExecutionID, PlanID: plan.Id, PlanDigest: generation.Checked().Digest(), Serial: true})
		if len(plan.Steps) > 0 {
			step, err := durableInitialStep(plan, effectiveFacts, sagaID)
			if err != nil {
				return schema.DurableAdmission{}, nil, nil, err
			}
			request.InitialSteps = append(request.InitialSteps, step)
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

func executionArtifactForGeneration(generation *Generation) (schema.ExecutionArtifact, error) {
	environment, err := json.Marshal(generation.Environment())
	if err != nil {
		return schema.ExecutionArtifact{}, err
	}
	type executorEntry struct {
		Name       string                `json:"name"`
		Descriptor invocation.Descriptor `json:"descriptor"`
	}
	descriptors := generation.ExecutorDescriptors()
	names := make([]string, 0, len(descriptors))
	for name := range descriptors {
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([]executorEntry, 0, len(names))
	for _, name := range names {
		entries = append(entries, executorEntry{Name: name, Descriptor: descriptors[name]})
	}
	executorManifest, err := json.Marshal(entries)
	if err != nil {
		return schema.ExecutionArtifact{}, err
	}
	compilerMetadata, err := json.Marshal(generation.Checked().CloneArtifact().Compiler)
	if err != nil {
		return schema.ExecutionArtifact{}, err
	}
	identity, err := json.Marshal(struct {
		Ruleset     string            `json:"ruleset"`
		Version     string            `json:"version"`
		FunctionIDs map[string]string `json:"function_ids"`
	}{generation.Ruleset(), generation.Version(), generation.FunctionIDs()})
	if err != nil {
		return schema.ExecutionArtifact{}, err
	}
	return schema.ExecutionArtifact{GenerationDigest: generation.Digest(), IRDigest: generation.Checked().Digest(), IRBytes: generation.Checked().Marshal(), Environment: environment, ExecutorManifest: executorManifest, FunctionManifest: identity, SourceDigest: generation.SourceDigest(), CompilerMetadata: compilerMetadata}, nil
}

func decodeArtifactEnvironment(artifact schema.ExecutionArtifact) (ir.Environment, error) {
	var environment ir.Environment
	if err := json.Unmarshal(artifact.Environment, &environment); err != nil {
		return ir.Environment{}, fmt.Errorf("decode artifact environment: %w", err)
	}
	return environment, nil
}
