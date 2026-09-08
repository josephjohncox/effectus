package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
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

// Engine owns its active generation and historical generations returned by its
// resolver. Execute is concurrent-safe and always reads durable execution state.
// Only in-flight calls and resolutions are retained; idle cache retention is zero.
// Close stops admission and waits for active calls before closing owned resources.
type Engine struct {
	generation      *Generation
	workflowStore   workflow.OutboxStore
	workflowFencing fencing.Provider
	workflowOptions schema.DispatcherOptions
	ledger          ledger.ExecutionLedger
	resolver        ArtifactResolver
	observer        Observer
	mu              sync.Mutex
	executions      map[string]*executionGate
	historical      map[string]*historicalGeneration
	active          sync.WaitGroup
	closeOnce       sync.Once
	closeErr        error
	started         bool
	closed          bool
}
type engineExecution struct {
	record            schema.ExecutionRecord
	facts             map[string]any
	selected          map[string]struct{}
	generation        *Generation
	releaseGeneration func()
}

func NewEngine(generation *Generation) (*Engine, error) {
	if generation == nil || generation.Checked() == nil || generation.Closed() {
		return nil, fmt.Errorf("open immutable generation is required")
	}
	return &Engine{generation: generation, ledger: schema.NewInMemoryExecutionLedger(), executions: make(map[string]*executionGate), historical: make(map[string]*historicalGeneration)}, nil
}

// Close waits for calls that already entered Execute, then closes resources.
// Do not call Close from an executor invoked by this engine. Repeated calls
// return the first resource-close error; borrowed stores are not closed.
func (engine *Engine) Close() error {
	if engine == nil {
		return nil
	}
	engine.closeOnce.Do(func() {
		engine.mu.Lock()
		engine.closed = true
		engine.mu.Unlock()
		engine.active.Wait()
		engine.recordCloseError(engine.generation.Close())
	})
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return engine.closeErr
}
func (engine *Engine) ConfigureWorkflow(store workflow.OutboxStore, provider fencing.Provider, options schema.DispatcherOptions) error {
	if engine == nil || store == nil {
		return fmt.Errorf("workflow outbox store is required")
	}
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.closed || engine.started {
		return fmt.Errorf("workflow cannot change after execution begins")
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
	if engine.closed || engine.started {
		return fmt.Errorf("execution ledger cannot change after execution begins")
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

// Generation returns a borrowed immutable reference. Do not close it separately
// or use its executor resources after Engine.Close.
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
	if request.WaitMode == "" {
		request.WaitMode = WaitTerminal
	}
	if request.WaitMode != WaitAccepted && request.WaitMode != WaitTerminal {
		return result, fmt.Errorf("%w: unknown wait mode", ErrInvalidExecuteRequest)
	}
	id := strings.TrimSpace(request.ResumeExecutionID)
	if (request.Admission == nil) == (id == "") {
		return result, fmt.Errorf("%w: set exactly one of admission or resume execution ID", ErrInvalidExecuteRequest)
	}
	if lease := request.RecoveryLease; lease != nil && (request.Admission != nil || request.WaitMode != WaitTerminal || lease.ExecutionID != id) {
		return result, fmt.Errorf("%w: recovery leases require terminal resume of the same execution", ErrInvalidExecuteRequest)
	}
	if request.Admission != nil {
		id = strings.TrimSpace(request.Admission.ExecutionID)
	}
	release, observer, beginErr := engine.beginExecution(ctx, id)
	if beginErr != nil {
		return result, beginErr
	}
	defer func() {
		release()
		if observer != nil {
			observer.ObserveExecution(result, resultErr)
		}
	}()
	var execution *engineExecution
	var created, atomic bool
	var err error
	if request.Admission != nil {
		execution, created, atomic, err = engine.admit(ctx, request.Admission, request.WaitMode == WaitTerminal)
	} else {
		execution, err = engine.loadExecution(ctx, strings.TrimSpace(request.ResumeExecutionID), request.WaitMode == WaitTerminal)
	}
	if execution != nil && execution.releaseGeneration != nil {
		defer execution.releaseGeneration()
	}
	if err != nil {
		if execution != nil && errors.Is(err, ErrBlockedDependency) && request.WaitMode == WaitTerminal && !schema.IsTerminalExecutionState(execution.record.State) {
			persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if persist := engine.persistExecutionState(persistCtx, execution, schema.ExecutionBlockedDependency, err.Error(), request.RecoveryLease); persist != nil {
				return engineResult(execution.record), errors.Join(err, fmt.Errorf("%w: %w", ErrDurableDisposition, persist))
			}
			return terminalExecutionResult(execution.record, request.WaitMode, err)
		}
		return engineFailureResult(execution), err
	}
	if schema.IsTerminalExecutionState(execution.record.State) {
		return terminalExecutionResult(execution.record, request.WaitMode, nil)
	}
	// Durable-admission callers never execute a replay synchronously. A matching
	// retry observes the recorded identity even after a worker has failed, while
	// a newly created admission requires one atomic ledger/outbox transaction.
	if request.WaitMode == WaitAccepted && (!created || atomic) {
		return engineResult(execution.record), nil
	}
	if err := engine.refreshExecution(ctx, execution, request.RecoveryLease); err != nil {
		return engineResult(execution.record), err
	}
	if schema.IsTerminalExecutionState(execution.record.State) {
		return terminalExecutionResult(execution.record, request.WaitMode, nil)
	}
	if created && !selectedExecutionHasSteps(execution) {
		err = engine.persistExecutionState(ctx, execution, schema.ExecutionCompleted, "", request.RecoveryLease)
		if err != nil {
			return engineResult(execution.record), err
		}
		return terminalExecutionResult(execution.record, request.WaitMode, nil)
	}
	if execution.generation == nil || execution.generation.Checked() == nil {
		return engineResult(execution.record), fmt.Errorf("%w: generation %s", ErrBlockedDependency, execution.record.GenerationDigest)
	}
	if request.WaitMode == WaitTerminal && execution.record.State == schema.ExecutionAccepted && request.RecoveryLease == nil {
		if err := engine.persistExecutionState(ctx, execution, schema.ExecutionRunning, "", nil); err != nil {
			return engineResult(execution.record), err
		}
		if schema.IsTerminalExecutionState(execution.record.State) {
			return terminalExecutionResult(execution.record, request.WaitMode, nil)
		}
	}
	err = engine.executeCheckedWorkflow(ctx, execution.generation, execution.record.TenantNamespace, execution.record.ExecutionID, execution.facts, execution.selected, request.WaitMode)
	// A caller deadline must not prevent recording an already observed outcome
	// or releasing recovery authority. The store still enforces the lease CAS.
	ctx, persistCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer persistCancel()
	if err != nil {
		state, disposition := engine.executionFailureState(ctx, execution)
		if disposition != nil {
			state = execution.record.State
		}
		if persist := engine.persistExecutionState(ctx, execution, state, err.Error(), request.RecoveryLease); persist != nil {
			return engineResult(execution.record), errors.Join(err, disposition, fmt.Errorf("%w: %v", ErrDurableDisposition, persist))
		}
		return terminalExecutionResult(execution.record, request.WaitMode, err)
	}
	if request.WaitMode == WaitTerminal {
		err = engine.persistExecutionState(ctx, execution, schema.ExecutionCompleted, "", request.RecoveryLease)
	}
	if err != nil {
		return engineResult(execution.record), err
	}
	return terminalExecutionResult(execution.record, request.WaitMode, nil)
}
func engineFailureResult(execution *engineExecution) ExecuteResult {
	if execution == nil {
		return ExecuteResult{}
	}
	return engineResult(execution.record)
}

func (engine *Engine) admit(ctx context.Context, input *Admission, resolve bool) (*engineExecution, bool, bool, error) {
	admission, err := prepareAdmission(input)
	if err != nil {
		return nil, false, false, err
	}
	identity := admissionIdentity(admission)
	if existing, e := engine.ledger.GetExecutionByAdmission(ctx, identity); e == nil {
		if err := engine.matchReplay(ctx, admission, existing); err != nil {
			return &engineExecution{record: existing}, false, false, err
		}
		x, e := engine.loadExecutionRecord(ctx, existing, resolve)
		return x, false, false, e
	} else if !errors.Is(e, schema.ErrExecutionNotFound) {
		return nil, false, false, e
	}
	generation, store := engine.generation, engine.workflowStore
	if generation == nil || store == nil {
		return nil, false, false, fmt.Errorf("checked durable workflow is not configured")
	}
	if admission.Ruleset != generation.Ruleset() || admission.Version != generation.Version() ||
		(admission.ExpectedGenerationDigest != "" && admission.ExpectedGenerationDigest != generation.Digest()) {
		return nil, false, false, ErrGenerationMismatch
	}
	hash, err := semanticAdmissionHash(admission, generation.Environment())
	if err != nil {
		return nil, false, false, err
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
				if err := engine.matchReplay(ctx, admission, existing); err != nil {
					return &engineExecution{record: existing}, false, atomic, err
				}
				x, loadErr := engine.loadExecutionRecord(ctx, existing, resolve)
				return x, false, atomic, loadErr
			}
			if errors.Is(err, ErrIdentityConflict) {
				return nil, false, atomic, fmt.Errorf("%w: %v", ErrIdentityConflict, err)
			}
		}
		return nil, false, atomic, err
	}
	if !created {
		if err := engine.matchReplay(ctx, admission, record); err != nil {
			return &engineExecution{record: record}, false, atomic, err
		}
		x, e := engine.loadExecutionRecord(ctx, record, resolve)
		return x, false, atomic, e
	}
	x := &engineExecution{record: record, facts: facts, selected: selected, generation: generation}
	return x, true, atomic, nil
}
func (engine *Engine) loadExecution(ctx context.Context, id string, resolve bool) (*engineExecution, error) {
	record, err := engine.ledger.GetExecution(ctx, id)
	if errors.Is(err, schema.ErrExecutionNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrExecutionNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	return engine.loadExecutionRecord(ctx, record, resolve)
}
func (engine *Engine) loadExecutionRecord(ctx context.Context, record schema.ExecutionRecord, resolve bool) (*engineExecution, error) {
	if !resolve || schema.IsTerminalExecutionState(record.State) {
		return &engineExecution{record: record}, nil
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
	var release func()
	if generation == nil || generation.Digest() != record.GenerationDigest {
		artifact, e := engine.ledger.GetArtifact(ctx, record.GenerationDigest)
		if e != nil {
			return engine.blockDependency(ctx, record, facts, selected, e)
		}
		if err := validateArtifactIdentity(record, artifact); err != nil {
			return &engineExecution{record: record}, err
		}
		generation, release, e = engine.acquireHistorical(ctx, artifact)
		if e != nil {
			return engine.blockDependency(ctx, record, facts, selected, e)
		}
	}
	if generation.Checked() == nil || generation.Ruleset() != record.Ruleset || generation.Version() != record.Version {
		if release != nil {
			release()
		}
		return &engineExecution{record: record}, ErrGenerationMismatch
	}
	x := &engineExecution{record: record, facts: facts, selected: selected, generation: generation, releaseGeneration: release}
	return x, nil
}
func (engine *Engine) blockDependency(_ context.Context, record schema.ExecutionRecord, facts map[string]any, selected map[string]struct{}, cause error) (*engineExecution, error) {
	// Execute decides disposition using the caller's authority. A lease read
	// from a record is not permission to impersonate its owner.
	x := &engineExecution{record: record, facts: facts, selected: selected}
	return x, fmt.Errorf("%w: %v", ErrBlockedDependency, cause)
}
func (engine *Engine) persistExecutionState(ctx context.Context, x *engineExecution, state schema.ExecutionState, message string, lease *schema.ExecutionLease) error {
	return engine.commitExecutionState(ctx, x, state, message, lease)
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
	nodes := 0
	return normalizeAdmissionValueDepth(value, 0, &nodes)
}

func normalizeAdmissionValueDepth(value any, depth int, nodes *int) (any, error) {
	*nodes++
	if depth > 64 || *nodes > 10000 {
		return nil, fmt.Errorf("admission value depth or node limit exceeded")
	}
	if value == nil {
		return nil, nil
	}
	if raw, ok := value.([]byte); ok {
		return base64.StdEncoding.EncodeToString(raw), nil
	}
	rv := reflect.ValueOf(value)
	// Do not invoke caller-supplied marshalers. Preserve the original nil map/slice
	// identities ({} and []), including nested occurrences.
	switch rv.Kind() {
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return nil, fmt.Errorf("admission map keys must be strings")
		}
		if rv.Len() > 10000 {
			return nil, fmt.Errorf("admission field limit exceeded")
		}
		keys := rv.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
		out := make(map[string]any, len(keys))
		for _, key := range keys {
			item, err := normalizeAdmissionValueDepth(rv.MapIndex(key).Interface(), depth+1, nodes)
			if err != nil {
				return nil, fmt.Errorf("field %q: %w", key.String(), err)
			}
			out[key.String()] = item
		}
		return out, nil
	case reflect.Slice, reflect.Array:
		if rv.Len() > 10000 {
			return nil, fmt.Errorf("admission item limit exceeded")
		}
		out := make([]any, rv.Len())
		for i := range out {
			item, err := normalizeAdmissionValueDepth(rv.Index(i).Interface(), depth+1, nodes)
			if err != nil {
				return nil, fmt.Errorf("list item %d: %w", i, err)
			}
			out[i] = item
		}
		return out, nil
	case reflect.Bool:
		value = rv.Bool()
	case reflect.String:
		if number, ok := value.(json.Number); ok {
			value = number
		} else {
			value = rv.String()
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value = rv.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value = rv.Uint()
	case reflect.Float32:
		value = float32(rv.Float())
	case reflect.Float64:
		value = rv.Float()
	default:
		return nil, fmt.Errorf("unsupported value type %T", value)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	var out any
	if err := decoder.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func isPostgresConcurrencyError(err error) bool {
	var value interface{ SQLState() string }
	return errors.As(err, &value) && (value.SQLState() == "40001" || value.SQLState() == "23505")
}
