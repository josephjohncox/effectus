package loader

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type signatureVerifierFunc func(context.Context, string, string) error

func (function signatureVerifierFunc) Verify(ctx context.Context, reference, digest string) error {
	return function(ctx, reference, digest)
}

func TestOCIVerificationRequiresDigestAndConfiguredSignaturePolicy(t *testing.T) {
	_, _, err := loadOCIBundleLoaders(t.Context(), "registry.example/bundle:latest", OCIVerificationPolicy{})
	require.ErrorContains(t, err, "pinned by digest")
	err = verifyOCIIdentity(t.Context(), "registry.example/bundle", "sha256:one", "sha256:two", OCIVerificationPolicy{})
	require.ErrorContains(t, err, "digest mismatch")
	err = verifyOCIIdentity(t.Context(), "registry.example/bundle", "sha256:one", "sha256:one", OCIVerificationPolicy{RequireSignature: true})
	require.ErrorContains(t, err, "no verifier")
}

func TestOCIVerificationCallsPluggableVerifier(t *testing.T) {
	called := false
	policy := OCIVerificationPolicy{RequireSignature: true, Verifier: signatureVerifierFunc(func(_ context.Context, reference, digest string) error {
		called = true
		require.Equal(t, "bundle", reference)
		require.Equal(t, "sha256:value", digest)
		return nil
	})}
	require.NoError(t, verifyOCIIdentity(t.Context(), "bundle", "sha256:value", "sha256:value", policy))
	require.True(t, called)
	policy.Verifier = signatureVerifierFunc(func(context.Context, string, string) error { return errors.New("invalid signature") })
	require.ErrorContains(t, verifyOCIIdentity(t.Context(), "bundle", "sha256:value", "sha256:value", policy), "invalid signature")
}
