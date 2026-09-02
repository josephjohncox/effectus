package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"sync/atomic"
	"testing"

	effectusv1 "github.com/josephjohncox/effectus/gen/effectus/v1"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
	"github.com/stretchr/testify/require"
)

type generationTestExecutor struct{}

func (generationTestExecutor) Invoke(context.Context, invocation.Request) invocation.Outcome {
	return invocation.Outcome{Class: invocation.OutcomeSuccess}
}

type generationTestCloser struct{ count atomic.Int32 }

func (closer *generationTestCloser) Close() error { closer.count.Add(1); return nil }

func TestGenerationDigestDeterministicAndProductionRejectsCallbacks(t *testing.T) {
	environment, checked := generationTestArtifact(t)
	descriptor, err := invocation.NewDescriptor(invocation.DescriptorSpec{
		Type: invocation.DescriptorHTTP, ResolverID: "resolver-v1", Settings: map[string]string{"b": "2", "a": "1"},
	})
	require.NoError(t, err)
	config := GenerationConfig{
		Checked: checked, Environment: environment, Ruleset: "orders", Version: "1", SourceDigest: "source",
		ExecutorDescriptors: map[string]invocation.Descriptor{"write": descriptor},
		FunctionIDs:         map[string]string{"lower": "stdlib/lower/v1"}, Executors: map[string]invocation.Executor{"write": generationTestExecutor{}}, Production: true,
	}
	first, err := NewGeneration(config)
	require.NoError(t, err)
	second, err := NewGeneration(config)
	require.NoError(t, err)
	require.Equal(t, first.Digest(), second.Digest())

	callback, descriptorErr := invocation.NewDescriptor(invocation.DescriptorSpec{Type: invocation.DescriptorEmbedded})
	require.NoError(t, descriptorErr)
	config.ExecutorDescriptors["write"] = callback
	_, err = NewGeneration(config)
	require.ErrorContains(t, err, "callback-only")
}

func TestGenerationCloseRetiresResourcesExactlyOnce(t *testing.T) {
	environment, checked := generationTestArtifact(t)
	closer := &generationTestCloser{}
	generation, err := NewGeneration(GenerationConfig{
		Checked: checked, Environment: environment, Ruleset: "orders", Version: "1", Closers: []io.Closer{closer},
	})
	require.NoError(t, err)
	require.NoError(t, generation.Close())
	require.NoError(t, generation.Close())
	require.Equal(t, int32(1), closer.count.Load())
	require.True(t, generation.Closed())
}

func generationTestArtifact(t *testing.T) (ir.Environment, *ir.Checked) {
	t.Helper()
	environment := ir.Environment{}
	digest, err := ir.EnvironmentDigest(environment)
	require.NoError(t, err)
	build := sha256.Sum256([]byte("runtime-generation-test"))
	checked, err := ir.Check(&effectusv1.RuleArtifact{
		FormatVersion: ir.FormatVersion, EnvironmentDigest: digest,
		Compiler: &effectusv1.CompilerMetadata{Name: "effectusc", Version: "test", BuildDigest: hex.EncodeToString(build[:])},
	}, environment, ir.Limits{})
	require.NoError(t, err)
	return environment, checked
}
