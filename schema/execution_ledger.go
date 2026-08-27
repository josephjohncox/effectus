package schema

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrArtifactNotFound    = errors.New("execution artifact not found")
	ErrExecutionNotFound   = errors.New("execution not found")
	ErrStaleExecutionLease = errors.New("stale execution recovery lease")
)

// ExecutionState is the durable top-level state shared by admission and recovery.
type ExecutionState string

const (
	ExecutionAdmitting           ExecutionState = "admitting"
	ExecutionAccepted            ExecutionState = "accepted"
	ExecutionRunning             ExecutionState = "running"
	ExecutionCompleted           ExecutionState = "completed"
	ExecutionFailed              ExecutionState = "failed"
	ExecutionBlockedUnknown      ExecutionState = "blocked_unknown"
	ExecutionBlockedFence        ExecutionState = "blocked_fence"
	ExecutionBlockedDependency   ExecutionState = "blocked_dependency"
	ExecutionBlockedCompensation ExecutionState = "blocked_compensation"
)

func IsTerminalExecutionState(state ExecutionState) bool {
	switch state {
	case ExecutionCompleted, ExecutionFailed, ExecutionBlockedUnknown, ExecutionBlockedFence,
		ExecutionBlockedDependency, ExecutionBlockedCompensation:
		return true
	default:
		return false
	}
}

// ExecutionArtifact stores every immutable input needed to reconstruct a generation.
type ExecutionArtifact struct {
	GenerationDigest string
	IRDigest         string
	IRBytes          []byte
	Environment      json.RawMessage
	ExecutorManifest json.RawMessage
	FunctionManifest json.RawMessage
	SourceDigest     string
	CompilerMetadata json.RawMessage
	CreatedAt        time.Time
}

// ExecutionPlanRecord pins one selected checked plan to its durable saga.
type ExecutionPlanRecord struct {
	ExecutionID string
	PlanID      string
	SagaID      string
	Ordinal     int
	State       string
}

// ExecutionRecord is the frozen result of one admission transaction.
type ExecutionRecord struct {
	ExecutionID       string
	AdmissionIdentity string
	RequestHash       string
	Ruleset           string
	Version           string
	TenantNamespace   string
	MergePolicy       string
	GenerationDigest  string
	EffectiveFacts    json.RawMessage
	State             ExecutionState
	Revision          uint64
	RecoveryOwner     string
	RecoveryToken     string
	RecoveryDeadline  time.Time
	LastError         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	Plans             []ExecutionPlanRecord
}

// FactApplication identifies the exactly-once fact event used by admission.
type FactApplication struct {
	ExecutionID     string
	FactEventID     string
	MergePolicy     string
	Facts           json.RawMessage
	AppliedRevision uint64
}

// DurableAdmission is the transport-neutral content of an admission transaction.
type DurableAdmission struct {
	Artifact        ExecutionArtifact
	Execution       ExecutionRecord
	FactApplication FactApplication
	Plans           []ExecutionPlanRecord
	Sagas           []CreateSagaRequest
	InitialSteps    []EnqueueStepRequest
}

// ExecutionLease is a CAS capability for one recovery attempt.
type ExecutionLease struct {
	ExecutionID string
	Owner       string
	Token       string
	Deadline    time.Time
	Revision    uint64
}

// ExecutionLedger persists artifacts, admission identity, frozen facts, plans,
// dispositions, and recovery leases.
type ExecutionLedger interface {
	PutArtifact(context.Context, ExecutionArtifact) error
	GetArtifact(context.Context, string) (ExecutionArtifact, error)
	AdmitExecution(context.Context, DurableAdmission) (ExecutionRecord, bool, error)
	GetExecution(context.Context, string) (ExecutionRecord, error)
	GetExecutionByAdmission(context.Context, string) (ExecutionRecord, error)
	SetExecutionState(context.Context, string, uint64, ExecutionState, string) (ExecutionRecord, error)
	LeaseExecutions(context.Context, string, int, time.Duration) ([]ExecutionLease, error)
	FinishExecutionLease(context.Context, ExecutionLease, ExecutionState, string) error
}

// AtomicAdmissionStore atomically commits the ledger, selected sagas, and first
// dispatch intents in one backend transaction.
type AtomicAdmissionStore interface {
	ExecutionLedger
	AdmitExecutionAtomic(context.Context, DurableAdmission) (ExecutionRecord, bool, error)
}

// InMemoryExecutionLedger provides deterministic development semantics. It is
// not durable and cannot make admission atomic with a separate OutboxStore.
type InMemoryExecutionLedger struct {
	mu          sync.Mutex
	artifacts   map[string]ExecutionArtifact
	executions  map[string]*ExecutionRecord
	byAdmission map[string]string
	facts       map[string]map[string]FactApplication
	now         func() time.Time
}

func NewInMemoryExecutionLedger() *InMemoryExecutionLedger {
	return &InMemoryExecutionLedger{
		artifacts: make(map[string]ExecutionArtifact), executions: make(map[string]*ExecutionRecord),
		byAdmission: make(map[string]string), facts: make(map[string]map[string]FactApplication), now: time.Now,
	}
}

func (store *InMemoryExecutionLedger) PutArtifact(ctx context.Context, artifact ExecutionArtifact) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateExecutionArtifact(artifact); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if existing, ok := store.artifacts[artifact.GenerationDigest]; ok {
		if !sameExecutionArtifact(existing, artifact) {
			return fmt.Errorf("%w: generation artifact %s", ErrIdentityConflict, artifact.GenerationDigest)
		}
		return nil
	}
	artifact.CreatedAt = store.now().UTC()
	store.artifacts[artifact.GenerationDigest] = cloneExecutionArtifact(artifact)
	return nil
}

func (store *InMemoryExecutionLedger) GetArtifact(ctx context.Context, digest string) (ExecutionArtifact, error) {
	if err := ctx.Err(); err != nil {
		return ExecutionArtifact{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	artifact, ok := store.artifacts[digest]
	if !ok {
		return ExecutionArtifact{}, fmt.Errorf("%w: %s", ErrArtifactNotFound, digest)
	}
	return cloneExecutionArtifact(artifact), nil
}

func (store *InMemoryExecutionLedger) AdmitExecution(ctx context.Context, admission DurableAdmission) (ExecutionRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return ExecutionRecord{}, false, err
	}
	if err := validateDurableAdmission(admission); err != nil {
		return ExecutionRecord{}, false, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if existingID := store.byAdmission[admission.Execution.AdmissionIdentity]; existingID != "" {
		existing := store.executions[existingID]
		if existing.RequestHash != admission.Execution.RequestHash || existing.ExecutionID != admission.Execution.ExecutionID {
			return ExecutionRecord{}, false, fmt.Errorf("%w: admission identity %s", ErrIdentityConflict, admission.Execution.AdmissionIdentity)
		}
		return cloneExecutionRecord(*existing), false, nil
	}
	if existing := store.executions[admission.Execution.ExecutionID]; existing != nil {
		if existing.AdmissionIdentity != admission.Execution.AdmissionIdentity || existing.RequestHash != admission.Execution.RequestHash {
			return ExecutionRecord{}, false, fmt.Errorf("%w: execution %s", ErrIdentityConflict, admission.Execution.ExecutionID)
		}
		return cloneExecutionRecord(*existing), false, nil
	}
	artifact, ok := store.artifacts[admission.Artifact.GenerationDigest]
	if !ok || !sameExecutionArtifact(artifact, admission.Artifact) {
		return ExecutionRecord{}, false, fmt.Errorf("%w: generation artifact %s", ErrIdentityConflict, admission.Artifact.GenerationDigest)
	}
	now := store.now().UTC()
	record := admission.Execution
	record.State = ExecutionAccepted
	record.Revision = 1
	record.CreatedAt, record.UpdatedAt = now, now
	record.Plans = append([]ExecutionPlanRecord(nil), admission.Plans...)
	store.executions[record.ExecutionID] = &record
	store.byAdmission[record.AdmissionIdentity] = record.ExecutionID
	store.facts[record.ExecutionID] = map[string]FactApplication{admission.FactApplication.FactEventID: cloneFactApplication(admission.FactApplication)}
	return cloneExecutionRecord(record), true, nil
}

func (store *InMemoryExecutionLedger) GetExecutionByAdmission(ctx context.Context, identity string) (ExecutionRecord, error) {
	if err := ctx.Err(); err != nil {
		return ExecutionRecord{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	id := store.byAdmission[identity]
	if id == "" || store.executions[id] == nil {
		return ExecutionRecord{}, fmt.Errorf("%w: admission %s", ErrExecutionNotFound, identity)
	}
	return cloneExecutionRecord(*store.executions[id]), nil
}

func (store *InMemoryExecutionLedger) GetExecution(ctx context.Context, id string) (ExecutionRecord, error) {
	if err := ctx.Err(); err != nil {
		return ExecutionRecord{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	record := store.executions[id]
	if record == nil {
		return ExecutionRecord{}, fmt.Errorf("%w: %s", ErrExecutionNotFound, id)
	}
	return cloneExecutionRecord(*record), nil
}

func (store *InMemoryExecutionLedger) SetExecutionState(ctx context.Context, id string, revision uint64, state ExecutionState, message string) (ExecutionRecord, error) {
	if err := ctx.Err(); err != nil {
		return ExecutionRecord{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	record := store.executions[id]
	if record == nil {
		return ExecutionRecord{}, fmt.Errorf("%w: %s", ErrExecutionNotFound, id)
	}
	if record.Revision != revision {
		return ExecutionRecord{}, ErrOptimisticConflict
	}
	record.State, record.LastError = state, message
	for index := range record.Plans {
		record.Plans[index].State = executionPlanDisposition(state)
	}
	record.Revision++
	record.UpdatedAt = store.now().UTC()
	return cloneExecutionRecord(*record), nil
}

func (store *InMemoryExecutionLedger) LeaseExecutions(ctx context.Context, owner string, limit int, duration time.Duration) ([]ExecutionLease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(owner) == "" || limit <= 0 || duration <= 0 {
		return nil, fmt.Errorf("recovery owner, positive limit, and lease duration are required")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	now := store.now().UTC()
	ids := make([]string, 0)
	for id, record := range store.executions {
		if IsTerminalExecutionState(record.State) {
			continue
		}
		if record.RecoveryToken == "" || !record.RecoveryDeadline.After(now) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	leases := make([]ExecutionLease, 0, len(ids))
	for _, id := range ids {
		token, err := executionLeaseToken()
		if err != nil {
			return nil, err
		}
		record := store.executions[id]
		record.RecoveryOwner, record.RecoveryToken, record.RecoveryDeadline = owner, token, now.Add(duration)
		record.Revision++
		record.UpdatedAt = now
		leases = append(leases, ExecutionLease{ExecutionID: id, Owner: owner, Token: token, Deadline: record.RecoveryDeadline, Revision: record.Revision})
	}
	return leases, nil
}

func (store *InMemoryExecutionLedger) FinishExecutionLease(ctx context.Context, lease ExecutionLease, state ExecutionState, message string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	record := store.executions[lease.ExecutionID]
	if record == nil {
		return fmt.Errorf("%w: %s", ErrExecutionNotFound, lease.ExecutionID)
	}
	if record.RecoveryOwner != lease.Owner || record.RecoveryToken != lease.Token || record.Revision != lease.Revision {
		return ErrStaleExecutionLease
	}
	if state != "" {
		record.State = state
		for index := range record.Plans {
			record.Plans[index].State = executionPlanDisposition(state)
		}
	}
	record.LastError = message
	record.RecoveryOwner, record.RecoveryToken, record.RecoveryDeadline = "", "", time.Time{}
	record.Revision++
	record.UpdatedAt = store.now().UTC()
	return nil
}

func executionPlanDisposition(state ExecutionState) string {
	switch state {
	case ExecutionCompleted:
		return "completed"
	case ExecutionRunning:
		return "running"
	case ExecutionFailed:
		return "failed"
	case ExecutionBlockedUnknown, ExecutionBlockedFence, ExecutionBlockedDependency, ExecutionBlockedCompensation:
		return "blocked"
	default:
		return "selected"
	}
}

func validateExecutionArtifact(artifact ExecutionArtifact) error {
	for name, value := range map[string]string{"generation_digest": artifact.GenerationDigest, "ir_digest": artifact.IRDigest, "source_digest": artifact.SourceDigest} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("artifact %s is required", name)
		}
	}
	if len(artifact.IRBytes) == 0 || !json.Valid(artifact.Environment) || !json.Valid(artifact.ExecutorManifest) || !json.Valid(artifact.FunctionManifest) || !json.Valid(artifact.CompilerMetadata) {
		return fmt.Errorf("artifact bytes and canonical JSON manifests are required")
	}
	return nil
}

func validateDurableAdmission(admission DurableAdmission) error {
	if err := validateExecutionArtifact(admission.Artifact); err != nil {
		return err
	}
	record := admission.Execution
	for name, value := range map[string]string{"execution_id": record.ExecutionID, "admission_identity": record.AdmissionIdentity, "request_hash": record.RequestHash, "generation_digest": record.GenerationDigest, "tenant_namespace": record.TenantNamespace, "ruleset": record.Ruleset, "version": record.Version} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("execution %s is required", name)
		}
	}
	if record.GenerationDigest != admission.Artifact.GenerationDigest || !json.Valid(record.EffectiveFacts) {
		return fmt.Errorf("execution artifact or effective facts are invalid")
	}
	if admission.FactApplication.ExecutionID != record.ExecutionID || admission.FactApplication.FactEventID == "" || !json.Valid(admission.FactApplication.Facts) {
		return fmt.Errorf("fact application is invalid")
	}
	for index, plan := range admission.Plans {
		if plan.ExecutionID != record.ExecutionID || plan.Ordinal != index || plan.PlanID == "" || plan.SagaID != StableSagaID(record.ExecutionID, plan.PlanID) {
			return fmt.Errorf("execution plan %d is invalid", index)
		}
	}
	return nil
}

func sameExecutionArtifact(left, right ExecutionArtifact) bool {
	return left.GenerationDigest == right.GenerationDigest && left.IRDigest == right.IRDigest && bytes.Equal(left.IRBytes, right.IRBytes) &&
		jsonSemanticallyEqual(left.Environment, right.Environment) && jsonSemanticallyEqual(left.ExecutorManifest, right.ExecutorManifest) &&
		jsonSemanticallyEqual(left.FunctionManifest, right.FunctionManifest) && left.SourceDigest == right.SourceDigest && jsonSemanticallyEqual(left.CompilerMetadata, right.CompilerMetadata)
}

func jsonSemanticallyEqual(left, right []byte) bool {
	var leftValue, rightValue any
	leftDecoder := json.NewDecoder(bytes.NewReader(left))
	leftDecoder.UseNumber()
	rightDecoder := json.NewDecoder(bytes.NewReader(right))
	rightDecoder.UseNumber()
	if leftDecoder.Decode(&leftValue) != nil || rightDecoder.Decode(&rightValue) != nil {
		return false
	}
	leftCanonical, leftErr := json.Marshal(leftValue)
	rightCanonical, rightErr := json.Marshal(rightValue)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftCanonical, rightCanonical)
}
func cloneExecutionArtifact(value ExecutionArtifact) ExecutionArtifact {
	value.IRBytes = append([]byte(nil), value.IRBytes...)
	value.Environment = append(json.RawMessage(nil), value.Environment...)
	value.ExecutorManifest = append(json.RawMessage(nil), value.ExecutorManifest...)
	value.FunctionManifest = append(json.RawMessage(nil), value.FunctionManifest...)
	value.CompilerMetadata = append(json.RawMessage(nil), value.CompilerMetadata...)
	return value
}
func cloneExecutionRecord(value ExecutionRecord) ExecutionRecord {
	value.EffectiveFacts = append(json.RawMessage(nil), value.EffectiveFacts...)
	value.Plans = append([]ExecutionPlanRecord(nil), value.Plans...)
	return value
}
func cloneFactApplication(value FactApplication) FactApplication {
	value.Facts = append(json.RawMessage(nil), value.Facts...)
	return value
}
func executionLeaseToken() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(data[:]), nil
}
