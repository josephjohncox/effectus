package schema

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExecutionLeaseRenewalAndExpiry(t *testing.T) {
	store := NewInMemoryExecutionLedger()
	now := time.Now().UTC()
	store.now = func() time.Time { return now }
	admission := testDurableAdmission("renew", "delivery", "payload", "generation")
	require.NoError(t, store.PutArtifact(t.Context(), admission.Artifact))
	_, _, err := store.AdmitExecution(t.Context(), admission)
	require.NoError(t, err)
	leases, err := store.LeaseExecutions(t.Context(), "owner", 1, time.Second)
	require.NoError(t, err)
	original := leases[0]
	now = now.Add(800 * time.Millisecond)
	renewed, err := store.RenewExecutionLease(t.Context(), original, time.Second)
	require.NoError(t, err)
	require.Equal(t, original.Token, renewed.Token)
	require.Equal(t, original.Revision, renewed.Revision)
	require.True(t, renewed.Deadline.After(original.Deadline))
	now = now.Add(300 * time.Millisecond)
	other, err := store.LeaseExecutions(t.Context(), "other", 1, time.Second)
	require.NoError(t, err)
	require.Empty(t, other, "the original deadline no longer expires a renewed lease")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = store.RenewExecutionLease(ctx, renewed, time.Second)
	require.ErrorIs(t, err, context.Canceled)
	wrong := renewed
	wrong.Token = "wrong"
	_, err = store.RenewExecutionLease(t.Context(), wrong, time.Second)
	require.ErrorIs(t, err, ErrStaleExecutionLease)
	_, err = store.RenewExecutionLease(t.Context(), renewed, 0)
	require.Error(t, err)
	now = renewed.Deadline
	_, err = store.RenewExecutionLease(t.Context(), renewed, time.Second)
	require.ErrorIs(t, err, ErrStaleExecutionLease, "an expired lease cannot be revived")
	require.ErrorIs(t, store.FinishExecutionLease(t.Context(), original, ExecutionCompleted, ""), ErrStaleExecutionLease, "expiry invalidates authority even before a competing claim")
	other, err = store.LeaseExecutions(t.Context(), "other", 1, time.Second)
	require.NoError(t, err)
	require.Len(t, other, 1)
	_, err = store.RenewExecutionLease(t.Context(), original, time.Second)
	require.ErrorIs(t, err, ErrStaleExecutionLease)
	require.NoError(t, store.FinishExecutionLease(t.Context(), other[0], ExecutionCompleted, ""))
	_, err = store.RenewExecutionLease(t.Context(), other[0], time.Second)
	require.ErrorIs(t, err, ErrStaleExecutionLease)
}

func TestRenewedExecutionLeaseCanFinishWithOriginalHandle(t *testing.T) {
	store := NewInMemoryExecutionLedger()
	now := time.Now().UTC()
	store.now = func() time.Time { return now }
	admission := testDurableAdmission("finish", "delivery", "payload", "generation")
	require.NoError(t, store.PutArtifact(t.Context(), admission.Artifact))
	_, _, err := store.AdmitExecution(t.Context(), admission)
	require.NoError(t, err)
	leases, err := store.LeaseExecutions(t.Context(), "owner", 1, time.Second)
	require.NoError(t, err)
	original := leases[0]
	now = now.Add(time.Second / 2)
	_, err = store.RenewExecutionLease(t.Context(), original, 2*time.Second)
	require.NoError(t, err)
	now = now.Add(time.Second)
	require.NoError(t, store.FinishExecutionLease(t.Context(), original, ExecutionCompleted, ""))
}
