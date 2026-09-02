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

	"github.com/josephjohncox/effectus/compiler"
	"github.com/josephjohncox/effectus/internal/loader"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
	"github.com/josephjohncox/effectus/schema"
	"github.com/josephjohncox/effectus/schema/ledger"
)

var (
	ErrInvalidExecuteRequest = errors.New("invalid engine execute request")
	ErrExecutionNotFound     = errors.New("engine execution not found")
	ErrIdentityConflict      = errors.New("engine admission identity conflict")
	ErrGenerationMismatch    = errors.New("engine generation mismatch")
	ErrBlockedDependency     = errors.New("execution blocked by missing dependency")
	ErrDurableDisposition    = errors.New("durable execution disposition failed")
)

// Observer receives checked-runtime events without coupling the runtime to a
// metrics implementation. Implementations must not block execution.
type Observer interface {
	ObserveExecution(ExecuteResult, error)
	ObserveRecovery(RecoveryObservation)
}

// RecoveryObservation describes one bounded recovery poll or disposition.
type RecoveryObservation struct {
	BacklogMeasured    bool
	Backlog            int64
	Blocked            int64
	OldestExecutionAge time.Duration
	OldestOutboxAge    time.Duration
	ExecutionID        string
	State              string
	Err                error
}

// WaitMode controls how far Execute drives the shared state machine.
type WaitMode string

const (
	WaitAccepted WaitMode = "accepted"
	WaitTerminal WaitMode = "terminal"
)

// Admission is the transport-neutral logical request for a new execution.
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

// ExecuteRequest admits a new execution or resumes an existing one. Exactly
// one of Admission and ResumeExecutionID must be set.
type ExecuteRequest struct {
	Admission         *Admission
	ResumeExecutionID string
	WaitMode          WaitMode
	RecoveryLease     *schema.ExecutionLease // Set only by RecoveryWorker.
}

// ExecuteResult reports the durable boundary reached by Execute.
type ExecuteResult struct {
	ExecutionID      string `json:"execution_id"`
	GenerationDigest string `json:"generation_digest"`
	State            string `json:"state"`
	DurablyAccepted  bool   `json:"durably_accepted"`
	Completed        bool   `json:"completed"`
}

// Engine is the one workflow entry point used by compatibility runtime and
// transport adapters. It pins the active compiled unit for each admitted
// in-process execution. Durable deployments replace the in-memory admission
// index with the execution ledger without changing this API.
type Engine struct {
	runtime *ExecutionRuntime

	mu         sync.Mutex
	executions map[string]*engineExecution
	ledger     ledger.ExecutionLedger
	resolver   ArtifactResolver
	observer   Observer
}

type engineExecution struct {
	mu             sync.Mutex
	record         schema.ExecutionRecord
	facts          map[string]any
	selected       map[string]struct{}
	unit           *compiler.CompiledUnit
	snapshotHandle *loader.ExtensionSnapshotHandle
}

func newRuntimeEngine(runtime *ExecutionRuntime) *Engine {
	return &Engine{runtime: runtime, executions: make(map[string]*engineExecution), ledger: schema.NewInMemoryExecutionLedger()}
}

// NewEngine returns the shared engine owned by an ExecutionRuntime.
func NewEngine(runtime *ExecutionRuntime) (*Engine, error) {
	if runtime == nil {
		return nil, fmt.Errorf("execution runtime is required")
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.engine == nil {
		runtime.engine = newRuntimeEngine(runtime)
	}
	return runtime.engine, nil
}

// ConfigureLedger replaces the development ledger. Configure the same
// PostgresOutboxStore as both workflow outbox and ledger to enable atomic admission.
func (engine *Engine) ConfigureLedger(durableLedger ledger.ExecutionLedger, resolver ArtifactResolver) error {
	if engine == nil || durableLedger == nil {
		return fmt.Errorf("execution ledger is required")
	}
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if len(engine.executions) != 0 {
		return fmt.Errorf("execution ledger cannot change after admission")
	}
	engine.ledger, engine.resolver = durableLedger, resolver
	return nil
}

// SetObserver installs an optional runtime observer.
func (engine *Engine) SetObserver(observer Observer) {
	if engine == nil {
		return
	}
	engine.mu.Lock()
	engine.observer = observer
	engine.mu.Unlock()
}

// ActiveGeneration returns a copy of the checked runtime publication currently
// used for new admissions, including its immutable bundle metadata.
func (engine *Engine) ActiveGeneration() *ExecutionGeneration {
	if engine == nil || engine.runtime == nil {
		return nil
	}
	return engine.runtime.ActiveGeneration()
}

// ActiveGenerationDigest returns the checked engine generation currently used
// for new admissions.
func (engine *Engine) ActiveGenerationDigest() string {
	generation := engine.ActiveGeneration()
	if generation == nil {
		return ""
	}
	return generation.GenerationDigest
}

// Execute enters the same state machine for new admissions and recovery.
func (engine *Engine) Execute(ctx context.Context, request ExecuteRequest) (result ExecuteResult, resultErr error) {
	if engine != nil && engine.runtime != nil {
		engine.runtime.executionMu.RLock()
		defer engine.runtime.executionMu.RUnlock()
		engine.runtime.mu.RLock()
		closed := engine.runtime.state == StateClosing || engine.runtime.state == StateClosed
		engine.runtime.mu.RUnlock()
		if closed {
			return ExecuteResult{}, fmt.Errorf("runtime is closed")
		}
		defer func() {
			engine.mu.Lock()
			observer := engine.observer
			engine.mu.Unlock()
			if observer != nil {
				observer.ObserveExecution(result, resultErr)
			}
		}()
	}
	if engine == nil || engine.runtime == nil || engine.ledger == nil {
		return ExecuteResult{}, fmt.Errorf("%w: engine is not configured", ErrInvalidExecuteRequest)
	}
	if ctx == nil {
		return ExecuteResult{}, fmt.Errorf("%w: context is nil", ErrInvalidExecuteRequest)
	}
	if request.WaitMode == "" {
		request.WaitMode = WaitTerminal
	}
	if request.WaitMode != WaitAccepted && request.WaitMode != WaitTerminal {
		return ExecuteResult{}, fmt.Errorf("%w: unknown wait mode %q", ErrInvalidExecuteRequest, request.WaitMode)
	}
	if (request.Admission == nil) == (strings.TrimSpace(request.ResumeExecutionID) == "") {
		return ExecuteResult{}, fmt.Errorf("%w: set exactly one of admission or resume execution ID", ErrInvalidExecuteRequest)
	}

	var execution *engineExecution
	var created, atomicAdmission bool
	var err error
	if request.Admission != nil {
		execution, created, atomicAdmission, err = engine.admit(ctx, request.Admission)
	} else {
		execution, err = engine.loadExecution(ctx, strings.TrimSpace(request.ResumeExecutionID), nil)
	}
	if err != nil {
		if execution != nil && execution.record.State == schema.ExecutionBlockedDependency {
			return engineResult(execution.record), err
		}
		if request.RecoveryLease != nil {
			if releaseErr := engine.ledger.FinishExecutionLease(ctx, *request.RecoveryLease, "", err.Error()); releaseErr != nil {
				return ExecuteResult{}, errors.Join(err, fmt.Errorf("%w: %v", ErrDurableDisposition, releaseErr))
			}
		}
		return ExecuteResult{}, err
	}
	execution.mu.Lock()
	defer execution.mu.Unlock()
	if schema.IsTerminalExecutionState(execution.record.State) {
		execution.releaseSnapshot()
		return engineResult(execution.record), nil
	}
	if created && !selectedExecutionHasSteps(execution) {
		if err := engine.persistExecutionState(ctx, execution, schema.ExecutionCompleted, "", request.RecoveryLease); err != nil {
			return engineResult(execution.record), err
		}
		execution.releaseSnapshot()
		return engineResult(execution.record), nil
	}
	if request.WaitMode == WaitAccepted && created && atomicAdmission {
		return engineResult(execution.record), nil
	}
	if execution.unit == nil || execution.unit.CheckedIR == nil {
		return engineResult(execution.record), fmt.Errorf("%w: generation %s", ErrBlockedDependency, execution.record.GenerationDigest)
	}

	if request.WaitMode == WaitTerminal && execution.record.State == schema.ExecutionAccepted && request.RecoveryLease == nil {
		updated, updateErr := engine.ledger.SetExecutionState(ctx, execution.record.ExecutionID, execution.record.Revision, schema.ExecutionRunning, "")
		if updateErr == nil {
			execution.record = updated
		} else if !errors.Is(updateErr, schema.ErrOptimisticConflict) {
			return engineResult(execution.record), updateErr
		}
	}
	workflowErr := engine.runtime.executeCheckedWorkflowMode(ctx, execution.unit, execution.record.TenantNamespace,
		execution.record.ExecutionID, execution.facts, execution.selected, request.WaitMode)
	if workflowErr != nil {
		state, dispositionErr := engine.executionFailureState(ctx, execution)
		if dispositionErr != nil {
			state = execution.record.State
		}
		if persistErr := engine.persistExecutionState(ctx, execution, state, workflowErr.Error(), request.RecoveryLease); persistErr != nil {
			return engineResult(execution.record), errors.Join(workflowErr, dispositionErr, fmt.Errorf("%w: %v", ErrDurableDisposition, persistErr))
		}
		if schema.IsTerminalExecutionState(execution.record.State) {
			execution.releaseSnapshot()
		}
		return engineResult(execution.record), workflowErr
	}
	if request.WaitMode == WaitTerminal {
		if err := engine.persistExecutionState(ctx, execution, schema.ExecutionCompleted, "", request.RecoveryLease); err != nil {
			return engineResult(execution.record), err
		}
		execution.releaseSnapshot()
	}
	return engineResult(execution.record), nil
}

func (execution *engineExecution) releaseSnapshot() {
	if execution != nil && execution.snapshotHandle != nil {
		_ = execution.snapshotHandle.Release()
		execution.snapshotHandle = nil
	}
}

func (engine *Engine) admit(ctx context.Context, admission *Admission) (*engineExecution, bool, bool, error) {
	if admission == nil {
		return nil, false, false, fmt.Errorf("%w: admission is nil", ErrInvalidExecuteRequest)
	}
	admission.ExecutionID = strings.TrimSpace(admission.ExecutionID)
	admission.TenantNamespace = strings.TrimSpace(admission.TenantNamespace)
	admission.AdmissionID = strings.TrimSpace(admission.AdmissionID)
	if admission.ExecutionID == "" {
		return nil, false, false, fmt.Errorf("%w: stable execution ID is required", ErrInvalidExecuteRequest)
	}
	if admission.TenantNamespace == "" {
		return nil, false, false, fmt.Errorf("%w: tenant namespace is required", ErrInvalidExecuteRequest)
	}
	requestHash, err := admissionHash(admission)
	if err != nil {
		return nil, false, false, fmt.Errorf("%w: hash admission: %v", ErrInvalidExecuteRequest, err)
	}
	identity := admission.AdmissionID
	if identity == "" {
		identity = admission.ExecutionID
	}
	if existing, getErr := engine.ledger.GetExecutionByAdmission(ctx, identity); getErr == nil {
		if admission.ExpectedGenerationDigest != "" && admission.ExpectedGenerationDigest != existing.GenerationDigest {
			return nil, false, false, ErrGenerationMismatch
		}
		if existing.ExecutionID != admission.ExecutionID || existing.RequestHash != requestHash {
			return nil, false, false, fmt.Errorf("%w: admission identity %s", ErrIdentityConflict, identity)
		}
		execution, loadErr := engine.loadExecutionRecord(ctx, existing, nil)
		return execution, false, false, loadErr
	} else if !errors.Is(getErr, schema.ErrExecutionNotFound) {
		return nil, false, false, getErr
	}

	engine.runtime.mu.RLock()
	if engine.runtime.state != StateReady {
		state := engine.runtime.state
		engine.runtime.mu.RUnlock()
		return nil, false, false, fmt.Errorf("runtime not ready (state: %s)", state)
	}
	if engine.runtime.activeGeneration == nil || engine.runtime.activeGeneration.unit == nil || engine.runtime.activeGeneration.unit.CheckedIR == nil {
		engine.runtime.mu.RUnlock()
		return nil, false, false, fmt.Errorf("no checked extension workflow is available")
	}
	unit := engine.runtime.activeGeneration.unit
	if engine.runtime.workflowStore == nil {
		engine.runtime.mu.RUnlock()
		return nil, false, false, fmt.Errorf("checked durable workflow outbox is not configured")
	}
	if _, redisOutbox := engine.runtime.workflowStore.(*schema.RedisOutboxStore); redisOutbox {
		engine.runtime.mu.RUnlock()
		return nil, false, false, fmt.Errorf("Redis outbox cannot provide atomic execution admission; use PostgreSQL for production checked admission")
	}
	if err := engine.validateUnitExecutors(unit); err != nil {
		engine.runtime.mu.RUnlock()
		return nil, false, false, err
	}
	unitSnapshot, snapshotErr := extensionSnapshot(unit)
	if snapshotErr != nil {
		engine.runtime.mu.RUnlock()
		return nil, false, false, snapshotErr
	}
	var snapshotHandle *loader.ExtensionSnapshotHandle
	if unitSnapshot != nil {
		snapshotHandle, err = unitSnapshot.Acquire()
		if err != nil {
			engine.runtime.mu.RUnlock()
			return nil, false, false, fmt.Errorf("acquire extension snapshot: %w", err)
		}
	}
	engine.runtime.mu.RUnlock()

	durable, selected, facts, err := buildDurableAdmission(ctx, unit, admission, requestHash)
	if err != nil {
		if snapshotHandle != nil {
			_ = snapshotHandle.Release()
		}
		return nil, false, false, err
	}
	if admission.ExpectedGenerationDigest != "" && admission.ExpectedGenerationDigest != durable.Artifact.GenerationDigest {
		if snapshotHandle != nil {
			_ = snapshotHandle.Release()
		}
		return nil, false, false, ErrGenerationMismatch
	}
	var record schema.ExecutionRecord
	var created bool
	atomicAdmission := false
	if atomicStore, ok := engine.ledger.(schema.AtomicAdmissionStore); ok && any(engine.runtime.workflowStore) == any(atomicStore) {
		record, created, err = atomicStore.AdmitExecutionAtomic(ctx, durable)
		atomicAdmission = true
	} else {
		if err = engine.ledger.PutArtifact(ctx, durable.Artifact); err == nil {
			record, created, err = engine.ledger.AdmitExecution(ctx, durable)
		}
	}
	if err != nil {
		if errors.Is(err, schema.ErrIdentityConflict) || isPostgresConcurrencyError(err) {
			existing, loadErr := engine.ledger.GetExecutionByAdmission(ctx, durable.Execution.AdmissionIdentity)
			if loadErr == nil && existing.RequestHash == durable.Execution.RequestHash && existing.GenerationDigest == durable.Execution.GenerationDigest {
				if snapshotHandle != nil {
					_ = snapshotHandle.Release()
				}
				execution, resolveErr := engine.loadExecutionRecord(ctx, existing, unit)
				return execution, false, atomicAdmission, resolveErr
			}
		}
		if snapshotHandle != nil {
			_ = snapshotHandle.Release()
		}
		if errors.Is(err, schema.ErrIdentityConflict) {
			return nil, false, atomicAdmission, fmt.Errorf("%w: %v", ErrIdentityConflict, err)
		}
		return nil, false, atomicAdmission, err
	}
	if !created {
		if snapshotHandle != nil {
			_ = snapshotHandle.Release()
		}
		execution, loadErr := engine.loadExecutionRecord(ctx, record, unit)
		return execution, false, atomicAdmission, loadErr
	}
	execution := &engineExecution{record: record, facts: facts, selected: selected, unit: unit, snapshotHandle: snapshotHandle}
	engine.mu.Lock()
	engine.executions[record.ExecutionID] = execution
	engine.mu.Unlock()
	return execution, true, atomicAdmission, nil
}

func isPostgresConcurrencyError(err error) bool {
	var value interface{ SQLState() string }
	if errors.As(err, &value) {
		code := value.SQLState()
		return code == "40001" || code == "23505"
	}
	return false
}

func (engine *Engine) validateUnitExecutors(unit *compiler.CompiledUnit) error {
	if engine.runtime.allowLegacyExecution {
		return nil
	}
	for name, compiledVerb := range unit.VerbSpecs {
		local, ok := compiledVerb.ExecutorConfig.(*compiler.LocalExecutorConfig)
		if !ok || local == nil || local.Implementation == nil {
			continue
		}
		if _, aware := any(local.Implementation).(invocation.Executor); !aware {
			return fmt.Errorf("verb %q uses an unrestricted Go continuation; production execution requires an invocation-aware immutable resolver", name)
		}
		if _, described := any(local.Implementation).(invocation.ResolverDescriptorProvider); !described {
			return fmt.Errorf("verb %q is invocation-aware but has no immutable resolver descriptor", name)
		}
	}
	return nil
}

func (engine *Engine) loadExecution(ctx context.Context, id string, preferred *compiler.CompiledUnit) (*engineExecution, error) {
	engine.mu.Lock()
	cached := engine.executions[id]
	engine.mu.Unlock()
	if cached != nil {
		return cached, nil
	}
	record, err := engine.ledger.GetExecution(ctx, id)
	if err != nil {
		if errors.Is(err, schema.ErrExecutionNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrExecutionNotFound, id)
		}
		return nil, err
	}
	return engine.loadExecutionRecord(ctx, record, preferred)
}

func (engine *Engine) loadExecutionRecord(ctx context.Context, record schema.ExecutionRecord, preferred *compiler.CompiledUnit) (*engineExecution, error) {
	engine.mu.Lock()
	cached := engine.executions[record.ExecutionID]
	engine.mu.Unlock()
	if cached != nil {
		cached.mu.Lock()
		generationDigest := cached.record.GenerationDigest
		cached.mu.Unlock()
		if generationDigest == record.GenerationDigest {
			return cached, nil
		}
	}
	facts, err := decodeExecutionFacts(record.EffectiveFacts)
	if err != nil {
		return nil, err
	}
	selected := make(map[string]struct{}, len(record.Plans))
	for _, plan := range record.Plans {
		selected[plan.PlanID] = struct{}{}
	}
	if preferred != nil {
		if artifact, artifactErr := executionArtifactForUnit(preferred); artifactErr == nil && artifact.GenerationDigest == record.GenerationDigest {
			preferredSnapshot, snapshotErr := extensionSnapshot(preferred)
			if snapshotErr != nil {
				return engine.blockDependency(ctx, record, facts, selected, snapshotErr)
			}
			var handle *loader.ExtensionSnapshotHandle
			if preferredSnapshot != nil {
				handle, err = preferredSnapshot.Acquire()
				if err != nil {
					return engine.blockDependency(ctx, record, facts, selected, fmt.Errorf("acquire preferred extension snapshot: %w", err))
				}
			}
			execution := &engineExecution{record: record, facts: facts, selected: selected, unit: preferred, snapshotHandle: handle}
			engine.mu.Lock()
			engine.executions[record.ExecutionID] = execution
			engine.mu.Unlock()
			return execution, nil
		}
	}
	artifact, err := engine.ledger.GetArtifact(ctx, record.GenerationDigest)
	if err != nil {
		return engine.blockDependency(ctx, record, facts, selected, err)
	}
	environment, err := decodeArtifactEnvironment(artifact)
	if err != nil {
		return engine.blockDependency(ctx, record, facts, selected, err)
	}
	checked, err := ir.Parse(artifact.IRBytes, environment, ir.Limits{})
	if err != nil || checked.Digest() != artifact.IRDigest {
		return engine.blockDependency(ctx, record, facts, selected, fmt.Errorf("checked artifact digest mismatch: %w", err))
	}
	resolver := engine.resolver
	if resolver == nil {
		engine.runtime.mu.RLock()
		var current *compiler.CompiledUnit
		if engine.runtime.activeGeneration != nil {
			current = engine.runtime.activeGeneration.unit
		}
		engine.runtime.mu.RUnlock()
		if current != nil {
			if currentArtifact, currentErr := executionArtifactForUnit(current); currentErr == nil && currentArtifact.GenerationDigest == record.GenerationDigest {
				resolver = ArtifactResolverFunc(func(context.Context, schema.ExecutionArtifact, *ir.Checked) (*compiler.CompiledUnit, error) {
					return current, nil
				})
			}
		}
	}
	if resolver == nil {
		return engine.blockDependency(ctx, record, facts, selected, fmt.Errorf("no immutable artifact resolver is configured"))
	}
	unit, err := resolver.ResolveArtifact(ctx, artifact, checked)
	if err != nil {
		return engine.blockDependency(ctx, record, facts, selected, err)
	}
	unitSnapshot, snapshotErr := extensionSnapshot(unit)
	if snapshotErr != nil {
		return engine.blockDependency(ctx, record, facts, selected, snapshotErr)
	}
	var snapshotHandle *loader.ExtensionSnapshotHandle
	if unitSnapshot != nil {
		snapshotHandle, err = unitSnapshot.Acquire()
		if err != nil {
			return engine.blockDependency(ctx, record, facts, selected, fmt.Errorf("acquire resolved extension snapshot: %w", err))
		}
		if unit.ExecutionOwnedSnapshot {
			if retireErr := unitSnapshot.Retire(); retireErr != nil {
				_ = snapshotHandle.Release()
				return engine.blockDependency(ctx, record, facts, selected, fmt.Errorf("retire execution-owned snapshot: %w", retireErr))
			}
		}
	}
	blockResolved := func(cause error) (*engineExecution, error) {
		if snapshotHandle != nil {
			_ = snapshotHandle.Release()
		}
		return engine.blockDependency(ctx, record, facts, selected, cause)
	}
	if unit == nil || unit.CheckedIR == nil || unit.CheckedIR.Digest() != checked.Digest() {
		return blockResolved(fmt.Errorf("resolver returned a mismatched checked generation"))
	}
	resolvedArtifact, resolvedErr := executionArtifactForUnit(unit)
	if resolvedErr != nil || resolvedArtifact.GenerationDigest != record.GenerationDigest {
		return blockResolved(fmt.Errorf("resolver executor/function manifest does not match pinned generation: %v", resolvedErr))
	}
	if err := engine.validateUnitExecutors(unit); err != nil {
		return blockResolved(err)
	}
	for _, plan := range checked.CloneArtifact().Plans {
		for _, step := range plan.Steps {
			if unit.VerbSpecs[step.Verb] == nil {
				return blockResolved(fmt.Errorf("verb contract %q is unavailable", step.Verb))
			}
		}
	}
	execution := &engineExecution{record: record, facts: facts, selected: selected, unit: unit, snapshotHandle: snapshotHandle}
	engine.mu.Lock()
	engine.executions[record.ExecutionID] = execution
	engine.mu.Unlock()
	return execution, nil
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
	execution := &engineExecution{record: record, facts: facts, selected: selected}
	engine.mu.Lock()
	engine.executions[record.ExecutionID] = execution
	engine.mu.Unlock()
	return execution, fmt.Errorf("%w: %v", ErrBlockedDependency, cause)
}

func (engine *Engine) persistExecutionState(ctx context.Context, execution *engineExecution, state schema.ExecutionState, message string, lease *schema.ExecutionLease) error {
	if lease != nil {
		leaseState := state
		if !schema.IsTerminalExecutionState(state) {
			leaseState = "" // release the CAS lease without terminalizing recoverable work
		}
		if err := engine.ledger.FinishExecutionLease(ctx, *lease, leaseState, message); err != nil {
			return fmt.Errorf("%w: %v", ErrDurableDisposition, err)
		}
		updated, err := engine.ledger.GetExecution(ctx, execution.record.ExecutionID)
		if err != nil {
			return fmt.Errorf("%w: reload execution after lease completion: %v", ErrDurableDisposition, err)
		}
		execution.record = updated
		return nil
	}
	updated, err := engine.ledger.SetExecutionState(ctx, execution.record.ExecutionID, execution.record.Revision, state, message)
	if err != nil {
		return err
	}
	execution.record = updated
	return nil
}

func (engine *Engine) executionFailureState(ctx context.Context, execution *engineExecution) (schema.ExecutionState, error) {
	state := execution.record.State
	for _, plan := range execution.record.Plans {
		saga, err := engine.runtime.workflowStore.GetSaga(ctx, plan.SagaID)
		if err != nil {
			return state, fmt.Errorf("read durable saga disposition %s: %w", plan.SagaID, err)
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
	// Running, compensating, queued, and unreadable work is recoverable. Never
	// infer a terminal disposition from the transient error returned by a store.
	return state, nil
}

func selectedExecutionHasSteps(execution *engineExecution) bool {
	if execution == nil || execution.unit == nil || execution.unit.CheckedIR == nil {
		return false
	}
	for _, plan := range execution.unit.CheckedIR.CloneArtifact().Plans {
		if _, selected := execution.selected[plan.Id]; selected && len(plan.Steps) != 0 {
			return true
		}
	}
	return false
}

func engineResult(record schema.ExecutionRecord) ExecuteResult {
	return ExecuteResult{ExecutionID: record.ExecutionID, GenerationDigest: record.GenerationDigest, State: string(record.State), DurablyAccepted: record.State != schema.ExecutionAdmitting, Completed: record.State == schema.ExecutionCompleted}
}

func decodeExecutionFacts(data json.RawMessage) (map[string]any, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	var facts map[string]any
	if err := decoder.Decode(&facts); err != nil {
		return nil, fmt.Errorf("decode frozen execution facts: %w", err)
	}
	return facts, nil
}

func admissionHash(admission *Admission) (string, error) {
	facts, err := canonicalJSONValue(admission.Facts)
	if err != nil {
		return "", err
	}
	payload := struct {
		Namespace   string          `json:"namespace"`
		Ruleset     string          `json:"ruleset"`
		Version     string          `json:"version"`
		MergePolicy string          `json:"merge_policy"`
		Facts       json.RawMessage `json:"facts"`
	}{
		Namespace: admission.TenantNamespace, Ruleset: admission.Ruleset,
		Version: admission.Version, MergePolicy: admission.MergePolicy, Facts: facts,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func canonicalJSONValue(value any) ([]byte, error) {
	// encoding/json orders string map keys. Normalize recursively to reject
	// unsupported values and non-finite numbers before identity is persisted.
	normalized, err := normalizeAdmissionValue(value)
	if err != nil {
		return nil, err
	}
	return json.Marshal(normalized)
}

func normalizeAdmissionValue(value any) (any, error) {
	switch value := value.(type) {
	case nil, bool, string, json.Number, float64, float32,
		int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		data, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		var normalized any
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		decoder.UseNumber()
		if err := decoder.Decode(&normalized); err != nil {
			return nil, err
		}
		return normalized, nil
	case map[string]any:
		names := make([]string, 0, len(value))
		for name := range value {
			names = append(names, name)
		}
		sort.Strings(names)
		result := make(map[string]any, len(value))
		for _, name := range names {
			normalized, err := normalizeAdmissionValue(value[name])
			if err != nil {
				return nil, fmt.Errorf("fact %q: %w", name, err)
			}
			result[name] = normalized
		}
		return result, nil
	case []any:
		result := make([]any, len(value))
		for index, item := range value {
			normalized, err := normalizeAdmissionValue(item)
			if err != nil {
				return nil, fmt.Errorf("fact item %d: %w", index, err)
			}
			result[index] = normalized
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported value type %T", value)
	}
}
