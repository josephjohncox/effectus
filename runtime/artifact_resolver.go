package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
	"github.com/josephjohncox/effectus/schema/ledger"
)

// ManifestArtifactResolver rebuilds a generation only from the descriptor
// manifest durably pinned with an execution. No extension loader or callback
// implementation participates in recovery.
type ManifestArtifactResolver struct{ resolvers *invocation.Registry }

func NewManifestArtifactResolver(registries ...*invocation.Registry) *ManifestArtifactResolver {
	if len(registries) > 0 && registries[0] != nil {
		return &ManifestArtifactResolver{registries[0]}
	}
	registry, _ := invocation.NewRegistry([]invocation.ResolverRegistration{{ID: invocation.HTTPResolverID, Resolver: invocation.HTTPResolver{}}})
	return &ManifestArtifactResolver{registry}
}

type artifactExecutorEntry struct {
	Name       string                `json:"name"`
	Descriptor invocation.Descriptor `json:"descriptor"`
}

func (resolver *ManifestArtifactResolver) ResolveGeneration(ctx context.Context, artifact ledger.ExecutionArtifact) (*Generation, error) {
	if resolver == nil || resolver.resolvers == nil {
		return nil, fmt.Errorf("artifact invocation resolver registry is not configured")
	}
	var environment ir.Environment
	if err := strictArtifactJSON(artifact.Environment, &environment); err != nil {
		return nil, fmt.Errorf("decode checked environment: %w", err)
	}
	checked, err := ir.Parse(artifact.IRBytes, environment, ir.Limits{})
	if err != nil {
		return nil, fmt.Errorf("parse checked artifact: %w", err)
	}
	if checked.Digest() != artifact.IRDigest {
		return nil, fmt.Errorf("checked artifact digest mismatch")
	}
	var entries []artifactExecutorEntry
	if err := strictArtifactJSON(artifact.ExecutorManifest, &entries); err != nil {
		return nil, fmt.Errorf("decode executor manifest: %w", err)
	}
	descriptors := make(map[string]invocation.Descriptor, len(entries))
	executors := make(map[string]invocation.Executor, len(entries))
	closers := make([]io.Closer, 0, len(entries))
	closeAll := func() {
		for i := len(closers) - 1; i >= 0; i-- {
			_ = closers[i].Close()
		}
	}
	for _, entry := range entries {
		if _, ok := environment.Verbs[entry.Name]; !ok {
			closeAll()
			return nil, fmt.Errorf("executor %q has no checked contract", entry.Name)
		}
		executor, closer, err := resolver.resolvers.Resolve(ctx, entry.Descriptor)
		if err != nil {
			closeAll()
			return nil, fmt.Errorf("resolve verb %q: %w", entry.Name, err)
		}
		descriptors[entry.Name] = entry.Descriptor
		executors[entry.Name] = executor
		if closer != nil {
			closers = append(closers, closer)
		}
	}
	var identity struct {
		Ruleset     string            `json:"ruleset"`
		Version     string            `json:"version"`
		FunctionIDs map[string]string `json:"function_ids"`
	}
	if err := strictArtifactJSON(artifact.FunctionManifest, &identity); err != nil {
		closeAll()
		return nil, fmt.Errorf("decode generation identity: %w", err)
	}
	generation, err := NewGeneration(GenerationConfig{Checked: checked, Environment: environment, Ruleset: identity.Ruleset, Version: identity.Version, ExecutorDescriptors: descriptors, FunctionIDs: identity.FunctionIDs, SourceDigest: artifact.SourceDigest, Executors: executors, Closers: closers, Production: true})
	if err != nil {
		closeAll()
		return nil, err
	}
	if generation.Digest() != artifact.GenerationDigest {
		_ = generation.Close()
		return nil, fmt.Errorf("resolved generation digest does not match pinned artifact")
	}
	return generation, nil
}
func strictArtifactJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON value")
		}
		return err
	}
	return nil
}
