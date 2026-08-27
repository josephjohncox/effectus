package schema

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testExecutionArtifact(digest string) ExecutionArtifact {
	return ExecutionArtifact{
		GenerationDigest: digest, IRDigest: "ir-" + digest, IRBytes: []byte{1, 2, 3},
		Environment:      json.RawMessage(`{"facts":{},"verbs":{},"functions":{},"types":{}}`),
		ExecutorManifest: json.RawMessage(`[]`), FunctionManifest: json.RawMessage(`{}`),
		SourceDigest: "source-" + digest, CompilerMetadata: json.RawMessage(`{"name":"test"}`),
	}
}

func testDurableAdmission(executionID, identity, requestHash, generation string) DurableAdmission {
	facts := json.RawMessage(`{"order":{"id":"42"}}`)
	return DurableAdmission{
		Artifact: testExecutionArtifact(generation),
		Execution: ExecutionRecord{ExecutionID: executionID, AdmissionIdentity: identity, RequestHash: requestHash,
			Ruleset: "orders", Version: "1", TenantNamespace: "tenant", MergePolicy: "merge", GenerationDigest: generation, EffectiveFacts: facts},
		FactApplication: FactApplication{ExecutionID: executionID, FactEventID: identity, MergePolicy: "merge", Facts: facts, AppliedRevision: 1},
	}
}

func TestExecutionLedgerDuplicateAdmissionAndConflict(t *testing.T) {
	store := NewInMemoryExecutionLedger()
	admission := testDurableAdmission("execution", "delivery", "payload-a", "generation-a")
	require.NoError(t, store.PutArtifact(t.Context(), admission.Artifact))
	first, created, err := store.AdmitExecution(t.Context(), admission)
	require.NoError(t, err)
	require.True(t, created)
	second, created, err := store.AdmitExecution(t.Context(), admission)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, first.EffectiveFacts, second.EffectiveFacts)
	require.Len(t, store.facts["execution"], 1, "fact event must apply exactly once")

	conflict := admission
	conflict.Execution.RequestHash = "payload-b"
	_, _, err = store.AdmitExecution(t.Context(), conflict)
	require.ErrorIs(t, err, ErrIdentityConflict)
}

func TestExecutionLedgerReplayPinsOriginalGeneration(t *testing.T) {
	store := NewInMemoryExecutionLedger()
	first := testDurableAdmission("execution", "delivery", "same-payload", "generation-a")
	require.NoError(t, store.PutArtifact(t.Context(), first.Artifact))
	_, created, err := store.AdmitExecution(t.Context(), first)
	require.NoError(t, err)
	require.True(t, created)

	replay := testDurableAdmission("execution", "delivery", "same-payload", "generation-b")
	require.NoError(t, store.PutArtifact(t.Context(), replay.Artifact))
	record, created, err := store.AdmitExecution(t.Context(), replay)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, "generation-a", record.GenerationDigest)
}

func TestExecutionLedgerLeaseExpiryAndCAS(t *testing.T) {
	store := NewInMemoryExecutionLedger()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	admission := testDurableAdmission("execution", "delivery", "payload", "generation")
	require.NoError(t, store.PutArtifact(t.Context(), admission.Artifact))
	_, _, err := store.AdmitExecution(t.Context(), admission)
	require.NoError(t, err)
	first, err := store.LeaseExecutions(t.Context(), "one", 1, time.Second)
	require.NoError(t, err)
	require.Len(t, first, 1)
	other, err := store.LeaseExecutions(t.Context(), "two", 1, time.Second)
	require.NoError(t, err)
	require.Empty(t, other)
	now = now.Add(2 * time.Second)
	second, err := store.LeaseExecutions(t.Context(), "two", 1, time.Second)
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.NotEqual(t, first[0].Token, second[0].Token)
	require.ErrorIs(t, store.FinishExecutionLease(t.Context(), first[0], ExecutionCompleted, ""), ErrStaleExecutionLease)
	require.NoError(t, store.FinishExecutionLease(t.Context(), second[0], ExecutionCompleted, ""))
}
