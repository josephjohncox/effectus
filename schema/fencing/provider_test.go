package fencing

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInMemoryProviderIsLocalAdvisoryAndMonotonicInProcess(t *testing.T) {
	provider := NewInMemoryProvider()
	require.Equal(t, GuaranteeLocalAdvisory, provider.Guarantee())
	first, err := provider.Acquire(t.Context(), Request{Authority: "db", Resource: "account-1", Holder: "one", TTL: time.Minute})
	require.NoError(t, err)
	_, err = provider.Acquire(t.Context(), Request{Authority: "db", Resource: "account-1", Holder: "two", TTL: time.Minute})
	require.ErrorIs(t, err, ErrLeaseHeld)
	require.NoError(t, first.Release(t.Context()))
	second, err := provider.Acquire(t.Context(), Request{Authority: "db", Resource: "account-1", Holder: "two", TTL: time.Minute})
	require.NoError(t, err)
	require.Greater(t, second.Grant().Token, first.Grant().Token)
	require.ErrorIs(t, first.Release(t.Context()), ErrStaleGrant)
	require.NoError(t, second.Release(t.Context()))
}

func TestInMemoryProviderIssuesHigherTokenAfterExpiry(t *testing.T) {
	provider := NewInMemoryProvider()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	provider.now = func() time.Time { return now }
	first, err := provider.Acquire(t.Context(), Request{Authority: "db", Resource: "account-1", Holder: "one", TTL: time.Second})
	require.NoError(t, err)
	now = now.Add(2 * time.Second)
	second, err := provider.Acquire(t.Context(), Request{Authority: "db", Resource: "account-1", Holder: "two", TTL: time.Second})
	require.NoError(t, err)
	require.Greater(t, second.Grant().Token, first.Grant().Token)
	require.ErrorIs(t, first.Renew(t.Context(), time.Second), ErrStaleGrant)
}
