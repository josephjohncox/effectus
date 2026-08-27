package schema

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/effectus/effectus-go/invocation"
)

// InMemoryOutboxStore implements the V2 protocol for tests and single-process
// development. Its data and fencing counters do not survive a process restart.
type InMemoryOutboxStore struct {
	mu         sync.Mutex
	sagas      map[string]*SagaInstance
	steps      map[string]map[string]*SagaStep
	dispatches map[string]*Dispatch
	attempts   map[string][]DispatchAttempt
	now        func() time.Time
}

func NewInMemoryOutboxStore() *InMemoryOutboxStore {
	return &InMemoryOutboxStore{
		sagas:      make(map[string]*SagaInstance),
		steps:      make(map[string]map[string]*SagaStep),
		dispatches: make(map[string]*Dispatch),
		attempts:   make(map[string][]DispatchAttempt),
		now:        time.Now,
	}
}

func (store *InMemoryOutboxStore) CreateSaga(ctx context.Context, request CreateSagaRequest) (*SagaInstance, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateSagaRequest(request); err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if existing := store.sagas[request.SagaID]; existing != nil {
		if existing.Namespace != request.Namespace || existing.ExecutionID != request.ExecutionID ||
			existing.PlanID != request.PlanID || existing.PlanDigest != request.PlanDigest || existing.Serial != request.Serial {
			return nil, fmt.Errorf("%w: saga %s", ErrIdentityConflict, request.SagaID)
		}
		return cloneSaga(existing), nil
	}
	now := store.now().UTC()
	saga := &SagaInstance{
		Namespace: request.Namespace, SagaID: request.SagaID, ExecutionID: request.ExecutionID,
		PlanID: request.PlanID, PlanDigest: request.PlanDigest, State: SagaRunning,
		Serial: request.Serial, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	store.sagas[request.SagaID] = saga
	store.steps[request.SagaID] = make(map[string]*SagaStep)
	return cloneSaga(saga), nil
}

func (store *InMemoryOutboxStore) EnqueueStep(ctx context.Context, request EnqueueStepRequest) (*Dispatch, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	request, arguments, argumentHash, compensationArguments, compensationHash, err := normalizeEnqueue(request)
	if err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	saga := store.sagas[request.SagaID]
	if saga == nil {
		return nil, fmt.Errorf("saga not found: %s", request.SagaID)
	}
	idempotencyKey := IdempotencyKey(saga.Namespace, saga.SagaID, request.EffectID, invocation.DirectionForward)
	dispatchID := "dispatch/" + idempotencyKey
	if existing := store.dispatches[dispatchID]; existing != nil {
		step := store.steps[request.SagaID][request.EffectID]
		if step != nil && sameStep(step, request, argumentHash, compensationHash) {
			return cloneDispatch(existing), nil
		}
		return nil, fmt.Errorf("%w: saga %s effect %s", ErrIdentityConflict, request.SagaID, request.EffectID)
	}
	if isTerminalSaga(saga.State) {
		return nil, fmt.Errorf("%w: saga %s is %s", ErrTerminalSaga, saga.SagaID, saga.State)
	}
	if saga.State != SagaRunning {
		return nil, fmt.Errorf("%w: cannot enqueue a forward step while saga %s is %s", ErrInvalidTransition, saga.SagaID, saga.State)
	}
	if request.Sequence != len(store.steps[request.SagaID])+1 {
		return nil, fmt.Errorf("%w: saga %s step sequence is %d, want dense sequence %d",
			ErrIdentityConflict, request.SagaID, request.Sequence, len(store.steps[request.SagaID])+1)
	}
	step := &SagaStep{
		SagaID: request.SagaID, EffectID: request.EffectID, Sequence: request.Sequence,
		Verb: request.Verb, ContractHash: request.ContractHash,
		Arguments: arguments, ArgumentHash: argumentHash,
		CompensationVerb: request.CompensationVerb, CompensationContract: request.CompensationContract,
		CompensationArguments: compensationArguments, CompensationArgumentHash: compensationHash,
		Fencing: append([]FencingRequirement(nil), request.Fencing...), State: StepPending,
	}
	store.steps[request.SagaID][request.EffectID] = step
	now := store.now().UTC()
	dispatch := &Dispatch{
		ID: dispatchID, SagaID: saga.SagaID, EffectID: step.EffectID, Sequence: step.Sequence,
		Direction: invocation.DirectionForward, Verb: step.Verb, ContractHash: step.ContractHash,
		Arguments: append(json.RawMessage(nil), step.Arguments...), ArgumentHash: step.ArgumentHash,
		IdempotencyKey: idempotencyKey, State: DispatchQueued, Fencing: request.Fencing,
		Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	store.dispatches[dispatch.ID] = dispatch
	saga.Revision++
	saga.UpdatedAt = now
	return cloneDispatch(dispatch), nil
}

func (store *InMemoryOutboxStore) ClaimDispatch(ctx context.Context, options ClaimOptions) (*Dispatch, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if options.Owner == "" || options.LeaseDuration <= 0 {
		return nil, fmt.Errorf("claim owner and positive lease duration are required")
	}
	now := options.Now.UTC()
	if now.IsZero() {
		now = store.now().UTC()
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	eligible := make([]*Dispatch, 0)
	for _, dispatch := range store.dispatches {
		if options.TargetDispatchID != "" && dispatch.ID != options.TargetDispatchID {
			continue
		}
		saga := store.sagas[dispatch.SagaID]
		if saga == nil || isTerminalSaga(saga.State) {
			continue
		}
		if saga.State == SagaCompensating && dispatch.Direction != invocation.DirectionCompensation {
			continue
		}
		if saga.State == SagaRunning && dispatch.Direction != invocation.DirectionForward {
			continue
		}
		switch dispatch.State {
		case DispatchQueued:
			eligible = append(eligible, dispatch)
		case DispatchRetryWait:
			if !dispatch.NextAttemptAt.After(now) {
				eligible = append(eligible, dispatch)
			}
		case DispatchInFlight:
			if !dispatch.LeaseDeadline.After(now) {
				eligible = append(eligible, dispatch)
			}
		}
	}
	sort.Slice(eligible, func(i, j int) bool {
		if !eligible[i].CreatedAt.Equal(eligible[j].CreatedAt) {
			return eligible[i].CreatedAt.Before(eligible[j].CreatedAt)
		}
		if eligible[i].SagaID != eligible[j].SagaID {
			return eligible[i].SagaID < eligible[j].SagaID
		}
		if eligible[i].Sequence != eligible[j].Sequence {
			return eligible[i].Sequence < eligible[j].Sequence
		}
		return eligible[i].ID < eligible[j].ID
	})
	for _, dispatch := range eligible {
		saga := store.sagas[dispatch.SagaID]
		if saga.Serial && store.hasOtherInFlightLocked(dispatch.SagaID, dispatch.ID, now) {
			continue
		}
		token, err := randomLeaseToken()
		if err != nil {
			return nil, err
		}
		dispatch.State = DispatchInFlight
		dispatch.Attempt++
		dispatch.LeaseOwner = options.Owner
		dispatch.LeaseToken = token
		dispatch.LeaseDeadline = now.Add(options.LeaseDuration)
		dispatch.FencingGrants = nil
		dispatch.Revision++
		dispatch.UpdatedAt = now
		store.attempts[dispatch.ID] = append(store.attempts[dispatch.ID], DispatchAttempt{
			DispatchID: dispatch.ID, Attempt: dispatch.Attempt, LeaseOwner: dispatch.LeaseOwner,
			LeaseToken: token, LeaseDeadline: dispatch.LeaseDeadline, StartedAt: now,
		})
		return cloneDispatch(dispatch), nil
	}
	return nil, ErrNoDispatch
}

func (store *InMemoryOutboxStore) SaveFencingGrants(ctx context.Context, dispatchID string, attempt uint64, leaseToken string, grants []invocation.FencingGrant) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateGrants(grants); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	dispatch := store.dispatches[dispatchID]
	if !currentLeaseAt(dispatch, attempt, leaseToken, store.now().UTC()) {
		return ErrStaleLease
	}
	if len(dispatch.Fencing) != len(grants) {
		return fmt.Errorf("fencing grant count does not match dispatch requirements")
	}
	for index, requirement := range dispatch.Fencing {
		if grants[index].Authority != requirement.Authority || grants[index].Resource != requirement.Resource {
			return fmt.Errorf("fencing grant %d does not match dispatch requirement", index)
		}
	}
	if len(dispatch.FencingGrants) != 0 && !sameGrants(dispatch.FencingGrants, grants) {
		return fmt.Errorf("%w: fencing grants already persisted", ErrIdentityConflict)
	}
	dispatch.FencingGrants = append([]invocation.FencingGrant(nil), grants...)
	dispatch.Revision++
	dispatch.UpdatedAt = store.now().UTC()
	attempts := store.attempts[dispatchID]
	attempts[len(attempts)-1].FencingGrants = append([]invocation.FencingGrant(nil), grants...)
	store.attempts[dispatchID] = attempts
	return nil
}

func (store *InMemoryOutboxStore) CompleteDispatch(ctx context.Context, completion Completion) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if completion.Now.IsZero() {
		completion.Now = store.now().UTC()
	} else {
		completion.Now = completion.Now.UTC()
	}
	if err := validateCompletion(completion); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	dispatch := store.dispatches[completion.DispatchID]
	if !currentLeaseAt(dispatch, completion.Attempt, completion.LeaseToken, store.now().UTC()) {
		return ErrStaleLease
	}
	saga := store.sagas[dispatch.SagaID]
	step := store.steps[dispatch.SagaID][dispatch.EffectID]
	if saga == nil || step == nil {
		return fmt.Errorf("dispatch dependencies are missing")
	}
	if err := applyCompletion(dispatch, step, saga, completion); err != nil {
		return err
	}
	attempts := store.attempts[dispatch.ID]
	attempts[len(attempts)-1].Outcome = completion.Outcome
	attempts[len(attempts)-1].Error = completion.Error
	attempts[len(attempts)-1].CompletedAt = completion.Now
	store.attempts[dispatch.ID] = attempts
	if dispatch.Direction == invocation.DirectionForward && (completion.Outcome == invocation.OutcomePermanentFailure ||
		(completion.Outcome == invocation.OutcomeRetryableKnownNotCommitted && completion.Exhausted)) {
		if err := store.startCompensationLocked(saga, completion.Now); err != nil {
			return err
		}
	}
	if dispatch.Direction == invocation.DirectionCompensation && completion.Outcome == invocation.OutcomeSuccess {
		if err := store.enqueueNextCompensationLocked(saga, completion.Now); err != nil {
			return err
		}
	}
	return nil
}

func (store *InMemoryOutboxStore) CompleteSaga(ctx context.Context, sagaID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	saga := store.sagas[sagaID]
	if saga == nil {
		return fmt.Errorf("saga not found: %s", sagaID)
	}
	if saga.State == SagaCompleted {
		return nil
	}
	if saga.State != SagaRunning {
		return fmt.Errorf("%w: cannot complete saga from %s", ErrInvalidTransition, saga.State)
	}
	for _, dispatch := range store.dispatches {
		if dispatch.SagaID == sagaID && dispatch.State != DispatchSucceeded {
			return fmt.Errorf("%w: dispatch %s is %s", ErrInvalidTransition, dispatch.ID, dispatch.State)
		}
	}
	now := store.now().UTC()
	saga.State = SagaCompleted
	saga.Revision++
	saga.UpdatedAt = now
	return nil
}

func (store *InMemoryOutboxStore) GetSaga(ctx context.Context, sagaID string) (*SagaInstance, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	saga := store.sagas[sagaID]
	if saga == nil {
		return nil, fmt.Errorf("saga not found: %s", sagaID)
	}
	return cloneSaga(saga), nil
}

func (store *InMemoryOutboxStore) GetDispatch(ctx context.Context, dispatchID string) (*Dispatch, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	dispatch := store.dispatches[dispatchID]
	if dispatch == nil {
		return nil, fmt.Errorf("dispatch not found: %s", dispatchID)
	}
	return cloneDispatch(dispatch), nil
}

func (store *InMemoryOutboxStore) ListDispatches(ctx context.Context, sagaID string) ([]*Dispatch, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	result := make([]*Dispatch, 0)
	for _, dispatch := range store.dispatches {
		if dispatch.SagaID == sagaID {
			result = append(result, cloneDispatch(dispatch))
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Sequence != result[j].Sequence {
			return result[i].Sequence < result[j].Sequence
		}
		return result[i].Direction < result[j].Direction
	})
	return result, nil
}

func (store *InMemoryOutboxStore) ListAttempts(ctx context.Context, dispatchID string) ([]DispatchAttempt, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	attempts := store.attempts[dispatchID]
	result := make([]DispatchAttempt, len(attempts))
	copy(result, attempts)
	for index := range result {
		result[index].FencingGrants = append([]invocation.FencingGrant(nil), attempts[index].FencingGrants...)
	}
	return result, nil
}

func (store *InMemoryOutboxStore) hasOtherInFlightLocked(sagaID, dispatchID string, now time.Time) bool {
	for _, dispatch := range store.dispatches {
		if dispatch.ID != dispatchID && dispatch.SagaID == sagaID && dispatch.State == DispatchInFlight && dispatch.LeaseDeadline.After(now) {
			return true
		}
	}
	return false
}

func (store *InMemoryOutboxStore) startCompensationLocked(saga *SagaInstance, now time.Time) error {
	candidate := store.compensationCandidateLocked(saga.SagaID)
	if candidate == nil {
		if store.hasSucceededStepLocked(saga.SagaID) {
			saga.State = SagaBlockedCompensation
		} else {
			saga.State = SagaFailed
		}
		saga.Revision++
		saga.UpdatedAt = now
		return nil
	}
	saga.State = SagaCompensating
	saga.Revision++
	saga.UpdatedAt = now
	return store.enqueueCompensationLocked(saga, candidate, now)
}

func (store *InMemoryOutboxStore) enqueueNextCompensationLocked(saga *SagaInstance, now time.Time) error {
	candidate := store.compensationCandidateLocked(saga.SagaID)
	if candidate == nil {
		if store.hasSucceededStepLocked(saga.SagaID) {
			saga.State = SagaBlockedCompensation
		} else {
			saga.State = SagaCompensated
		}
		saga.Revision++
		saga.UpdatedAt = now
		return nil
	}
	return store.enqueueCompensationLocked(saga, candidate, now)
}

func (store *InMemoryOutboxStore) hasSucceededStepLocked(sagaID string) bool {
	for _, step := range store.steps[sagaID] {
		if step.State == StepSucceeded {
			return true
		}
	}
	return false
}

func (store *InMemoryOutboxStore) compensationCandidateLocked(sagaID string) *SagaStep {
	var candidate *SagaStep
	for _, step := range store.steps[sagaID] {
		if step.State != StepSucceeded || step.CompensationVerb == "" {
			continue
		}
		if candidate == nil || step.Sequence > candidate.Sequence {
			candidate = step
		}
	}
	return candidate
}

func (store *InMemoryOutboxStore) enqueueCompensationLocked(saga *SagaInstance, step *SagaStep, now time.Time) error {
	key := IdempotencyKey(saga.Namespace, saga.SagaID, step.EffectID, invocation.DirectionCompensation)
	id := "dispatch/" + key
	if store.dispatches[id] != nil {
		return nil
	}
	store.dispatches[id] = &Dispatch{
		ID: id, SagaID: saga.SagaID, EffectID: step.EffectID, Sequence: step.Sequence,
		Direction: invocation.DirectionCompensation, Verb: step.CompensationVerb,
		ContractHash: step.CompensationContract, Arguments: append(json.RawMessage(nil), step.CompensationArguments...),
		ArgumentHash: step.CompensationArgumentHash, IdempotencyKey: key, State: DispatchQueued,
		Fencing: append([]FencingRequirement(nil), step.Fencing...), Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	return nil
}

func sameStep(step *SagaStep, request EnqueueStepRequest, argumentHash, compensationHash string) bool {
	return step.Sequence == request.Sequence && step.Verb == request.Verb && step.ContractHash == request.ContractHash &&
		step.ArgumentHash == argumentHash && step.CompensationVerb == request.CompensationVerb &&
		step.CompensationContract == request.CompensationContract && step.CompensationArgumentHash == compensationHash &&
		sameRequirements(step.Fencing, request.Fencing)
}

func currentLease(dispatch *Dispatch, attempt uint64, token string) bool {
	return dispatch != nil && dispatch.State == DispatchInFlight && dispatch.Attempt == attempt && token != "" && dispatch.LeaseToken == token
}

func currentLeaseAt(dispatch *Dispatch, attempt uint64, token string, now time.Time) bool {
	return currentLease(dispatch, attempt, token) && dispatch.LeaseDeadline.After(now)
}

func validateCompletion(completion Completion) error {
	if completion.DispatchID == "" || completion.Attempt == 0 || completion.LeaseToken == "" {
		return fmt.Errorf("dispatch ID, attempt, and lease token are required")
	}
	switch completion.Outcome {
	case invocation.OutcomeSuccess:
		if len(completion.Result) == 0 {
			return fmt.Errorf("successful dispatch requires a canonical result")
		}
		var decoded any
		decoder := json.NewDecoder(strings.NewReader(string(completion.Result)))
		decoder.UseNumber()
		if err := decoder.Decode(&decoded); err != nil {
			return fmt.Errorf("successful dispatch result is invalid JSON: %w", err)
		}
		canonical, err := json.Marshal(decoded)
		if err != nil || string(canonical) != string(completion.Result) {
			return fmt.Errorf("successful dispatch result is not canonical JSON")
		}
	case invocation.OutcomeRetryableKnownNotCommitted, invocation.OutcomePermanentFailure,
		invocation.OutcomeUnknown, invocation.OutcomeStaleFence:
		if completion.Error == "" {
			return fmt.Errorf("dispatch outcome %s requires an error", completion.Outcome)
		}
	default:
		return fmt.Errorf("invalid dispatch outcome %q", completion.Outcome)
	}
	return nil
}

func applyCompletion(dispatch *Dispatch, step *SagaStep, saga *SagaInstance, completion Completion) error {
	dispatch.LastOutcome = completion.Outcome
	dispatch.LastError = completion.Error
	dispatch.LeaseOwner = ""
	dispatch.LeaseToken = ""
	dispatch.LeaseDeadline = time.Time{}
	dispatch.Revision++
	dispatch.UpdatedAt = completion.Now
	saga.Revision++
	saga.UpdatedAt = completion.Now
	switch completion.Outcome {
	case invocation.OutcomeSuccess:
		dispatch.State = DispatchSucceeded
		dispatch.Result = append(json.RawMessage(nil), completion.Result...)
		if dispatch.Direction == invocation.DirectionForward {
			step.State = StepSucceeded
			step.Result = append(json.RawMessage(nil), completion.Result...)
		} else {
			step.State = StepCompensated
		}
	case invocation.OutcomeRetryableKnownNotCommitted:
		if completion.Exhausted {
			dispatch.State = DispatchFailedPermanent
			if dispatch.Direction == invocation.DirectionCompensation {
				saga.State = SagaBlockedCompensation
			} else {
				step.State = StepFailed
				saga.State = SagaFailed
			}
		} else {
			dispatch.State = DispatchRetryWait
			dispatch.NextAttemptAt = completion.NextAttemptAt
		}
	case invocation.OutcomePermanentFailure:
		dispatch.State = DispatchFailedPermanent
		if dispatch.Direction == invocation.DirectionForward {
			step.State = StepFailed
		} else {
			saga.State = SagaBlockedCompensation
		}
	case invocation.OutcomeUnknown:
		if completion.Exhausted {
			dispatch.State = DispatchBlockedUnknown
			if dispatch.Direction == invocation.DirectionCompensation {
				saga.State = SagaBlockedCompensation
			} else {
				saga.State = SagaBlockedUnknown
			}
		} else {
			dispatch.State = DispatchRetryWait
			dispatch.NextAttemptAt = completion.NextAttemptAt
		}
	case invocation.OutcomeStaleFence:
		dispatch.State = DispatchBlockedFence
		saga.State = SagaBlockedFence
	}
	return nil
}

func validateGrants(grants []invocation.FencingGrant) error {
	for index, grant := range grants {
		if grant.Authority == "" || grant.Resource == "" || grant.Token == 0 {
			return fmt.Errorf("invalid fencing grant at index %d", index)
		}
		if index > 0 {
			previous := grants[index-1]
			if previous.Authority > grant.Authority || (previous.Authority == grant.Authority && previous.Resource >= grant.Resource) {
				return fmt.Errorf("fencing grants must be unique and canonically ordered")
			}
		}
	}
	return nil
}

func sameRequirements(left, right []FencingRequirement) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func sameGrants(left, right []invocation.FencingGrant) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func randomLeaseToken() (string, error) {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", fmt.Errorf("generate lease token: %w", err)
	}
	return hex.EncodeToString(token[:]), nil
}

func cloneSaga(saga *SagaInstance) *SagaInstance {
	if saga == nil {
		return nil
	}
	copy := *saga
	return &copy
}
