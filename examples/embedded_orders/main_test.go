package main

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSharedOrderReviewArtifactIdentity(t *testing.T) {
	rule, scenario, err := sharedOrderReviewArtifacts()
	require.NoError(t, err)
	require.Equal(t, "5b7dc75ce28dd9dbd75124efd8c7bcfca5c19c209fec6256f2a9388161c88daa", fmt.Sprintf("%x", sha256.Sum256(rule)))
	require.Equal(t, "eae0fb388ca468fde352e7ac34a5c6afa2d28faaddf512c4202db3119ec739d4", fmt.Sprintf("%x", sha256.Sum256(scenario)))
}
