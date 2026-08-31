package schema

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/josephjohncox/effectus/invocation"
)

var (
	ErrIdentityConflict   = errors.New("durable identity conflict")
	ErrNoDispatch         = errors.New("no eligible dispatch")
	ErrStaleLease         = errors.New("stale dispatch lease")
	ErrTerminalSaga       = errors.New("terminal saga cannot be reopened")
	ErrInvalidTransition  = errors.New("invalid saga state transition")
	ErrOptimisticConflict = errors.New("optimistic persistence conflict")
)

// Durable workflow contracts live in schema/workflow. Compatibility aliases
// remain in contracts.go during the package-boundary migration.
// StableAdmissionID scopes a transport idempotency key to its checked ruleset.
func StableAdmissionID(namespace, deliveryID, ruleset, version string) string {
	return hashIdentity(namespace, deliveryID, ruleset, version)
}

// StableExecutionID derives an execution identity from admission identity.
func StableExecutionID(namespace, deliveryID, ruleset, version string) string {
	return StableAdmissionID(namespace, deliveryID, ruleset, version)
}

// StableSagaID derives one plan saga identity from an execution.
func StableSagaID(executionID, planID string) string {
	return hashIdentity(executionID, planID)
}

// IdempotencyKey returns a stable key. An attempt is deliberately not an input.
func IdempotencyKey(namespace, sagaID, effectID string, direction invocation.Direction) string {
	return hashIdentity(namespace, sagaID, effectID, string(direction))
}

func hashIdentity(components ...string) string {
	hash := sha256.New()
	for _, component := range components {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(component)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(component))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// CanonicalJSON encodes JSON-compatible data and rejects lossy or unsupported values.
func CanonicalJSON(value any) (json.RawMessage, string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, "", fmt.Errorf("encode canonical JSON: %w", err)
	}
	var decoded any
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, "", fmt.Errorf("verify canonical JSON: %w", err)
	}
	canonical, err := json.Marshal(decoded)
	if err != nil {
		return nil, "", fmt.Errorf("re-encode canonical JSON: %w", err)
	}
	digest := sha256.Sum256(canonical)
	return canonical, hex.EncodeToString(digest[:]), nil
}

func validateSagaRequest(request CreateSagaRequest) error {
	if !request.Serial {
		return fmt.Errorf("non-serial durable sagas are not supported until late-success compensation is atomic")
	}
	testOverride := request.AllowUnstableIdentityForTest && strings.HasSuffix(os.Args[0], ".test")
	if !testOverride && request.SagaID != StableSagaID(request.ExecutionID, request.PlanID) {
		return fmt.Errorf("%w: saga ID must equal StableSagaID(execution_id, plan_id)", ErrIdentityConflict)
	}
	for name, value := range map[string]string{
		"namespace": request.Namespace, "saga_id": request.SagaID,
		"execution_id": request.ExecutionID, "plan_id": request.PlanID,
		"plan_digest": request.PlanDigest,
	} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("%s is required and must not have surrounding whitespace", name)
		}
	}
	return nil
}

func normalizeEnqueue(request EnqueueStepRequest) (EnqueueStepRequest, json.RawMessage, string, json.RawMessage, string, error) {
	if strings.TrimSpace(request.SagaID) == "" || strings.TrimSpace(request.EffectID) == "" || strings.TrimSpace(request.Verb) == "" {
		return request, nil, "", nil, "", fmt.Errorf("saga_id, effect_id, and verb are required")
	}
	if request.Sequence <= 0 {
		return request, nil, "", nil, "", fmt.Errorf("step sequence must be positive")
	}
	if strings.TrimSpace(request.ContractHash) == "" {
		return request, nil, "", nil, "", fmt.Errorf("contract hash is required")
	}
	arguments, argumentHash, err := CanonicalJSON(request.Arguments)
	if err != nil {
		return request, nil, "", nil, "", err
	}
	var compensationArguments json.RawMessage
	var compensationHash string
	if request.CompensationVerb != "" {
		if request.CompensationContract == "" {
			return request, nil, "", nil, "", fmt.Errorf("compensation contract hash is required")
		}
		if request.CompensationArguments == nil {
			request.CompensationArguments = request.Arguments
		}
		compensationArguments, compensationHash, err = CanonicalJSON(request.CompensationArguments)
		if err != nil {
			return request, nil, "", nil, "", fmt.Errorf("compensation arguments: %w", err)
		}
	} else if request.CompensationContract != "" || request.CompensationArguments != nil {
		return request, nil, "", nil, "", fmt.Errorf("compensation metadata requires a compensation verb")
	}
	request.Fencing = append([]FencingRequirement(nil), request.Fencing...)
	sort.Slice(request.Fencing, func(i, j int) bool {
		if request.Fencing[i].Authority != request.Fencing[j].Authority {
			return request.Fencing[i].Authority < request.Fencing[j].Authority
		}
		return request.Fencing[i].Resource < request.Fencing[j].Resource
	})
	for index, requirement := range request.Fencing {
		if requirement.Authority == "" || requirement.Resource == "" {
			return request, nil, "", nil, "", fmt.Errorf("fencing authority and resource are required")
		}
		if index > 0 && requirement == request.Fencing[index-1] {
			return request, nil, "", nil, "", fmt.Errorf("duplicate fencing requirement %s/%s", requirement.Authority, requirement.Resource)
		}
	}
	return request, arguments, argumentHash, compensationArguments, compensationHash, nil
}

func isTerminalSaga(state SagaState) bool {
	switch state {
	case SagaCompleted, SagaCompensated, SagaFailed, SagaBlockedUnknown,
		SagaBlockedDependency, SagaBlockedFence, SagaBlockedCompensation:
		return true
	default:
		return false
	}
}

func cloneDispatch(dispatch *Dispatch) *Dispatch {
	if dispatch == nil {
		return nil
	}
	copy := *dispatch
	copy.Arguments = append(json.RawMessage(nil), dispatch.Arguments...)
	copy.Result = append(json.RawMessage(nil), dispatch.Result...)
	copy.Fencing = append([]FencingRequirement(nil), dispatch.Fencing...)
	copy.FencingGrants = append([]invocation.FencingGrant(nil), dispatch.FencingGrants...)
	return &copy
}
