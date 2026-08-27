package runtime

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/effectus/effectus-go/compiler"
	"github.com/effectus/effectus-go/invocation"
	"github.com/effectus/effectus-go/ir"
	"github.com/effectus/effectus-go/loader"
	"github.com/effectus/effectus-go/schema"
	"github.com/effectus/effectus-go/schema/verb"
)

// ManifestArtifactResolver reconstructs invocation-aware adapters only from
// immutable descriptors stored in the execution artifact.
type ManifestArtifactResolver struct{}

func NewManifestArtifactResolver() *ManifestArtifactResolver { return &ManifestArtifactResolver{} }

type artifactExecutorEntry struct {
	Name               string
	ResolverDescriptor map[string]any `json:"resolver_descriptor"`
}

func (*ManifestArtifactResolver) ResolveArtifact(_ context.Context, artifact schema.ExecutionArtifact, checked *ir.Checked) (*compiler.CompiledUnit, error) {
	var environment ir.Environment
	if err := json.Unmarshal(artifact.Environment, &environment); err != nil {
		return nil, fmt.Errorf("decode checked environment: %w", err)
	}
	var entries []artifactExecutorEntry
	if err := json.Unmarshal(artifact.ExecutorManifest, &entries); err != nil {
		return nil, fmt.Errorf("decode executor manifest: %w", err)
	}
	var functionEnvelope struct {
		InitialData map[string]any `json:"initial_data"`
		Functions   map[string]any `json:"functions"`
	}
	if len(artifact.FunctionManifest) > 0 {
		if err := json.Unmarshal(artifact.FunctionManifest, &functionEnvelope); err != nil {
			return nil, fmt.Errorf("decode function manifest: %w", err)
		}
		if len(functionEnvelope.Functions) > 0 {
			return nil, fmt.Errorf("artifact functions require a configured immutable function resolver")
		}
	}
	specs := make(map[string]*compiler.CompiledVerbSpec, len(entries))
	for _, entry := range entries {
		contract, ok := environment.Verbs[entry.Name]
		if !ok {
			return nil, fmt.Errorf("executor %q has no checked contract", entry.Name)
		}
		implementation, err := resolveInvocationDescriptor(entry.Name, entry.ResolverDescriptor)
		if err != nil {
			return nil, err
		}
		strict := true
		spec := &verb.Spec{Name: entry.Name, ArgTypes: contract.Arguments, RequiredArgs: contract.RequiredArgs, ReturnType: contract.ResultType, Inverse: contract.InverseVerb, Executor: implementation, StrictArgs: &strict, StrictReturn: &strict}
		specs[entry.Name] = &compiler.CompiledVerbSpec{Spec: spec, ExecutorType: compiler.ExecutorLocal, ExecutorConfig: &compiler.LocalExecutorConfig{Implementation: implementation}, TypeSignature: &compiler.TypeSignature{InputTypes: contract.Arguments, OutputType: contract.ResultType}}
	}
	return &compiler.CompiledUnit{VerbSpecs: specs, Functions: map[string]*compiler.CompiledFunction{}, CheckedIR: checked, IREnvironment: environment, InitialData: functionEnvelope.InitialData}, nil
}

func resolveInvocationDescriptor(name string, descriptor map[string]any) (verb.Executor, error) {
	kind, _ := descriptor["type"].(string)
	var implementation verb.Executor
	var err error
	switch kind {
	case "http":
		config := map[string]any{"url": descriptor["url"], "method": descriptor["method"], "headers": descriptor["headers"], "timeout": descriptor["timeout"], "allowPrivateNetwork": descriptor["allow_private_network"]}
		implementation, err = loader.NewHTTPExecutor(config)
	case "grpc":
		config := map[string]any{"address": descriptor["address"], "method": descriptor["method"], "metadata": descriptor["metadata"], "timeout": descriptor["timeout"], "useTLS": descriptor["tls"], "insecure": descriptor["insecure"], "serverName": descriptor["server_name"]}
		implementation, err = loader.NewGRPCExecutor(config)
	case "stream":
		config, _ := descriptor["config"].(map[string]any)
		implementation, err = loader.NewStreamExecutor(config)
	case "oci":
		implementation, err = loader.NewOCIExecutor(name, map[string]any{"ref": descriptor["reference"], "verb": descriptor["verb"], "signatureVerifier": descriptor["signature_verifier"]})
	default:
		return nil, fmt.Errorf("verb %q has unsupported immutable resolver type %q", name, kind)
	}
	if err != nil {
		return nil, fmt.Errorf("resolve verb %q: %w", name, err)
	}
	if _, ok := implementation.(invocation.Executor); !ok {
		return nil, fmt.Errorf("resolved verb %q is not invocation-aware", name)
	}
	return implementation, nil
}
