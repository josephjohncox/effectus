package runtime

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/josephjohncox/effectus/compiler"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
	"github.com/josephjohncox/effectus/schema"
	"github.com/stretchr/testify/require"
)

func TestManifestArtifactResolverRejectsLegacyAndUnknownDescriptorShapes(t *testing.T) {
	environment := ir.Environment{Verbs: map[string]ir.VerbContract{"Review": {ResultType: "string"}}}
	checked, err := compiler.CompileChecked(t.Context(), []compiler.Source{{Path: "review.eff", Content: []byte(`rule "review" priority 1 { when { true } then { Review() } }`)}}, environment, compiler.CompileOptions{})
	require.NoError(t, err)
	encodedEnvironment, err := json.Marshal(environment)
	require.NoError(t, err)
	resolver := NewManifestArtifactResolver()
	artifact := schema.ExecutionArtifact{Environment: encodedEnvironment, ExecutorManifest: []byte(`[{"name":"Review","descriptor":{"type":"http","resolver_id":"effectus/loader-http/v1","reference":"https://executor.invalid","settings":[]},"legacy":true}]`)}
	_, err = resolver.ResolveArtifact(context.Background(), artifact, checked)
	require.ErrorContains(t, err, "unknown field")

	descriptor, err := invocation.NewDescriptor(invocation.DescriptorSpec{Type: invocation.DescriptorHTTP, ResolverID: "effectus/loader-http/v1", Reference: "https://executor.invalid"})
	require.NoError(t, err)
	data, err := descriptor.CanonicalJSON()
	require.NoError(t, err)
	artifact.ExecutorManifest = []byte(`[{"name":"Review","descriptor":` + string(data) + `}]`)
	_, err = resolver.ResolveArtifact(context.Background(), artifact, checked)
	require.ErrorContains(t, err, "not registered")
}
