package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/josephjohncox/effectus/schema"
	"github.com/josephjohncox/effectus/schema/fencing"
	"github.com/josephjohncox/effectus/schema/ledger"
	"github.com/josephjohncox/effectus/schema/workflow"
)

var (
	ErrInvalidExecuteRequest = errors.New("invalid engine execute request")
	ErrExecutionNotFound     = errors.New("engine execution not found")
	// ErrIdentityConflict is the canonical identity-conflict sentinel. Durable
	// stores use the same sentinel so transports classify direct and raced
	// persistence conflicts consistently.
	ErrIdentityConflict   = schema.ErrIdentityConflict
	ErrGenerationMismatch = errors.New("engine generation mismatch")
	ErrBlockedDependency  = errors.New("execution blocked by missing dependency")
	ErrDurableDisposition = errors.New("durable execution disposition failed")
)

type Observer interface {
	ObserveExecution(ExecuteResult, error)
	ObserveRecovery(RecoveryObservation)
}
type RecoveryObservation struct {
	BacklogMeasured                     bool
	Backlog, Blocked                    int64
	OldestExecutionAge, OldestOutboxAge time.Duration
	ExecutionID, State                  string
	Err                                 error
}
type WaitMode string

const (
	WaitAccepted WaitMode = "accepted"
	WaitTerminal WaitMode = "terminal"
)

type Admission struct {
	ExecutionID              string         `json:"execution_id"`
	AdmissionID              string         `json:"admission_id,omitempty"`
	TenantNamespace          string         `json:"tenant_namespace"`
	Ruleset                  string         `json:"ruleset"`
	Version                  string         `json:"version"`
	Facts                    map[string]any `json:"facts"`
	MergePolicy              string         `json:"merge_policy,omitempty"`
	ExpectedGenerationDigest string         `json:"expected_generation_digest,omitempty"`
}
type ExecuteRequest struct {
	Admission         *Admission
	ResumeExecutionID string
	WaitMode          WaitMode
	RecoveryLease     *schema.ExecutionLease
}
type ExecuteResult struct {
	ExecutionID      string `json:"execution_id"`
	GenerationDigest string `json:"generation_digest"`
	State            string `json:"state"`
	DurablyAccepted  bool   `json:"durably_accepted"`
	Completed        bool   `json:"completed"`
}

// Engine owns exactly one immutable Generation. A process must be replaced to
// execute a changed bundle; durable recovery resolves the generation pinned in
// the execution artifact rather than consulting mutable process state.
type Engine struct {
	generation      *Generation
	workflowStore   workflow.OutboxStore
	workflowFencing fencing.Provider
	workflowOptions schema.DispatcherOptions
	ledger          ledger.ExecutionLedger
	resolver        ArtifactResolver
	observer        Observer
	mu              sync.Mutex
	executions      map[string]*engineExecution
	closed          bool
}
type engineExecution struct {
	mu         sync.Mutex
	record     schema.ExecutionRecord
	facts      map[string]any
	selected   map[string]struct{}
	generation *Generation
}

func NewEngine(generation *Generation) (*Engine, error) {
	if generation == nil || generation.Checked() == nil {
		return nil, fmt.Errorf("immutable generation is required")
	}
	return &Engine{generation: generation, ledger: schema.NewInMemoryExecutionLedger(), executions: make(map[string]*engineExecution)}, nil
}
func (engine *Engine) Close() error {
	if engine == nil {
		return nil
	}
	engine.mu.Lock()
	if engine.closed {
		engine.mu.Unlock()
		return nil
	}
	engine.closed = true
	generation := engine.generation
	engine.mu.Unlock()
	return generation.Close()
}
func (engine *Engine) ConfigureWorkflow(store workflow.OutboxStore, provider fencing.Provider, options schema.DispatcherOptions) error {
	if engine == nil || store == nil {
		return fmt.Errorf("workflow outbox store is required")
	}
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.closed || len(engine.executions) != 0 {
		return fmt.Errorf("workflow cannot change after admission")
	}
	engine.workflowStore, engine.workflowFencing, engine.workflowOptions = store, provider, options
	return nil
}
func (engine *Engine) ConfigureLedger(durable ledger.ExecutionLedger, resolver ArtifactResolver) error {
	if engine == nil || durable == nil {
		return fmt.Errorf("execution ledger is required")
	}
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.closed || len(engine.executions) != 0 {
		return fmt.Errorf("execution ledger cannot change after admission")
	}
	engine.ledger, engine.resolver = durable, resolver
	return nil
}
func (engine *Engine) SetObserver(observer Observer) {
	if engine != nil {
		engine.mu.Lock()
		engine.observer = observer
		engine.mu.Unlock()
	}
}
func (engine *Engine) Generation() *Generation {
	if engine == nil {
		return nil
	}
	return engine.generation
}
func (engine *Engine) ActiveGenerationDigest() string { return engine.Generation().Digest() }

func (engine *Engine) Execute(ctx context.Context, request ExecuteRequest) (result ExecuteResult, resultErr error) {
	if engine == nil || ctx == nil {
		return result, fmt.Errorf("%w: engine and context are required", ErrInvalidExecuteRequest)
	}
	engine.mu.Lock()
	closed := engine.closed
	observer := engine.observer
	engine.mu.Unlock()
	if closed {
		return result, fmt.Errorf("engine is closed")
	}
	defer func() {
		if observer != nil {
			observer.ObserveExecution(result, resultErr)
		}
	}()
	if request.WaitMode == "" {
		request.WaitMode = WaitTerminal
	}
	if request.WaitMode != WaitAccepted && request.WaitMode != WaitTerminal {
		return result, fmt.Errorf("%w: unknown wait mode", ErrInvalidExecuteRequest)
	}
	if (request.Admission == nil) == (strings.TrimSpace(request.ResumeExecutionID) == "") {
		return result, fmt.Errorf("%w: set exactly one of admission or resume execution ID", ErrInvalidExecuteRequest)
	}
	var execution *engineExecution
	var created, atomic bool
	var err error
	if request.Admission != nil {
		execution, created, atomic, err = engine.admit(ctx, request.Admission)
	} else {
		execution, err = engine.loadExecution(ctx, request.ResumeExecutionID)
	}
	if err != nil {
		return engineFailureResult(execution), err
	}
	execution.mu.Lock()
	defer execution.mu.Unlock()
	if schema.IsTerminalExecutionState(execution.record.State) {
		return engineResult(execution.record), nil
	}
	if created && !selectedExecutionHasSteps(execution) {
		err = engine.persistExecutionState(ctx, execution, schema.ExecutionCompleted, "", request.RecoveryLease)
		return engineResult(execution.record), err
	}
	// Durable-admission callers never execute a replay synchronously. A matching
	// retry observes the recorded identity even after a worker has failed, while
	// a newly created admission requires one atomic ledger/outbox transaction.
	if request.WaitMode == WaitAccepted && (!created || atomic) {
		return engineResult(execution.record), nil
	}
	if execution.generation == nil || execution.generation.Checked() == nil {
		return engineResult(execution.record), fmt.Errorf("%w: generation %s", ErrBlockedDependency, execution.record.GenerationDigest)
	}
	if request.WaitMode == WaitTerminal && execution.record.State == schema.ExecutionAccepted && request.RecoveryLease == nil {
		if updated, e := engine.ledger.SetExecutionState(ctx, execution.record.ExecutionID, execution.record.Revision, schema.ExecutionRunning, ""); e == nil {
			execution.record = updated
		} else if !errors.Is(e, schema.ErrOptimisticConflict) {
			return engineResult(execution.record), e
		}
	}
	err = engine.executeCheckedWorkflow(ctx, execution.generation, execution.record.TenantNamespace, execution.record.ExecutionID, execution.facts, execution.selected, request.WaitMode)
	if err != nil {
		state, disposition := engine.executionFailureState(ctx, execution)
		if disposition != nil {
			state = execution.record.State
		}
		if persist := engine.persistExecutionState(ctx, execution, state, err.Error(), request.RecoveryLease); persist != nil {
			return engineResult(execution.record), errors.Join(err, disposition, fmt.Errorf("%w: %v", ErrDurableDisposition, persist))
		}
		return engineResult(execution.record), err
	}
	if request.WaitMode == WaitTerminal {
		err = engine.persistExecutionState(ctx, execution, schema.ExecutionCompleted, "", request.RecoveryLease)
	}
	return engineResult(execution.record), err
}
func engineFailureResult(execution *engineExecution) ExecuteResult {
	if execution == nil {
		return ExecuteResult{}
	}
	return engineResult(execution.record)
}

func (engine *Engine) admit(ctx context.Context, admission *Admission) (*engineExecution, bool, bool, error) {
	if admission == nil {
		return nil, false, false, fmt.Errorf("%w: admission is nil", ErrInvalidExecuteRequest)
	}
	admission.ExecutionID = strings.TrimSpace(admission.ExecutionID)
	admission.TenantNamespace = strings.TrimSpace(admission.TenantNamespace)
	admission.AdmissionID = strings.TrimSpace(admission.AdmissionID)
	if admission.ExecutionID == "" || admission.TenantNamespace == "" {
		return nil, false, false, fmt.Errorf("%w: stable execution ID and tenant namespace are required", ErrInvalidExecuteRequest)
	}
	hash, err := admissionHash(admission)
	if err != nil {
		return nil, false, false, err
	}
	identity := admission.AdmissionID
	if identity == "" {
		identity = admission.ExecutionID
	}
	if existing, e := engine.ledger.GetExecutionByAdmission(ctx, identity); e == nil {
		if admission.ExpectedGenerationDigest != "" && admission.ExpectedGenerationDigest != existing.GenerationDigest {
			return nil, false, false, ErrGenerationMismatch
		}
		if existing.ExecutionID != admission.ExecutionID || existing.RequestHash != hash {
			return nil, false, false, fmt.Errorf("%w: admission identity %s", ErrIdentityConflict, identity)
		}
		x, e := engine.loadExecutionRecord(ctx, existing)
		return x, false, false, e
	} else if !errors.Is(e, schema.ErrExecutionNotFound) {
		return nil, false, false, e
	}
	engine.mu.Lock()
	generation, store := engine.generation, engine.workflowStore
	engine.mu.Unlock()
	if generation == nil || store == nil {
		return nil, false, false, fmt.Errorf("checked durable workflow is not configured")
	}
	durable, selected, facts, err := buildDurableAdmission(ctx, generation, admission, hash)
	if err != nil {
		return nil, false, false, err
	}
	if admission.ExpectedGenerationDigest != "" && admission.ExpectedGenerationDigest != durable.Artifact.GenerationDigest {
		return nil, false, false, ErrGenerationMismatch
	}
	var record schema.ExecutionRecord
	created := false
	atomic := false
	if atomicStore, ok := engine.ledger.(schema.AtomicAdmissionStore); ok && any(store) == any(atomicStore) {
		record, created, err = atomicStore.AdmitExecutionAtomic(ctx, durable)
		atomic = true
	} else {
		if err = engine.ledger.PutArtifact(ctx, durable.Artifact); err == nil {
			record, created, err = engine.ledger.AdmitExecution(ctx, durable)
		}
	}
	if err != nil {
		if errors.Is(err, ErrIdentityConflict) || isPostgresConcurrencyError(err) {
			existing, getErr := engine.ledger.GetExecutionByAdmission(ctx, durable.Execution.AdmissionIdentity)
			if getErr == nil {
				if existing.ExecutionID == durable.Execution.ExecutionID && existing.RequestHash == durable.Execution.RequestHash {
					x, loadErr := engine.loadExecutionRecord(ctx, existing)
					return x, false, atomic, loadErr
				}
				return nil, false, atomic, fmt.Errorf("%w: admission identity %s", ErrIdentityConflict, durable.Execution.AdmissionIdentity)
			}
			if errors.Is(err, ErrIdentityConflict) {
				return nil, false, atomic, fmt.Errorf("%w: %v", ErrIdentityConflict, err)
			}
		}
		return nil, false, atomic, err
	}
	if !created {
		x, e := engine.loadExecutionRecord(ctx, record)
		return x, false, atomic, e
	}
	x := &engineExecution{record: record, facts: facts, selected: selected, generation: generation}
	engine.mu.Lock()
	engine.executions[record.ExecutionID] = x
	engine.mu.Unlock()
	return x, true, atomic, nil
}
func (engine *Engine) loadExecution(ctx context.Context, id string) (*engineExecution, error) {
	engine.mu.Lock()
	x := engine.executions[id]
	engine.mu.Unlock()
	if x != nil {
		return x, nil
	}
	record, err := engine.ledger.GetExecution(ctx, id)
	if errors.Is(err, schema.ErrExecutionNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrExecutionNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	return engine.loadExecutionRecord(ctx, record)
}
func (engine *Engine) loadExecutionRecord(ctx context.Context, record schema.ExecutionRecord) (*engineExecution, error) {
	engine.mu.Lock()
	cached := engine.executions[record.ExecutionID]
	engine.mu.Unlock()
	if cached != nil {
		return cached, nil
	}
	facts, err := decodeExecutionFacts(record.EffectiveFacts)
	if err != nil {
		return nil, err
	}
	selected := map[string]struct{}{}
	for _, plan := range record.Plans {
		selected[plan.PlanID] = struct{}{}
	}
	generation := engine.generation
	if generation == nil || generation.Digest() != record.GenerationDigest {
		artifact, e := engine.ledger.GetArtifact(ctx, record.GenerationDigest)
		if e != nil {
			return engine.blockDependency(ctx, record, facts, selected, e)
		}
		if engine.resolver == nil {
			return engine.blockDependency(ctx, record, facts, selected, fmt.Errorf("no immutable artifact resolver is configured"))
		}
		generation, e = engine.resolver.ResolveGeneration(ctx, artifact)
		if e != nil {
			return engine.blockDependency(ctx, record, facts, selected, e)
		}
	}
	if generation.Checked() == nil {
		return engine.blockDependency(ctx, record, facts, selected, fmt.Errorf("resolved generation has no checked IR"))
	}
	x := &engineExecution{record: record, facts: facts, selected: selected, generation: generation}
	engine.mu.Lock()
	engine.executions[record.ExecutionID] = x
	engine.mu.Unlock()
	return x, nil
}
func (engine *Engine) blockDependency(ctx context.Context, record schema.ExecutionRecord, facts map[string]any, selected map[string]struct{}, cause error) (*engineExecution, error) {
	if record.State != schema.ExecutionBlockedDependency {
		if record.RecoveryToken != "" {
			lease := schema.ExecutionLease{ExecutionID: record.ExecutionID, Owner: record.RecoveryOwner, Token: record.RecoveryToken, Deadline: record.RecoveryDeadline, Revision: record.Revision}
			if err := engine.ledger.FinishExecutionLease(ctx, lease, schema.ExecutionBlockedDependency, cause.Error()); err == nil {
				record, _ = engine.ledger.GetExecution(ctx, record.ExecutionID)
			}
		} else if updated, err := engine.ledger.SetExecutionState(ctx, record.ExecutionID, record.Revision, schema.ExecutionBlockedDependency, cause.Error()); err == nil {
			record = updated
		}
	}
	x := &engineExecution{record: record, facts: facts, selected: selected}
	engine.mu.Lock()
	engine.executions[record.ExecutionID] = x
	engine.mu.Unlock()
	return x, fmt.Errorf("%w: %v", ErrBlockedDependency, cause)
}
func (engine *Engine) persistExecutionState(ctx context.Context, x *engineExecution, state schema.ExecutionState, message string, lease *schema.ExecutionLease) error {
	if lease != nil {
		next := state
		if !schema.IsTerminalExecutionState(state) {
			next = ""
		}
		if err := engine.ledger.FinishExecutionLease(ctx, *lease, next, message); err != nil {
			return fmt.Errorf("%w: %v", ErrDurableDisposition, err)
		}
		updated, err := engine.ledger.GetExecution(ctx, x.record.ExecutionID)
		if err != nil {
			return err
		}
		x.record = updated
		return nil
	}
	updated, err := engine.ledger.SetExecutionState(ctx, x.record.ExecutionID, x.record.Revision, state, message)
	if err == nil {
		x.record = updated
	}
	return err
}
func (engine *Engine) executionFailureState(ctx context.Context, x *engineExecution) (schema.ExecutionState, error) {
	for _, plan := range x.record.Plans {
		saga, err := engine.workflowStore.GetSaga(ctx, plan.SagaID)
		if err != nil {
			return x.record.State, err
		}
		switch saga.State {
		case schema.SagaBlockedUnknown:
			return schema.ExecutionBlockedUnknown, nil
		case schema.SagaBlockedFence:
			return schema.ExecutionBlockedFence, nil
		case schema.SagaBlockedDependency:
			return schema.ExecutionBlockedDependency, nil
		case schema.SagaBlockedCompensation:
			return schema.ExecutionBlockedCompensation, nil
		case schema.SagaFailed, schema.SagaCompensated:
			return schema.ExecutionFailed, nil
		}
	}
	return x.record.State, nil
}
func selectedExecutionHasSteps(x *engineExecution) bool {
	if x == nil || x.generation == nil || x.generation.Checked() == nil {
		return false
	}
	for _, plan := range x.generation.Checked().CloneArtifact().Plans {
		if _, ok := x.selected[plan.Id]; ok && len(plan.Steps) > 0 {
			return true
		}
	}
	return false
}
func engineResult(record schema.ExecutionRecord) ExecuteResult {
	return ExecuteResult{ExecutionID: record.ExecutionID, GenerationDigest: record.GenerationDigest, State: string(record.State), DurablyAccepted: record.State != schema.ExecutionAdmitting, Completed: record.State == schema.ExecutionCompleted}
}
func decodeExecutionFacts(data json.RawMessage) (map[string]any, error) {
	var facts map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&facts); err != nil {
		return nil, fmt.Errorf("decode frozen execution facts: %w", err)
	}
	return facts, nil
}
func admissionHash(a *Admission) (string, error) {
	facts, err := canonicalJSONValue(a.Facts)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(struct {
		Namespace   string          `json:"namespace"`
		Ruleset     string          `json:"ruleset"`
		Version     string          `json:"version"`
		MergePolicy string          `json:"merge_policy"`
		Facts       json.RawMessage `json:"facts"`
	}{a.TenantNamespace, a.Ruleset, a.Version, a.MergePolicy, facts})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
func canonicalJSONValue(value any) ([]byte, error) {
	normalized, err := normalizeAdmissionValue(value)
	if err != nil {
		return nil, err
	}
	return json.Marshal(normalized)
}
func normalizeAdmissionValue(value any) (any, error) {
	switch value := value.(type) {
	case nil, bool, string, json.Number, float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		data, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		var out any
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		decoder.UseNumber()
		err = decoder.Decode(&out)
		return out, err
	case map[string]any:
		keys := make([]string, 0, len(value))
		for k := range value {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(value))
		for _, k := range keys {
			v, e := normalizeAdmissionValue(value[k])
			if e != nil {
				return nil, e
			}
			out[k] = v
		}
		return out, nil
	case []any:
		out := make([]any, len(value))
		for i := range value {
			v, e := normalizeAdmissionValue(value[i])
			if e != nil {
				return nil, e
			}
			out[i] = v
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported value type %T", value)
	}
}
func isPostgresConcurrencyError(err error) bool {
	var value interface{ SQLState() string }
	return errors.As(err, &value) && (value.SQLState() == "40001" || value.SQLState() == "23505")
}
