package schema

import (
	"context"
	"fmt"

	effectusv1 "github.com/josephjohncox/effectus/gen/effectus/v1"
	"github.com/josephjohncox/effectus/ir"
)

// CheckedEnqueueRequest supplies runtime facts, prior result slots, and
// deployment metadata. The checked artifact supplies effect identity, order,
// verb, contract hash, and argument expressions.
type CheckedEnqueueRequest struct {
	SagaID                string
	PlanID                string
	EffectID              string
	Facts                 map[string]any
	ResultSlots           []any
	Arguments             map[string]any // Deprecated: accepted only when identical to resolved checked arguments. Removal deadline: 2027-09-01.
	CompensationVerb      string
	CompensationContract  string
	CompensationArguments map[string]any
	Fencing               []FencingRequirement
}

// EnqueueCheckedStep resolves arguments from the exact checked plan before it
// creates durable intent. It does not invoke an executor.
func EnqueueCheckedStep(ctx context.Context, store OutboxStore, checked *ir.Checked, request CheckedEnqueueRequest) (*Dispatch, error) {
	if store == nil || checked == nil {
		return nil, fmt.Errorf("outbox store and checked IR are required")
	}
	saga, err := store.GetSaga(ctx, request.SagaID)
	if err != nil {
		return nil, err
	}
	if saga.PlanID != request.PlanID || saga.PlanDigest != checked.Digest() {
		return nil, fmt.Errorf("%w: saga checked plan provenance", ErrIdentityConflict)
	}
	if saga.SagaID != StableSagaID(saga.ExecutionID, saga.PlanID) {
		return nil, fmt.Errorf("%w: saga ID is not the stable execution/plan identity", ErrIdentityConflict)
	}
	artifact := checked.CloneArtifact()
	for _, plan := range artifact.Plans {
		if plan.Id != request.PlanID {
			continue
		}
		for _, step := range plan.Steps {
			if step.Id != request.EffectID {
				continue
			}
			arguments, err := resolveCheckedStepArguments(step, request.Facts, request.ResultSlots)
			if err != nil {
				return nil, fmt.Errorf("resolve checked effect %q: %w", step.Id, err)
			}
			if request.Arguments != nil {
				_, expectedHash, err := CanonicalJSON(arguments)
				if err != nil {
					return nil, err
				}
				_, suppliedHash, err := CanonicalJSON(request.Arguments)
				if err != nil {
					return nil, err
				}
				if expectedHash != suppliedHash {
					return nil, fmt.Errorf("%w: supplied arguments contradict checked argument expressions", ErrIdentityConflict)
				}
			}
			compensationVerb, compensationContract := request.CompensationVerb, request.CompensationContract
			compensationArguments := request.CompensationArguments
			if step.Compensation != nil {
				if compensationVerb != "" && (compensationVerb != step.Compensation.InverseVerb || compensationContract != step.Compensation.InverseContractHash) {
					return nil, fmt.Errorf("%w: supplied compensation contradicts checked contract", ErrIdentityConflict)
				}
				compensationVerb, compensationContract = step.Compensation.InverseVerb, step.Compensation.InverseContractHash
				if compensationArguments == nil {
					compensationArguments = arguments
				}
			}
			fencing := request.Fencing
			if step.FencingRequirement == effectusv1.FencingRequirement_FENCING_REQUIREMENT_REQUIRED {
				checkedFencing := []FencingRequirement{{Authority: "checked-contract", Resource: step.Verb}}
				if len(fencing) != 0 && !sameRequirements(fencing, checkedFencing) {
					return nil, fmt.Errorf("%w: supplied fencing contradicts checked contract", ErrIdentityConflict)
				}
				fencing = checkedFencing
			}
			return store.EnqueueStep(ctx, EnqueueStepRequest{
				SagaID: request.SagaID, EffectID: step.Id, Sequence: int(step.Ordinal) + 1,
				Verb: step.Verb, ContractHash: step.ContractHash, Arguments: arguments,
				CompensationVerb: compensationVerb, CompensationContract: compensationContract,
				CompensationArguments: compensationArguments, Fencing: fencing,
			})
		}
		return nil, fmt.Errorf("checked effect %q not found in plan %q", request.EffectID, request.PlanID)
	}
	return nil, fmt.Errorf("checked plan not found: %s", request.PlanID)
}

func resolveCheckedStepArguments(step *effectusv1.Step, facts map[string]any, slots []any) (map[string]any, error) {
	arguments := make(map[string]any, len(step.Arguments))
	for _, argument := range step.Arguments {
		if argument == nil || argument.Value == nil {
			return nil, fmt.Errorf("argument is incomplete")
		}
		value, err := resolveCheckedStepValue(argument.Value, facts, slots)
		if err != nil {
			return nil, fmt.Errorf("argument %q: %w", argument.Name, err)
		}
		arguments[argument.Name] = value
	}
	return arguments, nil
}

func resolveCheckedStepValue(value *effectusv1.Value, facts map[string]any, slots []any) (any, error) {
	switch kind := value.Kind.(type) {
	case *effectusv1.Value_Literal:
		return checkedSagaLiteral(kind.Literal)
	case *effectusv1.Value_FactPath:
		resolved, ok := lookupCheckedSagaFact(facts, kind.FactPath)
		if !ok {
			return nil, fmt.Errorf("fact %q is missing", kind.FactPath)
		}
		return resolved, nil
	case *effectusv1.Value_ResultSlot:
		if int(kind.ResultSlot) >= len(slots) {
			return nil, fmt.Errorf("result slot %d is unavailable", kind.ResultSlot)
		}
		return slots[kind.ResultSlot], nil
	default:
		return nil, fmt.Errorf("value kind is not set")
	}
}

func lookupCheckedSagaFact(facts map[string]any, path string) (any, bool) {
	if value, ok := facts[path]; ok {
		return value, true
	}
	current := any(facts)
	for start := 0; start < len(path); {
		end := start
		for end < len(path) && path[end] != '.' {
			end++
		}
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[path[start:end]]
		if !ok {
			return nil, false
		}
		start = end + 1
	}
	return current, true
}

func checkedSagaLiteral(literal *effectusv1.Literal) (any, error) {
	if literal == nil {
		return nil, fmt.Errorf("literal is nil")
	}
	switch kind := literal.Kind.(type) {
	case *effectusv1.Literal_Null:
		return nil, nil
	case *effectusv1.Literal_BoolValue:
		return kind.BoolValue, nil
	case *effectusv1.Literal_IntValue:
		return kind.IntValue, nil
	case *effectusv1.Literal_DoubleValue:
		return kind.DoubleValue, nil
	case *effectusv1.Literal_StringValue:
		return kind.StringValue, nil
	case *effectusv1.Literal_BytesValue:
		return append([]byte(nil), kind.BytesValue...), nil
	case *effectusv1.Literal_ListValue:
		if kind.ListValue == nil {
			return nil, fmt.Errorf("list literal is nil")
		}
		values := make([]any, len(kind.ListValue.Values))
		for i, value := range kind.ListValue.Values {
			resolved, err := checkedSagaLiteral(value)
			if err != nil {
				return nil, err
			}
			values[i] = resolved
		}
		return values, nil
	case *effectusv1.Literal_ObjectValue:
		if kind.ObjectValue == nil {
			return nil, fmt.Errorf("object literal is nil")
		}
		object := make(map[string]any, len(kind.ObjectValue.Fields))
		for _, field := range kind.ObjectValue.Fields {
			resolved, err := checkedSagaLiteral(field.Value)
			if err != nil {
				return nil, err
			}
			object[field.Name] = resolved
		}
		return object, nil
	default:
		return nil, fmt.Errorf("literal kind is not set")
	}
}
