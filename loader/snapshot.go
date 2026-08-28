package loader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type StageOptions struct {
	Timeout        time.Duration
	MaxLoaders     int
	MaxSources     int
	MaxSourceBytes int
	MaxTotalBytes  int
	MaxVerbs       int
	MaxFunctions   int
	MaxTypes       int
	MaxDataEntries int
}

func normalizeStageOptions(options StageOptions) (StageOptions, error) {
	if options.Timeout < 0 || options.MaxLoaders < 0 || options.MaxSources < 0 || options.MaxSourceBytes < 0 || options.MaxTotalBytes < 0 || options.MaxVerbs < 0 || options.MaxFunctions < 0 || options.MaxTypes < 0 || options.MaxDataEntries < 0 {
		return StageOptions{}, fmt.Errorf("extension stage limits must not be negative")
	}
	if options.Timeout == 0 {
		options.Timeout = 30 * time.Second
	}
	if options.MaxLoaders == 0 {
		options.MaxLoaders = 1024
	}
	if options.MaxSources == 0 {
		options.MaxSources = 1024
	}
	if options.MaxSourceBytes == 0 {
		options.MaxSourceBytes = 4 << 20
	}
	if options.MaxTotalBytes == 0 {
		options.MaxTotalBytes = 32 << 20
	}
	if options.MaxVerbs == 0 {
		options.MaxVerbs = 4096
	}
	if options.MaxFunctions == 0 {
		options.MaxFunctions = 4096
	}
	if options.MaxTypes == 0 {
		options.MaxTypes = 4096
	}
	if options.MaxDataEntries == 0 {
		options.MaxDataEntries = 4096
	}
	return options, nil
}

type snapshotVerb struct {
	spec       immutableVerbSpec
	executor   VerbExecutor
	descriptor *ExecutorDescriptor
}
type snapshotData struct {
	path  string
	value any
}
type snapshotType struct {
	name       string
	definition TypeDefinition
}
type snapshotFunction struct {
	name           string
	implementation any
}

// ExtensionSnapshot contains immutable loader output. Compilation reads only
// this snapshot and cannot invoke file, OCI, DNS, or HTTP loaders.
type ExtensionSnapshot struct {
	verbs     []snapshotVerb
	functions []snapshotFunction
	data      []snapshotData
	types     []snapshotType
	sources   []SourceFile
	closers   []io.Closer
	refs      atomic.Int64
	retired   atomic.Bool
	closed    atomic.Bool
	closeMu   sync.Mutex
}

func (manager *ExtensionManager) Stage(ctx context.Context, options StageOptions) (*ExtensionSnapshot, error) {
	if manager == nil {
		return nil, fmt.Errorf("extension manager is required")
	}
	resolved, err := normalizeStageOptions(options)
	if err != nil {
		return nil, err
	}
	loaders := manager.GetLoaders()
	if len(loaders) > resolved.MaxLoaders {
		return nil, fmt.Errorf("extension loader count %d exceeds %d", len(loaders), resolved.MaxLoaders)
	}
	stageContext, cancel := context.WithTimeout(ctx, resolved.Timeout)
	defer cancel()
	target := &snapshotCaptureTarget{options: resolved, verbNames: map[string]struct{}{}, functionNames: map[string]struct{}{}, typeNames: map[string]struct{}{}, sourceNames: map[string]struct{}{}, dataNames: map[string]struct{}{}, closerIDs: map[uintptr]struct{}{}}
	for _, extensionLoader := range loaders {
		if err := stageContext.Err(); err != nil {
			_ = target.closeCandidates()
			return nil, err
		}
		if extensionLoader == nil {
			_ = target.closeCandidates()
			return nil, fmt.Errorf("extension loader is nil")
		}
		if err := extensionLoader.Load(stageContext, target); err != nil {
			_ = target.closeCandidates()
			return nil, fmt.Errorf("stage extension %s: %w", extensionLoader.Name(), err)
		}
	}
	return &ExtensionSnapshot{verbs: target.verbs, functions: target.functions, data: target.data, types: target.types, sources: target.sources, closers: target.closers}, nil
}

func (snapshot *ExtensionSnapshot) Name() string { return "ImmutableExtensionSnapshot" }
func (snapshot *ExtensionSnapshot) Load(ctx context.Context, target LoadTarget) error {
	if snapshot == nil || snapshot.retired.Load() || snapshot.closed.Load() {
		return fmt.Errorf("extension snapshot is retired")
	}
	for _, item := range snapshot.types {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := target.RegisterType(item.name, cloneTypeDefinition(item.definition)); err != nil {
			return err
		}
	}
	for _, item := range snapshot.functions {
		if err := target.RegisterFunction(item.name, item.implementation); err != nil {
			return err
		}
	}
	for _, item := range snapshot.data {
		if err := target.LoadData(item.path, cloneSnapshotValue(item.value)); err != nil {
			return err
		}
	}
	for _, item := range snapshot.verbs {
		spec := item.spec.clone()
		if item.descriptor != nil {
			descriptorTarget, ok := target.(DescriptorLoadTarget)
			if !ok {
				return fmt.Errorf("load target cannot accept executor descriptor for %q", spec.GetName())
			}
			if err := descriptorTarget.RegisterVerbDescriptor(&spec, cloneExecutorDescriptor(*item.descriptor)); err != nil {
				return err
			}
			continue
		}
		if err := target.RegisterVerb(&spec, item.executor); err != nil {
			return err
		}
	}
	if sourceTarget, ok := target.(SourceLoadTarget); ok {
		for _, source := range snapshot.sources {
			if err := sourceTarget.RegisterSource(SourceFile{Path: source.Path, Data: append([]byte(nil), source.Data...)}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (snapshot *ExtensionSnapshot) Acquire() (*ExtensionSnapshotHandle, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("extension snapshot is nil")
	}
	for {
		if snapshot.retired.Load() || snapshot.closed.Load() {
			return nil, fmt.Errorf("extension snapshot is retired")
		}
		snapshot.refs.Add(1)
		if !snapshot.retired.Load() && !snapshot.closed.Load() {
			return &ExtensionSnapshotHandle{snapshot: snapshot}, nil
		}
		if snapshot.refs.Add(-1) == 0 {
			_ = snapshot.closeIfUnused()
		}
	}
}
func (snapshot *ExtensionSnapshot) Retire() error {
	if snapshot == nil {
		return nil
	}
	snapshot.retired.Store(true)
	return snapshot.closeIfUnused()
}
func (snapshot *ExtensionSnapshot) Closed() bool { return snapshot != nil && snapshot.closed.Load() }
func (snapshot *ExtensionSnapshot) closeIfUnused() error {
	if !snapshot.retired.Load() || snapshot.refs.Load() != 0 || snapshot.closed.Load() {
		return nil
	}
	snapshot.closeMu.Lock()
	defer snapshot.closeMu.Unlock()
	if snapshot.closed.Load() || snapshot.refs.Load() != 0 {
		return nil
	}
	var result error
	for index := len(snapshot.closers) - 1; index >= 0; index-- {
		result = errors.Join(result, snapshot.closers[index].Close())
	}
	snapshot.closed.Store(true)
	return result
}

type ExtensionSnapshotHandle struct {
	snapshot *ExtensionSnapshot
	released atomic.Bool
}

func (handle *ExtensionSnapshotHandle) Snapshot() *ExtensionSnapshot {
	if handle == nil || handle.released.Load() {
		return nil
	}
	return handle.snapshot
}
func (handle *ExtensionSnapshotHandle) Release() error {
	if handle == nil || handle.snapshot == nil || !handle.released.CompareAndSwap(false, true) {
		return nil
	}
	if handle.snapshot.refs.Add(-1) < 0 {
		return fmt.Errorf("extension snapshot reference count became negative")
	}
	return handle.snapshot.closeIfUnused()
}

// ExtensionSnapshotManager is retained for embedded compatibility.
// Deprecated: ExecutionRuntime publishes the active snapshot with its checked generation.
type ExtensionSnapshotManager struct {
	active atomic.Pointer[ExtensionSnapshot]
}

func (manager *ExtensionSnapshotManager) Publish(snapshot *ExtensionSnapshot) error {
	if manager == nil || snapshot == nil {
		return fmt.Errorf("snapshot manager and snapshot are required")
	}
	if snapshot.retired.Load() || snapshot.closed.Load() {
		return fmt.Errorf("cannot publish retired snapshot")
	}
	previous := manager.active.Swap(snapshot)
	if previous != nil && previous != snapshot {
		_ = previous.Retire()
	}
	return nil
}
func (manager *ExtensionSnapshotManager) Acquire() (*ExtensionSnapshotHandle, error) {
	if manager == nil {
		return nil, fmt.Errorf("snapshot manager is nil")
	}
	snapshot := manager.active.Load()
	if snapshot == nil {
		return nil, fmt.Errorf("no active extension snapshot")
	}
	return snapshot.Acquire()
}
func (manager *ExtensionSnapshotManager) Close() error {
	if manager == nil {
		return nil
	}
	snapshot := manager.active.Swap(nil)
	if snapshot == nil {
		return nil
	}
	return snapshot.Retire()
}

type snapshotCaptureTarget struct {
	options                                                     StageOptions
	verbs                                                       []snapshotVerb
	functions                                                   []snapshotFunction
	data                                                        []snapshotData
	types                                                       []snapshotType
	sources                                                     []SourceFile
	closers                                                     []io.Closer
	verbNames, functionNames, typeNames, sourceNames, dataNames map[string]struct{}
	closerIDs                                                   map[uintptr]struct{}
	totalBytes                                                  int
}

func (target *snapshotCaptureTarget) RegisterVerb(spec VerbSpec, executor VerbExecutor) error {
	if len(target.verbs) >= target.options.MaxVerbs {
		return fmt.Errorf("extension verb limit exceeded")
	}
	immutable, err := captureImmutableVerbSpec(spec)
	if err != nil {
		return err
	}
	if executor == nil {
		return fmt.Errorf("extension verb %q executor is nil", immutable.name)
	}
	target.totalBytes += immutable.size()
	if target.totalBytes > target.options.MaxTotalBytes {
		return fmt.Errorf("extension snapshot total exceeds %d bytes", target.options.MaxTotalBytes)
	}
	if _, ok := target.verbNames[immutable.name]; ok {
		return fmt.Errorf("extension verb %q is duplicated", immutable.name)
	}
	target.verbNames[immutable.name] = struct{}{}
	target.verbs = append(target.verbs, snapshotVerb{spec: immutable, executor: executor})
	target.captureCloser(executor)
	return nil
}
func (target *snapshotCaptureTarget) RegisterVerbDescriptor(spec VerbSpec, descriptor ExecutorDescriptor) error {
	if len(target.verbs) >= target.options.MaxVerbs {
		return fmt.Errorf("extension verb limit exceeded")
	}
	immutable, err := captureImmutableVerbSpec(spec)
	if err != nil {
		return err
	}
	if strings.TrimSpace(descriptor.Type) == "" {
		return fmt.Errorf("extension verb %q descriptor type is required", immutable.name)
	}
	descriptor.VerbName = immutable.name
	descriptor = cloneExecutorDescriptor(descriptor)
	if _, ok := target.verbNames[immutable.name]; ok {
		return fmt.Errorf("extension verb %q is duplicated", immutable.name)
	}
	target.verbNames[immutable.name] = struct{}{}
	target.verbs = append(target.verbs, snapshotVerb{spec: immutable, descriptor: &descriptor})
	return nil
}

// AttachCloser adds a runtime-constructed resource to snapshot retirement.
// It must be called before the snapshot is published or acquired.
func (snapshot *ExtensionSnapshot) AttachCloser(closer io.Closer) error {
	if snapshot == nil || closer == nil {
		return fmt.Errorf("snapshot and closer are required")
	}
	snapshot.closeMu.Lock()
	defer snapshot.closeMu.Unlock()
	if snapshot.retired.Load() || snapshot.closed.Load() || snapshot.refs.Load() != 0 {
		return fmt.Errorf("cannot attach a resource after snapshot publication")
	}
	snapshot.closers = append(snapshot.closers, closer)
	return nil
}

func cloneExecutorDescriptor(descriptor ExecutorDescriptor) ExecutorDescriptor {
	var config map[string]interface{}
	if descriptor.Config != nil {
		config, _ = cloneSnapshotValue(descriptor.Config).(map[string]interface{})
	}
	return ExecutorDescriptor{Type: descriptor.Type, VerbName: descriptor.VerbName, Config: config}
}

func (target *snapshotCaptureTarget) RegisterFunction(name string, implementation any) error {
	if len(target.functions) >= target.options.MaxFunctions {
		return fmt.Errorf("extension function limit exceeded")
	}
	if strings.TrimSpace(name) == "" || implementation == nil {
		return fmt.Errorf("invalid extension function")
	}
	if _, ok := target.functionNames[name]; ok {
		return fmt.Errorf("extension function %q is duplicated", name)
	}
	target.functionNames[name] = struct{}{}
	target.functions = append(target.functions, snapshotFunction{name, implementation})
	target.captureCloser(implementation)
	return nil
}
func (target *snapshotCaptureTarget) LoadData(path string, value any) error {
	if len(target.data) >= target.options.MaxDataEntries {
		return fmt.Errorf("extension data limit exceeded")
	}
	if _, ok := target.dataNames[path]; ok {
		return fmt.Errorf("extension data %q is duplicated", path)
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode extension data %q: %w", path, err)
	}
	target.totalBytes += len(payload)
	if target.totalBytes > target.options.MaxTotalBytes {
		return fmt.Errorf("extension snapshot total exceeds %d bytes", target.options.MaxTotalBytes)
	}
	target.dataNames[path] = struct{}{}
	target.data = append(target.data, snapshotData{path, cloneSnapshotValue(value)})
	return nil
}
func (target *snapshotCaptureTarget) RegisterType(name string, definition TypeDefinition) error {
	if len(target.types) >= target.options.MaxTypes {
		return fmt.Errorf("extension type limit exceeded")
	}
	if _, ok := target.typeNames[name]; ok {
		return fmt.Errorf("extension type %q is duplicated", name)
	}
	payload, err := json.Marshal(definition)
	if err != nil {
		return fmt.Errorf("encode extension type %q: %w", name, err)
	}
	target.totalBytes += len(payload)
	if target.totalBytes > target.options.MaxTotalBytes {
		return fmt.Errorf("extension snapshot total exceeds %d bytes", target.options.MaxTotalBytes)
	}
	target.typeNames[name] = struct{}{}
	target.types = append(target.types, snapshotType{name, cloneTypeDefinition(definition)})
	return nil
}
func (target *snapshotCaptureTarget) RegisterSource(source SourceFile) error {
	if len(target.sources) >= target.options.MaxSources {
		return fmt.Errorf("extension source limit exceeded")
	}
	if len(source.Data) > target.options.MaxSourceBytes {
		return fmt.Errorf("extension source %q exceeds %d bytes", source.Path, target.options.MaxSourceBytes)
	}
	target.totalBytes += len(source.Data)
	if target.totalBytes > target.options.MaxTotalBytes {
		return fmt.Errorf("extension source total exceeds %d bytes", target.options.MaxTotalBytes)
	}
	if _, ok := target.sourceNames[source.Path]; ok {
		return fmt.Errorf("extension source %q is duplicated", source.Path)
	}
	target.sourceNames[source.Path] = struct{}{}
	target.sources = append(target.sources, SourceFile{Path: source.Path, Data: append([]byte(nil), source.Data...)})
	return nil
}
func (target *snapshotCaptureTarget) captureCloser(value any) {
	closer, ok := value.(io.Closer)
	if !ok {
		return
	}
	pointer := reflect.ValueOf(closer)
	if pointer.Kind() == reflect.Pointer && !pointer.IsNil() {
		id := pointer.Pointer()
		if _, ok := target.closerIDs[id]; ok {
			return
		}
		target.closerIDs[id] = struct{}{}
	}
	target.closers = append(target.closers, closer)
}
func (target *snapshotCaptureTarget) closeCandidates() error {
	var result error
	for index := len(target.closers) - 1; index >= 0; index-- {
		result = errors.Join(result, target.closers[index].Close())
	}
	return result
}

type immutableVerbSpec struct {
	name, description, result, inverse string
	capabilities                       []string
	resources                          []immutableResourceSpec
	arguments                          map[string]string
	required                           []string
}
type immutableResourceSpec struct {
	name         string
	capabilities []string
}

func captureImmutableVerbSpec(spec VerbSpec) (immutableVerbSpec, error) {
	if spec == nil {
		return immutableVerbSpec{}, fmt.Errorf("extension verb specification is nil")
	}
	result := immutableVerbSpec{name: spec.GetName(), description: spec.GetDescription(), result: spec.GetReturnType(), inverse: spec.GetInverseVerb(), capabilities: append([]string(nil), spec.GetCapabilities()...), arguments: cloneSnapshotStrings(spec.GetArgTypes()), required: append([]string(nil), spec.GetRequiredArgs()...)}
	for _, resource := range spec.GetResources() {
		if resource == nil {
			return immutableVerbSpec{}, fmt.Errorf("extension verb %q has nil resource", result.name)
		}
		result.resources = append(result.resources, immutableResourceSpec{name: resource.GetResource(), capabilities: append([]string(nil), resource.GetCapabilities()...)})
	}
	return result, nil
}
func (spec immutableVerbSpec) size() int {
	size := len(spec.name) + len(spec.description) + len(spec.result) + len(spec.inverse)
	for _, value := range spec.capabilities {
		size += len(value)
	}
	for key, value := range spec.arguments {
		size += len(key) + len(value)
	}
	for _, value := range spec.required {
		size += len(value)
	}
	for _, resource := range spec.resources {
		size += len(resource.name)
		for _, value := range resource.capabilities {
			size += len(value)
		}
	}
	return size
}
func (spec immutableVerbSpec) clone() immutableVerbSpec {
	result := spec
	result.capabilities = append([]string(nil), spec.capabilities...)
	result.arguments = cloneSnapshotStrings(spec.arguments)
	result.required = append([]string(nil), spec.required...)
	result.resources = append([]immutableResourceSpec(nil), spec.resources...)
	return result
}
func (spec *immutableVerbSpec) GetName() string        { return spec.name }
func (spec *immutableVerbSpec) GetDescription() string { return spec.description }
func (spec *immutableVerbSpec) GetCapabilities() []string {
	return append([]string(nil), spec.capabilities...)
}
func (spec *immutableVerbSpec) GetResources() []ResourceSpec {
	result := make([]ResourceSpec, len(spec.resources))
	for index := range spec.resources {
		resource := spec.resources[index]
		result[index] = resource
	}
	return result
}
func (spec *immutableVerbSpec) GetArgTypes() map[string]string {
	return cloneSnapshotStrings(spec.arguments)
}
func (spec *immutableVerbSpec) GetRequiredArgs() []string {
	return append([]string(nil), spec.required...)
}
func (spec *immutableVerbSpec) GetReturnType() string      { return spec.result }
func (spec *immutableVerbSpec) GetInverseVerb() string     { return spec.inverse }
func (resource immutableResourceSpec) GetResource() string { return resource.name }
func (resource immutableResourceSpec) GetCapabilities() []string {
	return append([]string(nil), resource.capabilities...)
}
func cloneSnapshotStrings(input map[string]string) map[string]string {
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}
func cloneTypeDefinition(input TypeDefinition) TypeDefinition {
	result := input
	result.Required = append([]string(nil), input.Required...)
	result.Properties = cloneSnapshotValue(input.Properties)
	return result
}
func cloneSnapshotValue(value any) any {
	switch typed := value.(type) {
	case map[string]string:
		result := make(map[string]string, len(typed))
		for key, item := range typed {
			result[key] = item
		}
		return result
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[key] = cloneSnapshotValue(item)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = cloneSnapshotValue(item)
		}
		return result
	case []string:
		return append([]string(nil), typed...)
	default:
		return typed
	}
}
