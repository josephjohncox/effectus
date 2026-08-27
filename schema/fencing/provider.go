// Package fencing defines resource leases that issue fencing tokens.
package fencing

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/effectus/effectus-go/invocation"
)

var (
	ErrLeaseHeld  = errors.New("fencing resource lease is held")
	ErrStaleGrant = errors.New("stale fencing grant")
)

// Guarantee states the persistence guarantee of a provider.
type Guarantee string

const (
	GuaranteeLocalAdvisory    Guarantee = "local_advisory"
	GuaranteeDurableMonotonic Guarantee = "durable_monotonic"
)

// Request identifies one resource lease.
type Request struct {
	Authority string
	Resource  string
	Holder    string
	TTL       time.Duration
}

// Lease is one acquired grant.
type Lease interface {
	Grant() invocation.FencingGrant
	Renew(context.Context, time.Duration) error
	Release(context.Context) error
}

// Provider issues grants and reports their persistence guarantee.
type Provider interface {
	Guarantee() Guarantee
	Acquire(context.Context, Request) (Lease, error)
}

// InMemoryProvider provides local advisory serialization only.
type InMemoryProvider struct {
	mu       sync.Mutex
	counters map[string]uint64
	leases   map[string]*memoryLeaseState
	now      func() time.Time
}

type memoryLeaseState struct {
	holder    string
	token     uint64
	expiresAt time.Time
}

type memoryLease struct {
	provider  *InMemoryProvider
	authority string
	resource  string
	holder    string
	token     uint64
}

func NewInMemoryProvider() *InMemoryProvider {
	return &InMemoryProvider{
		counters: make(map[string]uint64),
		leases:   make(map[string]*memoryLeaseState),
		now:      time.Now,
	}
}

func (*InMemoryProvider) Guarantee() Guarantee { return GuaranteeLocalAdvisory }

func (provider *InMemoryProvider) Acquire(ctx context.Context, request Request) (Lease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateRequest(request); err != nil {
		return nil, err
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	key := resourceKey(request.Authority, request.Resource)
	now := provider.now().UTC()
	if current := provider.leases[key]; current != nil && current.expiresAt.After(now) {
		return nil, fmt.Errorf("%w: %s/%s", ErrLeaseHeld, request.Authority, request.Resource)
	}
	provider.counters[key]++
	state := &memoryLeaseState{holder: request.Holder, token: provider.counters[key], expiresAt: now.Add(request.TTL)}
	provider.leases[key] = state
	return &memoryLease{
		provider: provider, authority: request.Authority, resource: request.Resource,
		holder: request.Holder, token: state.token,
	}, nil
}

func (lease *memoryLease) Grant() invocation.FencingGrant {
	return invocation.FencingGrant{Authority: lease.authority, Resource: lease.resource, Token: lease.token}
}

func (lease *memoryLease) Renew(ctx context.Context, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ttl <= 0 {
		return fmt.Errorf("fencing lease TTL must be positive")
	}
	lease.provider.mu.Lock()
	defer lease.provider.mu.Unlock()
	key := resourceKey(lease.authority, lease.resource)
	current := lease.provider.leases[key]
	if current == nil || current.holder != lease.holder || current.token != lease.token || !current.expiresAt.After(lease.provider.now().UTC()) {
		return ErrStaleGrant
	}
	current.expiresAt = lease.provider.now().UTC().Add(ttl)
	return nil
}

func (lease *memoryLease) Release(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	lease.provider.mu.Lock()
	defer lease.provider.mu.Unlock()
	key := resourceKey(lease.authority, lease.resource)
	current := lease.provider.leases[key]
	if current == nil {
		return nil
	}
	if current.holder != lease.holder || current.token != lease.token {
		return ErrStaleGrant
	}
	delete(lease.provider.leases, key)
	return nil
}

func validateRequest(request Request) error {
	if request.Authority == "" || request.Resource == "" || request.Holder == "" {
		return fmt.Errorf("fencing authority, resource, and holder are required")
	}
	if request.TTL <= 0 {
		return fmt.Errorf("fencing lease TTL must be positive")
	}
	return nil
}

func resourceKey(authority, resource string) string { return authority + "\x00" + resource }
