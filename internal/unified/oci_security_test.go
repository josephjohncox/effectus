package unified

import (
	"testing"

	"github.com/josephjohncox/effectus/internal/loader"
	"github.com/stretchr/testify/require"
)

func TestOCIBundlePullerRejectsMutableTagsBeforeNetwork(t *testing.T) {
	_, err := NewOCIBundlePuller(t.TempDir()).Pull("registry.example/bundle:latest")
	require.ErrorContains(t, err, "pinned by digest")
}

func TestOCIBundlePullerExposesSignaturePolicy(t *testing.T) {
	puller := NewOCIBundlePullerWithPolicy(t.TempDir(), loader.OCIVerificationPolicy{RequireSignature: true})
	require.True(t, puller.verification.RequireSignature)
}
