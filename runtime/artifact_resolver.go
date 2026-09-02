package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/josephjohncox/effectus/compiler"
	"github.com/josephjohncox/effectus/internal/loader"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
	"github.com/josephjohncox/effectus/schema/ledger"
	"github.com/josephjohncox/effectus/schema/verb"
)

// ManifestArtifactResolver reconstructs invocation-aware adapters only from
// immutable descriptors stored in the execution artifact.
type ManifestArtifactResolver struct{ resolvers *invocation.Registry }

// NewManifestArtifactResolver creates a fail-closed artifact resolver. The
// only implicit resolver is the canonical HTTP resolver; legacy loader resolver
// IDs are deliberately not decoded or recovered.
func NewManifestArtifactResolver(registries ...*invocation.Registry) *ManifestArtifactResolver {
	if len(registries) > 0 && registries[0] != nil {
		return &ManifestArtifactResolver{resolvers: registries[0]}
	}
	registry, _ := invocation.NewRegistry([]invocation.ResolverRegistration{{
		ID: invocation.HTTPResolverID, Resolver: invocation.HTTPResolver{},
	}})
	return &ManifestArtifactResolver{resolvers: registry}
}

type artifactExecutorEntry struct {
	Name       string                `json:"name"`
	Descriptor invocation.Descriptor `json:"descriptor"`
}

func (resolver *ManifestArtifactResolver) ResolveArtifact(ctx context.Context, artifact ledger.ExecutionArtifact, checked *ir.Checked) (*compiler.CompiledUnit, error) {
	var environment ir.Environment
	if err := strictArtifactJSON(artifact.Environment, &environment); err != nil {
		return nil, fmt.Errorf("decode checked environment: %w", err)
	}
	var entries []artifactExecutorEntry
	if err := strictArtifactJSON(artifact.ExecutorManifest, &entries); err != nil {
		return nil, fmt.Errorf("decode executor manifest: %w", err)
	}
	var functionEnvelope struct {
		InitialData map[string]any `json:"initial_data"`
		Functions   map[string]any `json:"functions"`
	}
	if len(artifact.FunctionManifest) > 0 {
		if err := strictArtifactJSON(artifact.FunctionManifest, &functionEnvelope); err != nil {
			return nil, fmt.Errorf("decode function manifest: %w", err)
		}
		if len(functionEnvelope.Functions) > 0 {
			return nil, fmt.Errorf("artifact functions require a configured immutable function resolver")
		}
	}
	if resolver == nil || resolver.resolvers == nil {
		return nil, fmt.Errorf("artifact invocation resolver registry is not configured")
	}
	specs := make(map[string]*compiler.CompiledVerbSpec, len(entries))
	closers := make([]io.Closer, 0, len(entries))
	closeResolved := func() {
		for index := len(closers) - 1; index >= 0; index-- {
			_ = closers[index].Close()
		}
	}
	for _, entry := range entries {
		contract, ok := environment.Verbs[entry.Name]
		if !ok {
			closeResolved()
			return nil, fmt.Errorf("executor %q has no checked contract", entry.Name)
		}
		implementation, closer, err := resolver.resolvers.Resolve(ctx, entry.Descriptor)
		if err != nil {
			closeResolved()
			return nil, fmt.Errorf("resolve verb %q: %w", entry.Name, err)
		}
		if closer != nil {
			closers = append(closers, closer)
		}
		adapter := &generationInvocationExecutor{executor: implementation, descriptor: entry.Descriptor}
		strict := true
		spec := &verb.Spec{
			Name: entry.Name, ArgTypes: contract.Arguments, RequiredArgs: contract.RequiredArgs,
			ReturnType: contract.ResultType, Inverse: contract.InverseVerb, Executor: adapter,
			StrictArgs: &strict, StrictReturn: &strict,
		}
		specs[entry.Name] = &compiler.CompiledVerbSpec{
			Spec: spec, ExecutorType: compiler.ExecutorLocal,
			ExecutorConfig: &compiler.LocalExecutorConfig{Implementation: adapter},
			TypeSignature:  &compiler.TypeSignature{InputTypes: contract.Arguments, OutputType: contract.ResultType},
		}
	}
	snapshot, err := loader.NewResourceSnapshot(closers...)
	if err != nil {
		closeResolved()
		return nil, fmt.Errorf("own resolved executor resources: %w", err)
	}
	return &compiler.CompiledUnit{
		VerbSpecs: specs, Functions: map[string]*compiler.CompiledFunction{}, CheckedIR: checked,
		IREnvironment: environment, SourceDigest: artifact.SourceDigest, InitialData: functionEnvelope.InitialData,
		ExtensionSnapshot: snapshot, ExecutionOwnedSnapshot: true,
	}, nil
}

func strictArtifactJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON value")
		}
		return err
	}
	return nil
}
