package schema

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/schema/fencing"
)

// DispatcherOptions control one durable dispatch worker.
type DispatcherOptions struct {
	Owner                 string
	RequestID             string
	LeaseDuration         time.Duration
	InvocationTimeout     time.Duration
	MaxAttempts           uint64
	InitialBackoff        time.Duration
	MaxBackoff            time.Duration
	RequireDurableFencing bool
}

// Dispatcher claims committed intents, persists fencing grants, then invokes.
type Dispatcher struct {
	store    OutboxStore
	provider fencing.Provider
	executor invocation.Executor
	options  DispatcherOptions
	now      func() time.Time
}

func NewDispatcher(store OutboxStore, provider fencing.Provider, executor invocation.Executor, options DispatcherOptions) (*Dispatcher, error) {
	if store == nil || executor == nil {
		return nil, fmt.Errorf("outbox store and invocation-aware executor are required")
	}
	if options.Owner == "" {
		return nil, fmt.Errorf("dispatcher owner is required")
	}
	if options.LeaseDuration <= 0 {
		options.LeaseDuration = 30 * time.Second
	}
	if options.InvocationTimeout <= 0 || options.InvocationTimeout >= options.LeaseDuration {
		options.InvocationTimeout = options.LeaseDuration * 3 / 4
	}
	if options.InvocationTimeout <= 0 {
		return nil, fmt.Errorf("dispatcher lease duration is too short")
	}
	if options.MaxAttempts == 0 {
		options.MaxAttempts = 8
	}
	if options.InitialBackoff <= 0 {
		options.InitialBackoff = time.Second
	}
	if options.MaxBackoff <= 0 {
		options.MaxBackoff = time.Minute
	}
	if options.RequireDurableFencing && (provider == nil || provider.Guarantee() != fencing.GuaranteeDurableMonotonic) {
		return nil, fmt.Errorf("durable fencing is required but the provider is not durable_monotonic")
	}
	return &Dispatcher{store: store, provider: provider, executor: executor, options: options, now: time.Now}, nil
}

// DispatchOne handles at most one eligible intent. External invocation occurs
// only after ClaimDispatch and SaveFencingGrants commit.
func (dispatcher *Dispatcher) DispatchOne(ctx context.Context) (*Dispatch, error) {
	return dispatcher.Dispatch(ctx, "")
}

// Dispatch claims only targetDispatchID when it is non-empty.
func (dispatcher *Dispatcher) Dispatch(ctx context.Context, targetDispatchID string) (*Dispatch, error) {
	dispatch, err := dispatcher.store.ClaimDispatch(ctx, ClaimOptions{
		Owner: dispatcher.options.Owner, LeaseDuration: dispatcher.options.LeaseDuration, TargetDispatchID: targetDispatchID,
	})
	if err != nil {
		return nil, err
	}
	leases, grants, err := dispatcher.acquireFences(ctx, dispatch)
	if err != nil {
		// No external call occurred, so this outcome is known not committed.
		completion := Completion{
			DispatchID: dispatch.ID, Attempt: dispatch.Attempt, LeaseToken: dispatch.LeaseToken,
			Outcome: invocation.OutcomeRetryableKnownNotCommitted, Error: err.Error(),
			Now: dispatcher.now().UTC(), Exhausted: dispatch.Attempt >= dispatcher.options.MaxAttempts,
		}
		if !completion.Exhausted {
			completion.NextAttemptAt = completion.Now.Add(dispatcher.backoff(dispatch))
		}
		return dispatch, errors.Join(err, dispatcher.store.CompleteDispatch(ctx, completion))
	}
	defer releaseFences(leases)
	if err := dispatcher.store.SaveFencingGrants(ctx, dispatch.ID, dispatch.Attempt, dispatch.LeaseToken, grants); err != nil {
		return dispatch, err
	}
	dispatch.FencingGrants = append([]invocation.FencingGrant(nil), grants...)

	arguments := make(map[string]any)
	decoder := json.NewDecoder(bytes.NewReader(dispatch.Arguments))
	decoder.UseNumber()
	if err := decoder.Decode(&arguments); err != nil {
		return dispatch, dispatcher.completeUnknown(ctx, dispatch, fmt.Errorf("decode persisted arguments: %w", err))
	}
	saga, err := dispatcher.store.GetSaga(ctx, dispatch.SagaID)
	if err != nil {
		return dispatch, err
	}
	now := dispatcher.now().UTC()
	deadline := now.Add(dispatcher.options.InvocationTimeout)
	leaseSafeDeadline := dispatch.LeaseDeadline.Add(-dispatcher.options.LeaseDuration / 10)
	if leaseSafeDeadline.Before(deadline) {
		deadline = leaseSafeDeadline
	}
	if !deadline.After(now) {
		return dispatch, fmt.Errorf("dispatch lease has insufficient time for external invocation")
	}
	invokeCtx, cancel := context.WithDeadline(ctx, deadline)
	invokeRequest := invocation.Request{
		Metadata: invocation.Context{
			RequestID:   dispatcher.options.RequestID,
			ExecutionID: saga.ExecutionID,
			Saga: invocation.Saga{
				SagaID: dispatch.SagaID, EffectID: dispatch.EffectID, Attempt: dispatch.Attempt,
				Direction: dispatch.Direction, IdempotencyKey: dispatch.IdempotencyKey,
			},
			FencingGrants: append([]invocation.FencingGrant(nil), grants...), Deadline: deadline,
		},
		Verb: dispatch.Verb, Arguments: arguments, ArgumentHash: dispatch.ArgumentHash,
		ContractHash: dispatch.ContractHash,
	}
	outcome := dispatcher.executor.Invoke(invokeCtx, invokeRequest)
	cancel()
	if err := invocation.ValidateOutcome(outcome); err != nil {
		return dispatch, dispatcher.completeUnknown(ctx, dispatch, fmt.Errorf("invalid invocation outcome: %w", err))
	}
	completion := Completion{
		DispatchID: dispatch.ID, Attempt: dispatch.Attempt, LeaseToken: dispatch.LeaseToken,
		Outcome: outcome.Class, Now: dispatcher.now().UTC(),
	}
	if outcome.Err != nil {
		completion.Error = outcome.Err.Error()
	}
	if outcome.Class == invocation.OutcomeSuccess {
		result, _, err := CanonicalJSON(outcome.Result)
		if err != nil {
			return dispatch, dispatcher.completeUnknown(ctx, dispatch, fmt.Errorf("successful result is not serializable: %w", err))
		}
		completion.Result = result
	} else if outcome.Class == invocation.OutcomeRetryableKnownNotCommitted || outcome.Class == invocation.OutcomeUnknown {
		completion.Exhausted = dispatch.Attempt >= dispatcher.options.MaxAttempts
		if outcome.Class == invocation.OutcomeUnknown {
			policy, ok := dispatcher.executor.(UnknownOutcomeRetryPolicy)
			if !ok || !policy.RetryUnknownOutcome(invokeRequest) {
				completion.Exhausted = true
			}
		}
		if !completion.Exhausted {
			completion.NextAttemptAt = completion.Now.Add(dispatcher.backoff(dispatch))
		}
	}
	if err := dispatcher.store.CompleteDispatch(ctx, completion); err != nil {
		return dispatch, err
	}
	return dispatcher.store.GetDispatch(ctx, dispatch.ID)
}

func (dispatcher *Dispatcher) acquireFences(ctx context.Context, dispatch *Dispatch) ([]fencing.Lease, []invocation.FencingGrant, error) {
	if len(dispatch.Fencing) == 0 {
		return nil, nil, nil
	}
	if dispatcher.provider == nil {
		return nil, nil, fmt.Errorf("dispatch requires fencing but no provider is configured")
	}
	if dispatcher.options.RequireDurableFencing && dispatcher.provider.Guarantee() != fencing.GuaranteeDurableMonotonic {
		return nil, nil, fmt.Errorf("dispatch requires durable fencing")
	}
	requirements := append([]FencingRequirement(nil), dispatch.Fencing...)
	sort.Slice(requirements, func(i, j int) bool {
		if requirements[i].Authority != requirements[j].Authority {
			return requirements[i].Authority < requirements[j].Authority
		}
		return requirements[i].Resource < requirements[j].Resource
	})
	leases := make([]fencing.Lease, 0, len(requirements))
	grants := make([]invocation.FencingGrant, 0, len(requirements))
	holder := fmt.Sprintf("%s/%d/%s", dispatch.ID, dispatch.Attempt, dispatch.LeaseToken)
	for _, requirement := range requirements {
		lease, err := dispatcher.provider.Acquire(ctx, fencing.Request{
			Authority: requirement.Authority, Resource: requirement.Resource,
			Holder: holder, TTL: dispatcher.options.LeaseDuration,
		})
		if err != nil {
			releaseFences(leases)
			return nil, nil, err
		}
		leases = append(leases, lease)
		grants = append(grants, lease.Grant())
	}
	return leases, grants, nil
}

func (dispatcher *Dispatcher) completeUnknown(ctx context.Context, dispatch *Dispatch, cause error) error {
	now := dispatcher.now().UTC()
	completion := Completion{
		DispatchID: dispatch.ID, Attempt: dispatch.Attempt, LeaseToken: dispatch.LeaseToken,
		Outcome: invocation.OutcomeUnknown, Error: cause.Error(), Now: now,
		Exhausted: dispatch.Attempt >= dispatcher.options.MaxAttempts,
	}
	if !completion.Exhausted {
		completion.NextAttemptAt = now.Add(dispatcher.backoff(dispatch))
	}
	return errors.Join(cause, dispatcher.store.CompleteDispatch(ctx, completion))
}

func (dispatcher *Dispatcher) backoff(dispatch *Dispatch) time.Duration {
	exponent := float64(dispatch.Attempt - 1)
	value := float64(dispatcher.options.InitialBackoff) * math.Pow(2, exponent)
	if value > float64(dispatcher.options.MaxBackoff) {
		value = float64(dispatcher.options.MaxBackoff)
	}
	// Stable jitter prevents synchronized retries without changing after restart.
	seed := uint64(0)
	for _, char := range dispatch.IdempotencyKey {
		seed = seed*33 + uint64(char)
	}
	seed += dispatch.Attempt * 0x9e3779b97f4a7c15
	jitter := 0.75 + float64(seed%5000)/10000 // [0.75, 1.2499]
	return time.Duration(value * jitter)
}

func releaseFences(leases []fencing.Lease) {
	for index := len(leases) - 1; index >= 0; index-- {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = leases[index].Release(releaseCtx)
		cancel()
	}
}
