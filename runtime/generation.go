package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
)

// GenerationConfig contains every value covered by a generation digest.
type GenerationConfig struct {
	Checked             *ir.Checked
	Environment         ir.Environment
	Ruleset             string
	Version             string
	ExecutorDescriptors map[string]invocation.Descriptor
	FunctionIDs         map[string]string
	SourceDigest        string
	Executors           map[string]invocation.Executor
	Closers             []io.Closer
	Production          bool
}

// Generation is immutable after construction. Manager ownership and acquired
// handles are the only references counted for retirement.
type Generation struct {
	checked             *ir.Checked
	environment         ir.Environment
	ruleset             string
	version             string
	executorDescriptors map[string]invocation.Descriptor
	functionIDs         map[string]string
	sourceDigest        string
	digest              string
	executors           map[string]invocation.Executor
	closers             []io.Closer

	closed   atomic.Bool
	closeMu  sync.Mutex
	closeErr error
}

func NewGeneration(config GenerationConfig) (*Generation, error) {
	if config.Checked == nil || config.Checked.Digest() == "" {
		return nil, fmt.Errorf("generation checked IR is required")
	}
	if strings.TrimSpace(config.Ruleset) == "" || strings.TrimSpace(config.Version) == "" {
		return nil, fmt.Errorf("generation ruleset and version are required")
	}
	environmentDigest, err := ir.EnvironmentDigest(config.Environment)
	if err != nil {
		return nil, fmt.Errorf("generation environment: %w", err)
	}
	if config.Checked.CloneArtifact().EnvironmentDigest != environmentDigest {
		return nil, fmt.Errorf("generation environment does not match checked IR")
	}
	if config.Production && strings.TrimSpace(config.SourceDigest) == "" {
		return nil, fmt.Errorf("production generation source digest is required")
	}
	for verb, descriptor := range config.ExecutorDescriptors {
		if _, err := descriptor.CanonicalJSON(); err != nil {
			return nil, fmt.Errorf("generation executor %q descriptor: %w", verb, err)
		}
	}
	for verb, executor := range config.Executors {
		if executor == nil {
			return nil, fmt.Errorf("generation executor %q is nil", verb)
		}
		descriptor, ok := config.ExecutorDescriptors[verb]
		if !ok {
			return nil, fmt.Errorf("generation executor %q has no canonical descriptor", verb)
		}
		if config.Production && strings.TrimSpace(descriptor.ResolverID()) == "" {
			return nil, fmt.Errorf("production generation executor %q is callback-only and cannot be recovered", verb)
		}
	}
	if config.Production {
		for _, plan := range config.Checked.CloneArtifact().Plans {
			for _, step := range plan.Steps {
				if err := validateResolvedGenerationVerb(step.Verb, config.ExecutorDescriptors, config.Executors); err != nil {
					return nil, err
				}
				if step.Compensation != nil {
					if err := validateResolvedGenerationVerb(step.Compensation.InverseVerb, config.ExecutorDescriptors, config.Executors); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	manifest := generationDigestManifest{
		IRDigest: config.Checked.Digest(), EnvironmentDigest: environmentDigest,
		Ruleset: config.Ruleset, Version: config.Version, SourceDigest: config.SourceDigest,
		Executors: canonicalExecutorDescriptors(config.ExecutorDescriptors), Functions: canonicalStringManifest(config.FunctionIDs),
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("marshal generation manifest: %w", err)
	}
	digest := sha256.Sum256(data)
	return &Generation{
		checked: config.Checked, environment: cloneGenerationEnvironment(config.Environment), ruleset: config.Ruleset, version: config.Version,
		executorDescriptors: cloneExecutorDescriptors(config.ExecutorDescriptors), functionIDs: cloneGenerationStringMap(config.FunctionIDs),
		sourceDigest: config.SourceDigest, digest: hex.EncodeToString(digest[:]), executors: cloneExecutors(config.Executors),
		closers: append([]io.Closer(nil), config.Closers...),
	}, nil
}

func validateResolvedGenerationVerb(verb string, descriptors map[string]invocation.Descriptor, executors map[string]invocation.Executor) error {
	descriptor, described := descriptors[verb]
	if !described {
		return fmt.Errorf("production generation verb %q has no canonical descriptor", verb)
	}
	if descriptor.ResolverID() == "" {
		return fmt.Errorf("production generation verb %q is callback-only and cannot be recovered", verb)
	}
	if executors[verb] == nil {
		return fmt.Errorf("production generation verb %q is unresolved", verb)
	}
	return nil
}

type generationDigestManifest struct {
	IRDigest          string                    `json:"ir_digest"`
	EnvironmentDigest string                    `json:"environment_digest"`
	Ruleset           string                    `json:"ruleset"`
	Version           string                    `json:"version"`
	SourceDigest      string                    `json:"source_digest"`
	Executors         []namedExecutorDescriptor `json:"executors"`
	Functions         []namedString             `json:"functions"`
}

type namedExecutorDescriptor struct {
	Name       string                `json:"name"`
	Descriptor invocation.Descriptor `json:"descriptor"`
}

type namedString struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func canonicalExecutorDescriptors(values map[string]invocation.Descriptor) []namedExecutorDescriptor {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]namedExecutorDescriptor, 0, len(names))
	for _, name := range names {
		result = append(result, namedExecutorDescriptor{Name: name, Descriptor: values[name]})
	}
	return result
}
func canonicalStringManifest(values map[string]string) []namedString {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]namedString, 0, len(names))
	for _, name := range names {
		result = append(result, namedString{Name: name, Value: values[name]})
	}
	return result
}

func (generation *Generation) Checked() *ir.Checked {
	if generation == nil {
		return nil
	}
	return generation.checked
}
func (generation *Generation) Environment() ir.Environment {
	if generation == nil {
		return ir.Environment{}
	}
	return cloneGenerationEnvironment(generation.environment)
}
func (generation *Generation) Ruleset() string {
	if generation == nil {
		return ""
	}
	return generation.ruleset
}
func (generation *Generation) Version() string {
	if generation == nil {
		return ""
	}
	return generation.version
}
func (generation *Generation) Digest() string {
	if generation == nil {
		return ""
	}
	return generation.digest
}
func (generation *Generation) SourceDigest() string {
	if generation == nil {
		return ""
	}
	return generation.sourceDigest
}
func (generation *Generation) Executor(verb string) (invocation.Executor, bool) {
	if generation == nil {
		return nil, false
	}
	executor, ok := generation.executors[verb]
	return executor, ok
}

// ExecutorDescriptors returns the immutable resolver manifest.
func (generation *Generation) ExecutorDescriptors() map[string]invocation.Descriptor {
	if generation == nil {
		return nil
	}
	return cloneExecutorDescriptors(generation.executorDescriptors)
}

// FunctionIDs returns the immutable function resolver identities.
func (generation *Generation) FunctionIDs() map[string]string {
	if generation == nil {
		return nil
	}
	return cloneGenerationStringMap(generation.functionIDs)
}

// Closed reports whether generation-owned executor resources are retired.
func (generation *Generation) Closed() bool { return generation != nil && generation.closed.Load() }

// Close retires all generation-owned resources exactly once in reverse
// acquisition order. A changed generation requires replacing the process.
func (generation *Generation) Close() error {
	if generation == nil {
		return nil
	}
	generation.closeMu.Lock()
	defer generation.closeMu.Unlock()
	if generation.closed.Load() {
		return generation.closeErr
	}
	for index := len(generation.closers) - 1; index >= 0; index-- {
		if generation.closers[index] != nil {
			if err := generation.closers[index].Close(); err != nil && generation.closeErr == nil {
				generation.closeErr = err
			}
		}
	}
	generation.closed.Store(true)
	return generation.closeErr
}

func cloneGenerationEnvironment(environment ir.Environment) ir.Environment {
	result := ir.Environment{Facts: cloneGenerationStringMap(environment.Facts), Verbs: make(map[string]ir.VerbContract, len(environment.Verbs)), Functions: make(map[string]ir.FunctionContract, len(environment.Functions)), Types: make(map[string]ir.TypeDefinition, len(environment.Types))}
	for name, contract := range environment.Verbs {
		contract.Arguments = cloneGenerationStringMap(contract.Arguments)
		contract.RequiredArgs = append([]string(nil), contract.RequiredArgs...)
		result.Verbs[name] = contract
	}
	for name, contract := range environment.Functions {
		contract.ArgumentTypes = append([]string(nil), contract.ArgumentTypes...)
		result.Functions[name] = contract
	}
	for name, definition := range environment.Types {
		definition.Fields = cloneGenerationStringMap(definition.Fields)
		definition.RequiredFields = append([]string(nil), definition.RequiredFields...)
		result.Types[name] = definition
	}
	return result
}
func cloneGenerationStringMap(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}
func cloneExecutorDescriptors(values map[string]invocation.Descriptor) map[string]invocation.Descriptor {
	result := make(map[string]invocation.Descriptor, len(values))
	for name, value := range values {
		result[name] = value
	}
	return result
}
func cloneExecutors(values map[string]invocation.Executor) map[string]invocation.Executor {
	result := make(map[string]invocation.Executor, len(values))
	for name, value := range values {
		result[name] = value
	}
	return result
}
