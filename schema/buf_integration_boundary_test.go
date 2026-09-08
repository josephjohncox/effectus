package schema

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBufConstructorDoesNotCreateWorkspaceFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-created")
	integration, err := NewBufIntegration(root)
	require.NoError(t, err)
	require.NotNil(t, integration)
	_, err = os.Stat(root)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = NewBufIntegration("")
	require.Error(t, err)
}

func TestBufRejectsNilAndCanceledInputs(t *testing.T) {
	integration, err := NewBufIntegration(t.TempDir())
	require.NoError(t, err)
	for _, target := range []*BufIntegration{nil, {}, integration} {
		require.NotPanics(t, func() { require.Error(t, target.RegisterVerbSchema(t.Context(), nil)) })
		require.NotPanics(t, func() { require.Error(t, target.RegisterFactSchema(t.Context(), nil)) })
		for _, ctx := range []context.Context{nil, alreadyCanceledBufContext()} {
			require.NotPanics(t, func() { require.Error(t, target.RegisterVerbSchema(ctx, &VerbSchema{Name: "notify"})) })
			require.NotPanics(t, func() { require.Error(t, target.RegisterFactSchema(ctx, &FactSchema{Name: "order"})) })
			require.NotPanics(t, func() { _, err := target.GenerateCode(ctx); require.Error(t, err) })
			require.NotPanics(t, func() { _, err := target.ValidateSchemas(ctx); require.Error(t, err) })
		}
	}
	ctx := alreadyCanceledBufContext()
	require.ErrorIs(t, integration.RegisterVerbSchema(ctx, &VerbSchema{Name: "notify"}), context.Canceled)
	require.ErrorIs(t, integration.RegisterFactSchema(ctx, &FactSchema{Name: "order"}), context.Canceled)
	_, err = integration.GenerateCode(ctx)
	require.ErrorIs(t, err, context.Canceled)
	_, err = integration.ValidateSchemas(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, integration.ListVerbSchemas())
	require.Empty(t, integration.ListFactSchemas())
}

func alreadyCanceledBufContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestBufCanceledRegistrationDoesNotMutateInputs(t *testing.T) {
	integration, err := NewBufIntegration(t.TempDir())
	require.NoError(t, err)
	verb := &VerbSchema{Name: "notify", InputSchema: map[string]any{"message": "string"}}
	fact := &FactSchema{Name: "order", Schema: map[string]any{"id": "string"}}
	require.True(t, errors.Is(integration.RegisterVerbSchema(alreadyCanceledBufContext(), verb), context.Canceled))
	require.True(t, verb.UpdatedAt.IsZero())
	require.True(t, errors.Is(integration.RegisterFactSchema(alreadyCanceledBufContext(), fact), context.Canceled))
	require.True(t, fact.UpdatedAt.IsZero())
}
