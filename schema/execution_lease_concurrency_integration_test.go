//go:build integration

package schema

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPostgresExecutionLeaseRenewalRacesFinish(t *testing.T) {
	db := openSagaIntegrationDB(t)
	db.SetMaxOpenConns(4)
	require.NoError(t, MigrateSagaV2(t.Context(), db))
	store, err := NewPostgresOutboxStore(db)
	require.NoError(t, err)
	for iteration := 0; iteration < 16; iteration++ {
		t.Run(fmt.Sprintf("race-%02d", iteration), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			id, identity, generation := "race-"+uuid.NewString(), "delivery-"+uuid.NewString(), "generation-"+uuid.NewString()
			admission := testDurableAdmission(id, identity, "payload", generation)
			sagaID := StableSagaID(id, "plan")
			admission.Plans = []ExecutionPlanRecord{{ExecutionID: id, PlanID: "plan", SagaID: sagaID, Ordinal: 0}}
			admission.Sagas = []CreateSagaRequest{{Namespace: "tenant", SagaID: sagaID, ExecutionID: id, PlanID: "plan", PlanDigest: admission.Artifact.IRDigest, Serial: true}}
			admission.InitialSteps = []EnqueueStepRequest{{SagaID: sagaID, EffectID: "effect", Sequence: 1, Verb: "write", ContractHash: "contract", Arguments: map[string]any{}}}
			t.Cleanup(func() { cleanupExecutionIntegration(t, db, id, sagaID, generation) })
			_, _, err := store.AdmitExecutionAtomic(ctx, admission)
			require.NoError(t, err)
			original, err := store.LeaseExecution(ctx, id, "owner", time.Minute)
			require.NoError(t, err)

			start := make(chan struct{})
			var workers sync.WaitGroup
			workers.Add(2)
			var renewed ExecutionLease
			var renewalErr, finishErr error
			go func() {
				defer workers.Done()
				<-start
				renewed, renewalErr = store.RenewExecutionLease(ctx, original, 2*time.Minute)
			}()
			go func() {
				defer workers.Done()
				<-start
				finishErr = store.FinishExecutionLease(ctx, original, ExecutionCompleted, "")
			}()
			close(start)
			// Join both database users before assertions or fixture cleanup.
			workers.Wait()
			require.NoError(t, finishErr, "renewal must not invalidate the original terminal CAS handle")
			if renewalErr == nil {
				require.Equal(t, original.ExecutionID, renewed.ExecutionID)
				require.Equal(t, original.Owner, renewed.Owner)
				require.Equal(t, original.Token, renewed.Token)
				require.Equal(t, original.Revision, renewed.Revision)
				require.False(t, renewed.Deadline.Before(original.Deadline))
			} else {
				require.ErrorIs(t, renewalErr, ErrStaleExecutionLease, "finish may win before renewal")
			}
			record, err := store.GetExecution(ctx, id)
			require.NoError(t, err)
			require.Equal(t, ExecutionCompleted, record.State)
			require.Equal(t, original.Revision+1, record.Revision)
			require.Empty(t, record.RecoveryOwner)
			require.Empty(t, record.RecoveryToken)
			require.True(t, record.RecoveryDeadline.IsZero())
			_, err = store.RenewExecutionLease(ctx, original, time.Minute)
			require.ErrorIs(t, err, ErrStaleExecutionLease, "late renewal must not resurrect terminal ownership")
		})
	}
}
