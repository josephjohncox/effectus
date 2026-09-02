package invocation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

// DescriptorType identifies a supported executor transport.
type DescriptorType string

const (
	DescriptorHTTP     DescriptorType = "http"
	DescriptorGRPC     DescriptorType = "grpc"
	DescriptorStream   DescriptorType = "stream"
	DescriptorOCI      DescriptorType = "oci"
	DescriptorEmbedded DescriptorType = "embedded"
)

var digestPinnedOCIReference = regexp.MustCompile(`^.+@sha256:[0-9a-f]{64}$`)

// DescriptorSpec is construction input for an immutable Descriptor. NewDescriptor
// copies all maps and rejects reserved invocation metadata.
type DescriptorSpec struct {
	Type       DescriptorType
	ResolverID string
	Reference  string
	Headers    map[string]string
	Settings   map[string]string
}

// Descriptor is canonical resolver input. Its fields are private so a value
// cannot be changed after NewDescriptor or ParseDescriptor returns.
type Descriptor struct {
	descriptor descriptorDocument
}

type descriptorDocument struct {
	Type       DescriptorType `json:"type"`
	ResolverID string         `json:"resolver_id,omitempty"`
	Reference  string         `json:"reference,omitempty"`
	Headers    []DescriptorKV `json:"headers,omitempty"`
	Settings   []DescriptorKV `json:"settings,omitempty"`
}

// DescriptorKV is one canonically ordered descriptor value.
type DescriptorKV struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// NewDescriptor validates and freezes canonical executor resolver input.
func NewDescriptor(spec DescriptorSpec) (Descriptor, error) {
	spec.ResolverID = strings.TrimSpace(spec.ResolverID)
	spec.Reference = strings.TrimSpace(spec.Reference)
	if err := validateDescriptorType(spec.Type); err != nil {
		return Descriptor{}, err
	}
	if spec.Type == DescriptorOCI && !digestPinnedOCIReference.MatchString(spec.Reference) {
		return Descriptor{}, fmt.Errorf("invocation descriptor OCI reference must be digest-pinned")
	}
	headers, err := canonicalDescriptorValues(spec.Headers, true)
	if err != nil {
		return Descriptor{}, err
	}
	settings, err := canonicalDescriptorValues(spec.Settings, false)
	if err != nil {
		return Descriptor{}, err
	}
	return Descriptor{descriptor: descriptorDocument{
		Type: spec.Type, ResolverID: spec.ResolverID, Reference: spec.Reference,
		Headers: headers, Settings: settings,
	}}, nil
}

func validateDescriptorType(value DescriptorType) error {
	switch value {
	case DescriptorHTTP, DescriptorGRPC, DescriptorStream, DescriptorOCI, DescriptorEmbedded:
		return nil
	default:
		return fmt.Errorf("unknown invocation descriptor type %q", value)
	}
}

func canonicalDescriptorValues(values map[string]string, headers bool) ([]DescriptorKV, error) {
	names := make([]string, 0, len(values))
	for rawName := range values {
		names = append(names, rawName)
	}
	sort.Strings(names)
	seenFoldedNames := make(map[string]string, len(names))
	for _, rawName := range names {
		name := strings.TrimSpace(rawName)
		if name == "" || name != rawName {
			return nil, fmt.Errorf("invocation descriptor contains a non-canonical key %q", rawName)
		}
		if headers {
			foldedName := strings.ToLower(name)
			if previous, duplicate := seenFoldedNames[foldedName]; duplicate {
				return nil, fmt.Errorf("invocation descriptor headers %q and %q collide case-insensitively", previous, name)
			}
			seenFoldedNames[foldedName] = name
			if _, reserved := reservedHTTPHeaders[foldedName]; reserved || strings.HasPrefix(foldedName, "x-effectus-") {
				return nil, fmt.Errorf("invocation descriptor header %q is reserved", name)
			}
		}
	}
	result := make([]DescriptorKV, 0, len(names))
	for _, name := range names {
		result = append(result, DescriptorKV{Name: name, Value: values[name]})
	}
	return result, nil
}

// Type returns the executor transport type.
func (descriptor Descriptor) Type() DescriptorType { return descriptor.descriptor.Type }

// ResolverID returns the stable resolver implementation identity. An empty ID
// denotes an embedded callback and is invalid in a production Generation.
func (descriptor Descriptor) ResolverID() string { return descriptor.descriptor.ResolverID }

// Reference returns the transport endpoint or digest-pinned artifact reference.
func (descriptor Descriptor) Reference() string { return descriptor.descriptor.Reference }

// Headers returns a copy of static non-reserved transport headers.
func (descriptor Descriptor) Headers() map[string]string {
	return descriptorValuesMap(descriptor.descriptor.Headers)
}

// Settings returns a copy of transport-specific scalar settings.
func (descriptor Descriptor) Settings() map[string]string {
	return descriptorValuesMap(descriptor.descriptor.Settings)
}

func descriptorValuesMap(values []DescriptorKV) map[string]string {
	result := make(map[string]string, len(values))
	for _, value := range values {
		result[value.Name] = value.Value
	}
	return result
}

// CanonicalJSON returns the unique JSON encoding of descriptor.
func (descriptor Descriptor) CanonicalJSON() ([]byte, error) {
	if err := descriptor.validate(); err != nil {
		return nil, err
	}
	return json.Marshal(descriptor.descriptor)
}

// MarshalJSON implements json.Marshaler using the canonical representation.
func (descriptor Descriptor) MarshalJSON() ([]byte, error) { return descriptor.CanonicalJSON() }

// UnmarshalJSON implements strict JSON decoding. Unknown and duplicate fields
// are rejected, and the receiver changes only after complete validation.
func (descriptor *Descriptor) UnmarshalJSON(data []byte) error {
	if descriptor == nil {
		return fmt.Errorf("invocation descriptor receiver is nil")
	}
	parsed, err := ParseDescriptor(data)
	if err != nil {
		return err
	}
	*descriptor = parsed
	return nil
}

// ParseDescriptor strictly decodes a canonical executor descriptor.
func ParseDescriptor(data []byte) (Descriptor, error) {
	if err := rejectDuplicateJSONNames(data); err != nil {
		return Descriptor{}, fmt.Errorf("decode invocation descriptor: %w", err)
	}
	var document descriptorDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return Descriptor{}, fmt.Errorf("decode invocation descriptor: %w", err)
	}
	if decoder.More() {
		return Descriptor{}, fmt.Errorf("decode invocation descriptor: trailing JSON value")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Descriptor{}, fmt.Errorf("decode invocation descriptor: trailing JSON value")
		}
		return Descriptor{}, fmt.Errorf("decode invocation descriptor: %w", err)
	}
	descriptor := Descriptor{descriptor: document}
	if err := descriptor.validate(); err != nil {
		return Descriptor{}, err
	}
	return descriptor, nil
}

func (descriptor Descriptor) validate() error {
	if err := validateDescriptorType(descriptor.descriptor.Type); err != nil {
		return err
	}
	if descriptor.descriptor.ResolverID != strings.TrimSpace(descriptor.descriptor.ResolverID) || descriptor.descriptor.Reference != strings.TrimSpace(descriptor.descriptor.Reference) {
		return fmt.Errorf("invocation descriptor identifiers must be normalized")
	}
	if descriptor.descriptor.Type == DescriptorOCI && !digestPinnedOCIReference.MatchString(descriptor.descriptor.Reference) {
		return fmt.Errorf("invocation descriptor OCI reference must be digest-pinned")
	}
	if err := validateCanonicalDescriptorValues(descriptor.descriptor.Headers, true); err != nil {
		return err
	}
	return validateCanonicalDescriptorValues(descriptor.descriptor.Settings, false)
}

func validateCanonicalDescriptorValues(values []DescriptorKV, headers bool) error {
	previous := ""
	seenFoldedNames := make(map[string]string, len(values))
	for index, value := range values {
		if value.Name == "" || value.Name != strings.TrimSpace(value.Name) {
			return fmt.Errorf("invocation descriptor contains a non-canonical key %q", value.Name)
		}
		if index != 0 && value.Name <= previous {
			return fmt.Errorf("invocation descriptor keys are duplicated or not ordered")
		}
		if headers {
			lower := strings.ToLower(value.Name)
			if previousName, duplicate := seenFoldedNames[lower]; duplicate {
				return fmt.Errorf("invocation descriptor headers %q and %q collide case-insensitively", previousName, value.Name)
			}
			seenFoldedNames[lower] = value.Name
			if _, reserved := reservedHTTPHeaders[lower]; reserved || strings.HasPrefix(lower, "x-effectus-") {
				return fmt.Errorf("invocation descriptor header %q is reserved", value.Name)
			}
		}
		previous = value.Name
	}
	return nil
}

// Resolver constructs an executor and returns the resource owned by the
// generation. The closer can be nil when the executor owns no resources.
type Resolver interface {
	Resolve(context.Context, Descriptor) (Executor, io.Closer, error)
}

// ResolverFunc adapts a function to Resolver.
type ResolverFunc func(context.Context, Descriptor) (Executor, io.Closer, error)

func (function ResolverFunc) Resolve(ctx context.Context, descriptor Descriptor) (Executor, io.Closer, error) {
	return function(ctx, descriptor)
}

// Registry is an immutable resolver registry keyed by stable resolver ID.
type Registry struct {
	resolvers map[string]Resolver
}

// ResolverRegistration binds one stable ID to one resolver.
type ResolverRegistration struct {
	ID       string
	Resolver Resolver
}

// NewRegistry constructs an immutable registry and rejects duplicate IDs.
func NewRegistry(registrations []ResolverRegistration) (*Registry, error) {
	registry := &Registry{resolvers: make(map[string]Resolver, len(registrations))}
	for _, registration := range registrations {
		id := strings.TrimSpace(registration.ID)
		if id == "" || id != registration.ID || registration.Resolver == nil {
			return nil, fmt.Errorf("invocation resolver registration is invalid")
		}
		if _, duplicate := registry.resolvers[id]; duplicate {
			return nil, fmt.Errorf("invocation resolver %q is registered more than once", id)
		}
		registry.resolvers[id] = registration.Resolver
	}
	return registry, nil
}

// Resolve resolves a descriptor or fails closed when its resolver is absent.
func (registry *Registry) Resolve(ctx context.Context, descriptor Descriptor) (Executor, io.Closer, error) {
	if registry == nil {
		return nil, nil, fmt.Errorf("invocation resolver registry is nil")
	}
	if err := descriptor.validate(); err != nil {
		return nil, nil, err
	}
	id := descriptor.ResolverID()
	if id == "" {
		return nil, nil, fmt.Errorf("callback-only invocation descriptor cannot be resolved")
	}
	resolver := registry.resolvers[id]
	if resolver == nil {
		return nil, nil, fmt.Errorf("invocation resolver %q is not registered", id)
	}
	executor, closer, err := resolver.Resolve(ctx, descriptor)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve invocation descriptor %q: %w", id, err)
	}
	if executor == nil {
		if closer != nil {
			_ = closer.Close()
		}
		return nil, nil, fmt.Errorf("invocation resolver %q returned a nil executor", id)
	}
	return executor, closer, nil
}

// ResolverDescriptorProvider exposes the canonical descriptor for an executor.
type ResolverDescriptorProvider interface {
	InvocationResolverDescriptor() (Descriptor, error)
}

func rejectDuplicateJSONNames(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return scanJSONValue(decoder)
}

func scanJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			nameToken, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := nameToken.(string)
			if !ok {
				return fmt.Errorf("object key is not a string")
			}
			if _, duplicate := seen[name]; duplicate {
				return fmt.Errorf("duplicate object field %q", name)
			}
			seen[name] = struct{}{}
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
}
