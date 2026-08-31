package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strconv"
	"sync"
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
	config := GenerationConfig{
		Checked: checked, Environment: environment, Ruleset: "orders", Version: "1", SourceDigest: "source",
		ExecutorDescriptors: map[string]ExecutorDescriptor{"write": {Type: "http", ResolverID: "resolver-v1", Config: map[string]string{"b": "2", "a": "1"}}},
		FunctionIDs:         map[string]string{"lower": "stdlib/lower/v1"}, Executors: map[string]invocation.Executor{"write": generationTestExecutor{}}, Production: true,
	}
	first, err := NewGeneration(config)
	require.NoError(t, err)
	second, err := NewGeneration(config)
	require.NoError(t, err)
	require.Equal(t, first.Digest(), second.Digest())

	config.ExecutorDescriptors["write"] = ExecutorDescriptor{Type: "local"}
	_, err = NewGeneration(config)
	require.ErrorContains(t, err, "callback-only")
}

func TestGenerationManagerRetiresAfterLastHandle(t *testing.T) {
	environment, checked := generationTestArtifact(t)
	firstCloser := &generationTestCloser{}
	first, err := NewGeneration(GenerationConfig{Checked: checked, Environment: environment, Ruleset: "orders", Version: "1", Closers: []io.Closer{firstCloser}})
	require.NoError(t, err)
	second, err := NewGeneration(GenerationConfig{Checked: checked, Environment: environment, Ruleset: "orders", Version: "2"})
	require.NoError(t, err)
	manager := &GenerationManager{}
	require.NoError(t, manager.Publish(first))
	handle, err := manager.Acquire()
	require.NoError(t, err)
	require.Same(t, first, handle.Generation())
	require.NoError(t, manager.Publish(second))
	require.True(t, first.Retired())
	require.Zero(t, firstCloser.count.Load())
	require.NoError(t, handle.Release())
	require.Equal(t, int32(1), firstCloser.count.Load())
	require.True(t, first.Closed())
}

func TestGenerationManagerConcurrentAcquireAndPublish(t *testing.T) {
	environment, checked := generationTestArtifact(t)
	manager := &GenerationManager{}
	makeGeneration := func(version string) *Generation {
		generation, err := NewGeneration(GenerationConfig{Checked: checked, Environment: environment, Ruleset: "orders", Version: version})
		require.NoError(t, err)
		return generation
	}
	require.NoError(t, manager.Publish(makeGeneration("0")))
	var wait sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < 100; iteration++ {
				handle, err := manager.Acquire()
				if err == nil {
					require.NotEmpty(t, handle.Generation().Digest())
					require.NoError(t, handle.Release())
				}
			}
		}()
	}
	for version := 1; version <= 20; version++ {
		require.NoError(t, manager.Publish(makeGeneration(strconv.Itoa(version))))
	}
	wait.Wait()
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
