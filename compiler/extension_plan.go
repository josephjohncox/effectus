package compiler

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/effectus/effectus-go/ir"
	"github.com/effectus/effectus-go/loader"
	"github.com/effectus/effectus-go/schema"
	"github.com/effectus/effectus-go/schema/verb"
)

var ErrUnsupportedExtensionWorkflow = errors.New("unsupported extension workflow")

// CheckedFunctionProvider supplies the immutable contract required to expose a
// function to checked predicates. Plain callbacks remain generation metadata.
type CheckedFunctionProvider interface {
	CheckedFunctionContract() ir.FunctionContract
	CheckedFunctionImplementation() any
	CheckedFunctionDescriptor() any
}

type extensionCandidateTarget struct {
	delegate    *schema.LoaderAdapter
	sources     []loader.SourceFile
	types       map[string]loader.TypeDefinition
	facts       map[string]string
	functions   map[string]interface{}
	initialData map[string]interface{}
}

func newExtensionCandidateTarget(registry *schema.Registry, verbs *verb.Registry) *extensionCandidateTarget {
	return &extensionCandidateTarget{
		delegate: schema.NewLoaderAdapter(registry, verbs),
		types:    make(map[string]loader.TypeDefinition), facts: make(map[string]string),
		functions: make(map[string]interface{}), initialData: make(map[string]interface{}),
	}
}

func (target *extensionCandidateTarget) RegisterVerb(spec loader.VerbSpec, executor loader.VerbExecutor) error {
	captured, err := captureExtensionVerbSpec(spec)
	if err != nil {
		return err
	}
	if err := validateExtensionCapabilities(captured); err != nil {
		return err
	}
	return target.delegate.RegisterVerb(captured, executor)
}

type capturedExtensionVerbSpec struct {
	name, description, resultType, inverse string
	capabilities                           []string
	resources                              []loader.ResourceSpec
	arguments                              map[string]string
	required                               []string
}

func captureExtensionVerbSpec(spec loader.VerbSpec) (*capturedExtensionVerbSpec, error) {
	if spec == nil {
		return nil, fmt.Errorf("extension verb specification is nil")
	}
	captured := &capturedExtensionVerbSpec{
		name: spec.GetName(), description: spec.GetDescription(), resultType: spec.GetReturnType(), inverse: spec.GetInverseVerb(),
		capabilities: append([]string(nil), spec.GetCapabilities()...), arguments: cloneStringValues(spec.GetArgTypes()),
		required: append([]string(nil), spec.GetRequiredArgs()...),
	}
	for _, resource := range spec.GetResources() {
		if resource == nil {
			return nil, fmt.Errorf("verb %q has a nil resource", captured.name)
		}
		captured.resources = append(captured.resources, capturedExtensionResource{
			name: resource.GetResource(), capabilities: append([]string(nil), resource.GetCapabilities()...),
		})
	}
	return captured, nil
}

func (spec *capturedExtensionVerbSpec) GetName() string        { return spec.name }
func (spec *capturedExtensionVerbSpec) GetDescription() string { return spec.description }
func (spec *capturedExtensionVerbSpec) GetCapabilities() []string {
	return append([]string(nil), spec.capabilities...)
}
func (spec *capturedExtensionVerbSpec) GetResources() []loader.ResourceSpec {
	return append([]loader.ResourceSpec(nil), spec.resources...)
}
func (spec *capturedExtensionVerbSpec) GetArgTypes() map[string]string {
	return cloneStringValues(spec.arguments)
}
func (spec *capturedExtensionVerbSpec) GetRequiredArgs() []string {
	return append([]string(nil), spec.required...)
}
func (spec *capturedExtensionVerbSpec) GetReturnType() string  { return spec.resultType }
func (spec *capturedExtensionVerbSpec) GetInverseVerb() string { return spec.inverse }

type capturedExtensionResource struct {
	name         string
	capabilities []string
}

func (resource capturedExtensionResource) GetResource() string { return resource.name }
func (resource capturedExtensionResource) GetCapabilities() []string {
	return append([]string(nil), resource.capabilities...)
}

func (target *extensionCandidateTarget) RegisterFunction(name string, function interface{}) error {
	if strings.TrimSpace(name) == "" || name != strings.TrimSpace(name) || function == nil {
		return fmt.Errorf("invalid extension function %q", name)
	}
	if _, duplicate := target.functions[name]; duplicate {
		return fmt.Errorf("extension function %q is already registered", name)
	}
	target.functions[name] = function
	return target.delegate.RegisterFunction(name, function)
}

func (target *extensionCandidateTarget) LoadData(path string, value interface{}) error {
	if strings.TrimSpace(path) == "" || path != strings.TrimSpace(path) || strings.HasPrefix(path, "__") {
		return fmt.Errorf("invalid extension data path %q", path)
	}
	typeName, err := inferExtensionFactType(value)
	if err != nil {
		return fmt.Errorf("extension data %q: %w", path, err)
	}
	if previous, exists := target.facts[path]; exists && previous != typeName {
		return fmt.Errorf("extension data %q changes type from %s to %s", path, previous, typeName)
	}
	if _, duplicate := target.initialData[path]; duplicate {
		return fmt.Errorf("extension data %q is already registered", path)
	}
	target.facts[path] = typeName
	target.initialData[path] = cloneExtensionValue(value)
	return target.delegate.LoadData(path, value)
}

func (target *extensionCandidateTarget) RegisterType(name string, definition loader.TypeDefinition) error {
	if _, duplicate := target.types[name]; duplicate {
		return fmt.Errorf("extension type %q is already registered", name)
	}
	target.types[name] = cloneLoaderTypeDefinition(definition)
	return target.delegate.RegisterType(name, definition)
}

func (target *extensionCandidateTarget) RegisterSource(source loader.SourceFile) error {
	path := filepath.ToSlash(filepath.Clean(strings.TrimSpace(source.Path)))
	if path == ".." || strings.HasPrefix(path, "../") || filepath.IsAbs(path) {
		return fmt.Errorf("extension source path %q escapes the snapshot", source.Path)
	}
	if filepath.Ext(path) != ".eff" && filepath.Ext(path) != ".effx" {
		return fmt.Errorf("extension source %q must use .eff or .effx", source.Path)
	}
	for _, existing := range target.sources {
		if existing.Path == path {
			return fmt.Errorf("extension source %q is already registered", path)
		}
	}
	target.sources = append(target.sources, loader.SourceFile{Path: path, Data: append([]byte(nil), source.Data...)})
	return nil
}

func cloneLoaderTypeDefinition(definition loader.TypeDefinition) loader.TypeDefinition {
	clone := definition
	clone.Required = append([]string(nil), definition.Required...)
	clone.Properties = cloneExtensionValue(definition.Properties)
	return clone
}

func cloneInterfaceMap(values map[string]interface{}) map[string]interface{} {
	clone := make(map[string]interface{}, len(values))
	for name, value := range values {
		clone[name] = cloneExtensionValue(value)
	}
	return clone
}

func cloneExtensionValue(value interface{}) interface{} {
	switch value := value.(type) {
	case map[string]interface{}:
		clone := make(map[string]interface{}, len(value))
		for name, item := range value {
			clone[name] = cloneExtensionValue(item)
		}
		return clone
	case []interface{}:
		clone := make([]interface{}, len(value))
		for index, item := range value {
			clone[index] = cloneExtensionValue(item)
		}
		return clone
	case []string:
		return append([]string(nil), value...)
	default:
		return value
	}
}

func validateExtensionCapabilities(spec loader.VerbSpec) error {
	if spec == nil || strings.TrimSpace(spec.GetName()) == "" || spec.GetName() != strings.TrimSpace(spec.GetName()) {
		return fmt.Errorf("extension verb name is invalid")
	}
	declared, err := parseCapabilityList(spec.GetCapabilities(), false)
	if err != nil {
		return fmt.Errorf("verb %q capabilities: %w", spec.GetName(), err)
	}
	if declared&verb.CapExclusive != 0 && declared&verb.CapCommutative != 0 {
		return fmt.Errorf("verb %q cannot be both exclusive and commutative", spec.GetName())
	}
	resources := make(map[string]struct{})
	for _, resource := range spec.GetResources() {
		if resource == nil || strings.TrimSpace(resource.GetResource()) == "" || resource.GetResource() != strings.TrimSpace(resource.GetResource()) {
			return fmt.Errorf("verb %q has an invalid resource", spec.GetName())
		}
		if _, duplicate := resources[resource.GetResource()]; duplicate {
			return fmt.Errorf("verb %q repeats resource %q", spec.GetName(), resource.GetResource())
		}
		resources[resource.GetResource()] = struct{}{}
		required, err := parseCapabilityList(resource.GetCapabilities(), true)
		if err != nil {
			return fmt.Errorf("verb %q resource %q capabilities: %w", spec.GetName(), resource.GetResource(), err)
		}
		if required&declared != required {
			return fmt.Errorf("verb %q resource %q requires capabilities not declared by the verb", spec.GetName(), resource.GetResource())
		}
	}
	return nil
}

func parseCapabilityList(values []string, requireAccess bool) (verb.Capability, error) {
	seen := make(map[string]struct{}, len(values))
	result := verb.CapNone
	access := verb.CapNone
	for _, value := range values {
		if value != strings.TrimSpace(value) {
			return verb.CapNone, fmt.Errorf("capability %q has surrounding whitespace", value)
		}
		if _, duplicate := seen[value]; duplicate {
			return verb.CapNone, fmt.Errorf("duplicate capability %q", value)
		}
		seen[value] = struct{}{}
		var capability verb.Capability
		switch value {
		case "read":
			capability = verb.CapRead
		case "write":
			capability = verb.CapWrite
		case "create":
			capability = verb.CapCreate
		case "delete":
			capability = verb.CapDelete
		case "idempotent":
			capability = verb.CapIdempotent
		case "exclusive":
			capability = verb.CapExclusive
		case "commutative":
			capability = verb.CapCommutative
		default:
			return verb.CapNone, fmt.Errorf("unknown capability %q", value)
		}
		result |= capability
		if capability&(verb.CapRead|verb.CapWrite|verb.CapCreate|verb.CapDelete) != 0 {
			access |= capability
		}
	}
	if requireAccess && access == verb.CapNone {
		return verb.CapNone, fmt.Errorf("resource must declare an access capability")
	}
	return result, nil
}

func buildExtensionEnvironment(target *extensionCandidateTarget, compiled map[string]*CompiledVerbSpec) (ir.Environment, error) {
	environment := ir.Environment{
		Facts: cloneStringValues(target.facts), Verbs: make(map[string]ir.VerbContract, len(compiled)),
		Functions: make(map[string]ir.FunctionContract), Types: make(map[string]ir.TypeDefinition, len(target.types)),
	}
	for name, function := range target.functions {
		if provider, ok := function.(CheckedFunctionProvider); ok {
			contract := provider.CheckedFunctionContract()
			if !contract.Pure || !contract.Total {
				return ir.Environment{}, fmt.Errorf("checked function %q must be pure and total", name)
			}
			environment.Functions[name] = contract
		}
	}
	for name, definition := range target.types {
		converted, err := convertExtensionType(name, definition)
		if err != nil {
			return ir.Environment{}, err
		}
		environment.Types[name] = converted
	}
	for name, compiledVerb := range compiled {
		if compiledVerb == nil || compiledVerb.Spec == nil {
			return ir.Environment{}, fmt.Errorf("verb %q has no compiled specification", name)
		}
		if compiledVerb.Spec.Inverse != "" {
			if _, exists := compiled[compiledVerb.Spec.Inverse]; !exists {
				return ir.Environment{}, fmt.Errorf("verb %q references unknown inverse %q", name, compiledVerb.Spec.Inverse)
			}
		}
		contract := ir.VerbContract{
			Arguments:    cloneStringValues(compiledVerb.Spec.ArgTypes),
			RequiredArgs: append([]string(nil), compiledVerb.Spec.RequiredArgs...), ResultType: compiledVerb.Spec.ReturnType,
			InverseVerb: compiledVerb.Spec.Inverse, RetryPolicy: ir.RetryPolicy{MaxAttempts: 1},
		}
		if compiledVerb.Spec.Capability&verb.CapIdempotent != 0 {
			// A declaration proves only that Effectus supplies a stable key. It
			// does not prove destination-side deduplication after an unknown outcome.
			contract.IdempotencyPolicy = ir.IdempotencyKeyRequired
		}
		contract.FencingRequired = compiledVerb.Spec.Capability&verb.CapExclusive != 0
		environment.Verbs[name] = contract
	}
	for name, contract := range environment.Verbs {
		if contract.InverseVerb == "" {
			continue
		}
		inverse := environment.Verbs[contract.InverseVerb]
		for _, argument := range inverse.RequiredArgs {
			forwardType, ok := contract.Arguments[argument]
			if !ok || forwardType != inverse.Arguments[argument] {
				return ir.Environment{}, fmt.Errorf("verb %q inverse %q requires incompatible argument %q", name, contract.InverseVerb, argument)
			}
		}
	}
	if _, err := ir.EnvironmentDigest(environment); err != nil {
		return ir.Environment{}, fmt.Errorf("extension IR environment: %w", err)
	}
	return environment, nil
}

func convertExtensionType(name string, definition loader.TypeDefinition) (ir.TypeDefinition, error) {
	if strings.TrimSpace(name) == "" || name != strings.TrimSpace(name) {
		return ir.TypeDefinition{}, fmt.Errorf("extension type name %q is invalid", name)
	}
	if definition.Name != "" && definition.Name != name {
		return ir.TypeDefinition{}, fmt.Errorf("extension type %q declares mismatched name %q", name, definition.Name)
	}
	switch strings.ToLower(strings.TrimSpace(definition.Type)) {
	case "object":
		properties, ok := definition.Properties.(map[string]interface{})
		if !ok {
			return ir.TypeDefinition{}, fmt.Errorf("extension type %q object properties must be an object", name)
		}
		fields := make(map[string]string, len(properties))
		for field, raw := range properties {
			fieldDefinition, ok := raw.(map[string]interface{})
			if !ok {
				return ir.TypeDefinition{}, fmt.Errorf("extension type %q field %q must be an object", name, field)
			}
			for property := range fieldDefinition {
				if property != "type" && property != "description" {
					return ir.TypeDefinition{}, fmt.Errorf("extension type %q field %q has unsupported property %q", name, field, property)
				}
			}
			rawType, ok := fieldDefinition["type"].(string)
			if !ok {
				return ir.TypeDefinition{}, fmt.Errorf("extension type %q field %q needs a string type", name, field)
			}
			fieldType, err := extensionTypeName(rawType)
			if err != nil {
				return ir.TypeDefinition{}, fmt.Errorf("extension type %q field %q: %w", name, field, err)
			}
			fields[field] = fieldType
		}
		return ir.TypeDefinition{Kind: ir.TypeKindObject, Fields: fields, RequiredFields: append([]string(nil), definition.Required...)}, nil
	case "array":
		properties, ok := definition.Properties.(map[string]interface{})
		if !ok {
			return ir.TypeDefinition{}, fmt.Errorf("extension type %q array properties must contain items", name)
		}
		if len(properties) != 1 {
			return ir.TypeDefinition{}, fmt.Errorf("extension type %q array has unsupported properties", name)
		}
		items, ok := properties["items"].(map[string]interface{})
		if !ok {
			return ir.TypeDefinition{}, fmt.Errorf("extension type %q array needs an items object", name)
		}
		if len(items) != 1 {
			return ir.TypeDefinition{}, fmt.Errorf("extension type %q array item has unsupported properties", name)
		}
		rawType, ok := items["type"].(string)
		if !ok {
			return ir.TypeDefinition{}, fmt.Errorf("extension type %q array needs an item type", name)
		}
		element, err := extensionTypeName(rawType)
		if err != nil {
			return ir.TypeDefinition{}, fmt.Errorf("extension type %q array: %w", name, err)
		}
		return ir.TypeDefinition{Kind: ir.TypeKindList, ElementType: element}, nil
	default:
		return ir.TypeDefinition{}, fmt.Errorf("extension type %q: %w: type kind %q", name, ErrUnsupportedExtensionWorkflow, definition.Type)
	}
}

func extensionTypeName(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "boolean", "bool":
		return "bool", nil
	case "integer", "int", "int32", "int64":
		return "int", nil
	case "number", "float", "float32", "float64", "double":
		return "float", nil
	case "string":
		return "string", nil
	case "bytes":
		return "bytes", nil
	default:
		if strings.TrimSpace(value) == value && value != "" {
			return value, nil
		}
		return "", fmt.Errorf("invalid type %q", value)
	}
}

func inferExtensionFactType(value interface{}) (string, error) {
	switch value := value.(type) {
	case bool:
		return "bool", nil
	case string:
		return "string", nil
	case json.Number:
		if _, err := value.Int64(); err == nil {
			return "int", nil
		}
		if _, err := value.Float64(); err == nil {
			return "float", nil
		}
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32:
		return "int", nil
	case uint64:
		if value <= uint64(^uint64(0)>>1) {
			return "int", nil
		}
	case float32, float64:
		return "float", nil
	}
	return "", fmt.Errorf("%w: fact value type %T needs an explicit workflow fact declaration", ErrUnsupportedExtensionWorkflow, value)
}

func cloneStringValues(source map[string]string) map[string]string {
	clone := make(map[string]string, len(source))
	for name, value := range source {
		clone[name] = value
	}
	return clone
}
