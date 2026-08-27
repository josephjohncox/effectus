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

	"github.com/effectus/effectus-go/invocation"
	"github.com/effectus/effectus-go/ir"
)

// ExecutorDescriptor is the immutable resolver input for one verb binding.
type ExecutorDescriptor struct {
	Type       string            `json:"type"`
	ResolverID string            `json:"resolver_id"`
	Reference  string            `json:"reference,omitempty"`
	Config     map[string]string `json:"config,omitempty"`
}

// GenerationConfig contains every value covered by a generation digest.
type GenerationConfig struct {
	Checked             *ir.Checked
	Environment         ir.Environment
	Ruleset             string
	Version             string
	ExecutorDescriptors map[string]ExecutorDescriptor
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
	executorDescriptors map[string]ExecutorDescriptor
	functionIDs         map[string]string
	sourceDigest        string
	digest              string
	executors           map[string]invocation.Executor
	closers             []io.Closer

	refs    atomic.Int64
	retired atomic.Bool
	closed  atomic.Bool
	closeMu sync.Mutex
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
	for verb, executor := range config.Executors {
		if executor == nil {
			return nil, fmt.Errorf("generation executor %q is nil", verb)
		}
		descriptor, ok := config.ExecutorDescriptors[verb]
		if !ok || strings.TrimSpace(descriptor.Type) == "" {
			return nil, fmt.Errorf("generation executor %q has no canonical descriptor", verb)
		}
		if config.Production && strings.TrimSpace(descriptor.ResolverID) == "" {
			return nil, fmt.Errorf("production generation executor %q is callback-only and cannot be recovered", verb)
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
	Name       string             `json:"name"`
	Descriptor ExecutorDescriptor `json:"descriptor"`
}

type namedString struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func canonicalExecutorDescriptors(values map[string]ExecutorDescriptor) []namedExecutorDescriptor {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]namedExecutorDescriptor, 0, len(names))
	for _, name := range names {
		result = append(result, namedExecutorDescriptor{Name: name, Descriptor: cloneExecutorDescriptor(values[name])})
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
func (generation *Generation) Retired() bool { return generation != nil && generation.retired.Load() }
func (generation *Generation) Closed() bool  { return generation != nil && generation.closed.Load() }

func (generation *Generation) closeIfUnused() error {
	if generation == nil || !generation.retired.Load() || generation.refs.Load() != 0 || generation.closed.Load() {
		return nil
	}
	generation.closeMu.Lock()
	defer generation.closeMu.Unlock()
	if generation.closed.Load() || generation.refs.Load() != 0 {
		return nil
	}
	var first error
	for index := len(generation.closers) - 1; index >= 0; index-- {
		if generation.closers[index] != nil {
			if err := generation.closers[index].Close(); err != nil && first == nil {
				first = err
			}
		}
	}
	generation.closed.Store(true)
	return first
}

// GenerationManager atomically publishes exactly one active generation.
type GenerationManager struct{ active atomic.Pointer[Generation] }

func (manager *GenerationManager) Publish(generation *Generation) error {
	if manager == nil || generation == nil {
		return fmt.Errorf("generation manager and generation are required")
	}
	if generation.retired.Load() || generation.closed.Load() {
		return fmt.Errorf("cannot publish a retired generation")
	}
	previous := manager.active.Swap(generation)
	if previous != nil && previous != generation {
		previous.retired.Store(true)
		return previous.closeIfUnused()
	}
	return nil
}

func (manager *GenerationManager) Acquire() (*GenerationHandle, error) {
	if manager == nil {
		return nil, fmt.Errorf("generation manager is nil")
	}
	for {
		generation := manager.active.Load()
		if generation == nil {
			return nil, fmt.Errorf("no active generation")
		}
		generation.refs.Add(1)
		if manager.active.Load() == generation && !generation.retired.Load() && !generation.closed.Load() {
			return &GenerationHandle{generation: generation}, nil
		}
		if generation.refs.Add(-1) == 0 {
			_ = generation.closeIfUnused()
		}
	}
}

func (manager *GenerationManager) ActiveDigest() string {
	if manager == nil {
		return ""
	}
	generation := manager.active.Load()
	if generation == nil {
		return ""
	}
	return generation.digest
}

// GenerationHandle pins a generation until Release.
type GenerationHandle struct {
	generation *Generation
	released   atomic.Bool
}

func (handle *GenerationHandle) Generation() *Generation {
	if handle == nil || handle.released.Load() {
		return nil
	}
	return handle.generation
}
func (handle *GenerationHandle) Release() error {
	if handle == nil || handle.generation == nil || !handle.released.CompareAndSwap(false, true) {
		return nil
	}
	if handle.generation.refs.Add(-1) < 0 {
		panic("runtime generation reference count became negative")
	}
	return handle.generation.closeIfUnused()
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
func cloneExecutorDescriptor(value ExecutorDescriptor) ExecutorDescriptor {
	value.Config = cloneGenerationStringMap(value.Config)
	return value
}
func cloneExecutorDescriptors(values map[string]ExecutorDescriptor) map[string]ExecutorDescriptor {
	result := make(map[string]ExecutorDescriptor, len(values))
	for name, value := range values {
		result[name] = cloneExecutorDescriptor(value)
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
