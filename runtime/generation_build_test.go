package runtime

import (
	"context"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/josephjohncox/effectus/bundle"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
	"github.com/stretchr/testify/require"
)

func TestCompileGenerationIsDeterministicAndResolvesCheckedVerbs(t *testing.T) {
	descriptor, err := invocation.NewDescriptor(invocation.DescriptorSpec{Type: invocation.DescriptorHTTP, ResolverID: "test/http/v1"})
	require.NoError(t, err)
	sourceBundle, err := bundle.New(bundle.Spec{
		Name: "orders", Version: "1",
		Sources: []bundle.Source{{Path: "rules/orders.eff", Content: `rule "Review" priority 1 { when { order.risk > 80 } then { RequestReview(orderId: order.id) } }`}},
		Environment: ir.Environment{
			Facts: map[string]string{"order.risk": "int", "order.id": "string"},
			Verbs: map[string]ir.VerbContract{"RequestReview": {
				Arguments: map[string]string{"orderId": "string"}, RequiredArgs: []string{"orderId"}, ResultType: "string",
			}},
		},
		Executors: map[string]invocation.Descriptor{"RequestReview": descriptor},
	})
	require.NoError(t, err)
	registry, err := invocation.NewRegistry([]invocation.ResolverRegistration{{
		ID: "test/http/v1", Resolver: invocation.ResolverFunc(func(context.Context, invocation.Descriptor) (invocation.Executor, io.Closer, error) {
			return generationTestExecutor{}, io.NopCloser(strings.NewReader("")), nil
		}),
	}})
	require.NoError(t, err)
	first, err := CompileGeneration(t.Context(), GenerationBuildConfig{Bundle: sourceBundle, Resolvers: registry, Production: true})
	require.NoError(t, err)
	second, err := CompileGeneration(t.Context(), GenerationBuildConfig{Bundle: sourceBundle, Resolvers: registry, Production: true})
	require.NoError(t, err)
	require.Equal(t, first.Digest(), second.Digest())
	require.Equal(t, first.Checked().Digest(), second.Checked().Digest())
	require.NotNil(t, first.Checked())
	_, ok := first.Executor("RequestReview")
	require.True(t, ok)
	require.NoError(t, first.Close())
	require.NoError(t, second.Close())
}

func TestCompileGenerationResolvesUnusedDescriptorsAndClosesEachExactlyOnce(t *testing.T) {
	used, err := invocation.NewDescriptor(invocation.DescriptorSpec{Type: invocation.DescriptorHTTP, ResolverID: "test/http/v1", Reference: "used"})
	require.NoError(t, err)
	unused, err := invocation.NewDescriptor(invocation.DescriptorSpec{Type: invocation.DescriptorHTTP, ResolverID: "test/http/v1", Reference: "unused"})
	require.NoError(t, err)
	sourceBundle, err := bundle.New(bundle.Spec{
		Name: "orders", Version: "1",
		Sources: []bundle.Source{{Path: "rules/orders.eff", Content: `rule "Review" priority 1 { when { true } then { RequestReview() } }`}},
		Environment: ir.Environment{Verbs: map[string]ir.VerbContract{
			"RequestReview": {ResultType: "string"}, "Unused": {ResultType: "string"},
		}},
		Executors: map[string]invocation.Descriptor{"RequestReview": used, "Unused": unused},
	})
	require.NoError(t, err)
	var resolved, closed atomic.Int32
	registry, err := invocation.NewRegistry([]invocation.ResolverRegistration{{
		ID: "test/http/v1", Resolver: invocation.ResolverFunc(func(context.Context, invocation.Descriptor) (invocation.Executor, io.Closer, error) {
			resolved.Add(1)
			return generationTestExecutor{}, generationBuildTestCloser{closed: &closed}, nil
		}),
	}})
	require.NoError(t, err)
	first, err := CompileGeneration(t.Context(), GenerationBuildConfig{Bundle: sourceBundle, Resolvers: registry, Production: true})
	require.NoError(t, err)
	second, err := CompileGeneration(t.Context(), GenerationBuildConfig{Bundle: sourceBundle, Resolvers: registry, Production: true})
	require.NoError(t, err)
	require.Equal(t, int32(4), resolved.Load())
	require.Equal(t, first.Digest(), second.Digest())
	_, ok := first.Executor("Unused")
	require.True(t, ok)
	require.NoError(t, first.Close())
	require.NoError(t, first.Close())
	require.Equal(t, int32(2), closed.Load())
	require.NoError(t, second.Close())
	require.Equal(t, int32(4), closed.Load())
}

func TestCompileGenerationRejectsUnresolvableUnusedDescriptorAndClosesAcquiredResources(t *testing.T) {
	used, err := invocation.NewDescriptor(invocation.DescriptorSpec{Type: invocation.DescriptorHTTP, ResolverID: "test/http/v1"})
	require.NoError(t, err)
	unused, err := invocation.NewDescriptor(invocation.DescriptorSpec{Type: invocation.DescriptorHTTP, ResolverID: "missing/http/v1"})
	require.NoError(t, err)
	sourceBundle, err := bundle.New(bundle.Spec{
		Name: "orders", Version: "1",
		Sources: []bundle.Source{{Path: "rules/orders.eff", Content: `rule "Review" priority 1 { when { true } then { RequestReview() } }`}},
		Environment: ir.Environment{Verbs: map[string]ir.VerbContract{
			"RequestReview": {ResultType: "string"}, "Unused": {ResultType: "string"},
		}},
		Executors: map[string]invocation.Descriptor{"RequestReview": used, "Unused": unused},
	})
	require.NoError(t, err)
	var closed atomic.Int32
	registry, err := invocation.NewRegistry([]invocation.ResolverRegistration{{
		ID: "test/http/v1", Resolver: invocation.ResolverFunc(func(context.Context, invocation.Descriptor) (invocation.Executor, io.Closer, error) {
			return generationTestExecutor{}, generationBuildTestCloser{closed: &closed}, nil
		}),
	}})
	require.NoError(t, err)
	_, err = CompileGeneration(t.Context(), GenerationBuildConfig{Bundle: sourceBundle, Resolvers: registry, Production: true})
	require.ErrorContains(t, err, `verb "Unused"`)
	require.Equal(t, int32(1), closed.Load())
}

type generationBuildTestCloser struct{ closed *atomic.Int32 }

func (closer generationBuildTestCloser) Close() error {
	closer.closed.Add(1)
	return nil
}

func TestCompileGenerationFailsClosedForUnresolvedExecutor(t *testing.T) {
	sourceBundle, err := bundle.New(bundle.Spec{
		Name: "orders", Version: "1",
		Sources:     []bundle.Source{{Path: "rules/orders.eff", Content: `rule "Review" priority 1 { when { true } then { RequestReview() } }`}},
		Environment: ir.Environment{Verbs: map[string]ir.VerbContract{"RequestReview": {ResultType: "string"}}},
	})
	require.NoError(t, err)
	_, err = CompileGeneration(t.Context(), GenerationBuildConfig{Bundle: sourceBundle, Production: true})
	require.ErrorContains(t, err, "no invocation descriptor")
}
