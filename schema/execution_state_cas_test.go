package schema

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExecutionStateCASPreservesOwnedAndTerminalRecords(t *testing.T) {
	store := NewInMemoryExecutionLedger()
	admission := testDurableAdmission("guarded", "delivery", "payload", "generation")
	admission.Plans = []ExecutionPlanRecord{{ExecutionID: "guarded", PlanID: "plan", SagaID: StableSagaID("guarded", "plan"), State: "selected"}}
	require.NoError(t, store.PutArtifact(t.Context(), admission.Artifact))
	_, _, err := store.AdmitExecution(t.Context(), admission)
	require.NoError(t, err)
	leases, err := store.LeaseExecutions(t.Context(), "owner", 1, time.Minute)
	require.NoError(t, err)
	owned, err := store.GetExecution(t.Context(), "guarded")
	require.NoError(t, err)
	_, err = store.SetExecutionState(t.Context(), "guarded", owned.Revision, ExecutionCompleted, "wrong owner")
	require.ErrorIs(t, err, ErrOptimisticConflict)
	after, err := store.GetExecution(t.Context(), "guarded")
	require.NoError(t, err)
	require.Equal(t, owned, after)
	require.NoError(t, store.FinishExecutionLease(t.Context(), leases[0], ExecutionCompleted, "done"))
	terminal, err := store.GetExecution(t.Context(), "guarded")
	require.NoError(t, err)
	_, err = store.SetExecutionState(t.Context(), "guarded", terminal.Revision, ExecutionRunning, "resurrect")
	require.ErrorIs(t, err, ErrOptimisticConflict)
	after, err = store.GetExecution(t.Context(), "guarded")
	require.NoError(t, err)
	require.Equal(t, terminal, after)
}
