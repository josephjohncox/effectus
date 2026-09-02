package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/josephjohncox/effectus/bundle"
	"github.com/josephjohncox/effectus/compiler"
	"github.com/josephjohncox/effectus/invocation"
)

// GenerationBuildConfig is the single source-bundle to generation startup input.
type GenerationBuildConfig struct {
	Bundle         *bundle.SourceBundle
	CompileOptions compiler.CompileOptions
	Resolvers      *invocation.Registry
	FunctionIDs    map[string]string
	Production     bool
}

// CompileGeneration compiles one source bundle exactly once, resolves every
// declared invocation descriptor, and freezes the resulting Generation.
func CompileGeneration(ctx context.Context, config GenerationBuildConfig) (*Generation, error) {
	if ctx == nil {
		return nil, fmt.Errorf("compile generation: context is nil")
	}
	if config.Bundle == nil {
		return nil, fmt.Errorf("compile generation: source bundle is required")
	}
	checked, err := compiler.CompileChecked(ctx, config.Bundle, config.CompileOptions)
	if err != nil {
		return nil, err
	}

	descriptors := config.Bundle.Executors()
	for _, plan := range checked.CloneArtifact().Plans {
		for _, step := range plan.Steps {
			if _, ok := descriptors[step.Verb]; !ok {
				return nil, fmt.Errorf("compile generation: checked verb %q has no invocation descriptor", step.Verb)
			}
			if step.Compensation != nil {
				if _, ok := descriptors[step.Compensation.InverseVerb]; !ok {
					return nil, fmt.Errorf("compile generation: checked verb %q has no invocation descriptor", step.Compensation.InverseVerb)
				}
			}
		}
	}
	// A generation owns and publishes the complete descriptor manifest. Resolve
	// every declared descriptor, including declarations that no checked rule
	// currently reaches, so publication cannot later discover an unresolved
	// manifest entry. Sorting also fixes acquisition and reverse-close order.
	verbNames := make([]string, 0, len(descriptors))
	for verbName := range descriptors {
		verbNames = append(verbNames, verbName)
	}
	sort.Strings(verbNames)
	executors := make(map[string]invocation.Executor, len(descriptors))
	closers := make([]io.Closer, 0, len(descriptors))
	closeAcquired := func() error {
		var result error
		for index := len(closers) - 1; index >= 0; index-- {
			result = errors.Join(result, closers[index].Close())
		}
		return result
	}
	for _, verbName := range verbNames {
		descriptor, ok := descriptors[verbName]
		if !ok {
			_ = closeAcquired()
			return nil, fmt.Errorf("compile generation: checked verb %q has no invocation descriptor", verbName)
		}
		if config.Production && descriptor.ResolverID() == "" {
			_ = closeAcquired()
			return nil, fmt.Errorf("compile generation: checked verb %q is callback-only", verbName)
		}
		if config.Resolvers == nil {
			_ = closeAcquired()
			return nil, fmt.Errorf("compile generation: invocation resolver registry is required for verb %q", verbName)
		}
		executor, closer, err := config.Resolvers.Resolve(ctx, descriptor)
		if err != nil {
			_ = closeAcquired()
			return nil, fmt.Errorf("compile generation: verb %q: %w", verbName, err)
		}
		executors[verbName] = executor
		if closer != nil {
			closers = append(closers, closer)
		}
	}
	sourceDigest, err := config.Bundle.Digest()
	if err != nil {
		_ = closeAcquired()
		return nil, fmt.Errorf("compile generation: source digest: %w", err)
	}
	generation, err := NewGeneration(GenerationConfig{
		Checked: checked, Environment: config.Bundle.Environment(), Ruleset: config.Bundle.Name(), Version: config.Bundle.Version(),
		ExecutorDescriptors: descriptors, FunctionIDs: config.FunctionIDs, SourceDigest: sourceDigest,
		Executors: executors, Closers: closers, Production: config.Production,
	})
	if err != nil {
		_ = closeAcquired()
		return nil, fmt.Errorf("compile generation: %w", err)
	}
	return generation, nil
}
