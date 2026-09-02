// Package bundle defines the portable source input to checked compilation.
// A SourceBundle never contains checked IR or mutable runtime state.
package bundle

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
)

const (
	// FormatVersion is the only source bundle format accepted by this release.
	FormatVersion   = "effectus.source-bundle.v1"
	ociManifestPath = "effectus/source-bundle.json"
)

// Source is one normalized .eff or .effx source file.
type Source struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Spec is construction input for an immutable SourceBundle.
type Spec struct {
	Name        string
	Version     string
	Sources     []Source
	Environment ir.Environment
	Executors   map[string]invocation.Descriptor
	Metadata    map[string]string
}

// SourceBundle is immutable after construction. Accessors return copies.
type SourceBundle struct {
	document sourceDocument
}

type sourceDocument struct {
	FormatVersion string                    `json:"format_version"`
	Name          string                    `json:"name"`
	Version       string                    `json:"version"`
	Sources       []Source                  `json:"sources"`
	Environment   ir.Environment            `json:"environment"`
	Executors     []namedDescriptor         `json:"executors,omitempty"`
	Metadata      []invocation.DescriptorKV `json:"metadata,omitempty"`
}

type namedDescriptor struct {
	Verb       string                `json:"verb"`
	Descriptor invocation.Descriptor `json:"descriptor"`
}

type semanticDocument struct {
	FormatVersion string            `json:"format_version"`
	Name          string            `json:"name"`
	Version       string            `json:"version"`
	Sources       []Source          `json:"sources"`
	Environment   ir.Environment    `json:"environment"`
	Executors     []namedDescriptor `json:"executors,omitempty"`
}

// New validates and freezes a source bundle. Source paths must already be
// normalized; source line endings are normalized to LF before hashing.
func New(spec Spec) (*SourceBundle, error) {
	name := strings.TrimSpace(spec.Name)
	version := strings.TrimSpace(spec.Version)
	if name == "" || name != spec.Name {
		return nil, fmt.Errorf("source bundle name is required and must be normalized")
	}
	if version == "" || version != spec.Version {
		return nil, fmt.Errorf("source bundle version is required and must be normalized")
	}
	if _, err := ir.EnvironmentDigest(spec.Environment); err != nil {
		return nil, fmt.Errorf("source bundle declarations: %w", err)
	}

	sources := make([]Source, len(spec.Sources))
	seenPaths := make(map[string]struct{}, len(spec.Sources))
	for index, source := range spec.Sources {
		normalizedPath, err := validateSourcePath(source.Path)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seenPaths[normalizedPath]; duplicate {
			return nil, fmt.Errorf("source bundle repeats source path %q", normalizedPath)
		}
		seenPaths[normalizedPath] = struct{}{}
		content := strings.ReplaceAll(strings.ReplaceAll(source.Content, "\r\n", "\n"), "\r", "\n")
		if !utf8.ValidString(content) {
			return nil, fmt.Errorf("source bundle source %q is not UTF-8", normalizedPath)
		}
		sources[index] = Source{Path: normalizedPath, Content: content}
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Path < sources[j].Path })

	executorNames := make([]string, 0, len(spec.Executors))
	for verbName, descriptor := range spec.Executors {
		if verbName == "" || verbName != strings.TrimSpace(verbName) {
			return nil, fmt.Errorf("source bundle executor verb %q is not normalized", verbName)
		}
		if _, declared := spec.Environment.Verbs[verbName]; !declared {
			return nil, fmt.Errorf("source bundle executor %q has no verb contract", verbName)
		}
		if _, err := descriptor.CanonicalJSON(); err != nil {
			return nil, fmt.Errorf("source bundle executor %q: %w", verbName, err)
		}
		executorNames = append(executorNames, verbName)
	}
	sort.Strings(executorNames)
	executors := make([]namedDescriptor, 0, len(executorNames))
	for _, verbName := range executorNames {
		executors = append(executors, namedDescriptor{Verb: verbName, Descriptor: spec.Executors[verbName]})
	}

	metadata, err := canonicalMetadata(spec.Metadata)
	if err != nil {
		return nil, err
	}
	return &SourceBundle{document: sourceDocument{
		FormatVersion: FormatVersion, Name: name, Version: version, Sources: sources,
		Environment: cloneEnvironment(spec.Environment), Executors: executors, Metadata: metadata,
	}}, nil
}

func validateSourcePath(sourcePath string) (string, error) {
	if sourcePath == "" || sourcePath != strings.TrimSpace(sourcePath) || strings.Contains(sourcePath, "\\") || strings.HasPrefix(sourcePath, "/") {
		return "", fmt.Errorf("source bundle path %q is not a normalized relative path", sourcePath)
	}
	normalized := path.Clean(sourcePath)
	if normalized != sourcePath || normalized == "." || normalized == ".." || strings.HasPrefix(normalized, "../") {
		return "", fmt.Errorf("source bundle path %q is not a normalized relative path", sourcePath)
	}
	extension := path.Ext(normalized)
	if extension != ".eff" && extension != ".effx" {
		return "", fmt.Errorf("source bundle path %q must end in .eff or .effx", sourcePath)
	}
	return normalized, nil
}

func canonicalMetadata(values map[string]string) ([]invocation.DescriptorKV, error) {
	names := make([]string, 0, len(values))
	for name := range values {
		if name == "" || name != strings.TrimSpace(name) {
			return nil, fmt.Errorf("source bundle metadata key %q is not normalized", name)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]invocation.DescriptorKV, 0, len(names))
	for _, name := range names {
		result = append(result, invocation.DescriptorKV{Name: name, Value: values[name]})
	}
	return result, nil
}

// Parse strictly decodes a source bundle and rejects unknown or duplicate JSON fields.
func Parse(data []byte) (*SourceBundle, error) {
	if err := rejectDuplicateNames(data); err != nil {
		return nil, fmt.Errorf("decode source bundle: %w", err)
	}
	var document sourceDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode source bundle: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("decode source bundle: trailing JSON value")
		}
		return nil, fmt.Errorf("decode source bundle: %w", err)
	}
	if document.FormatVersion != FormatVersion {
		return nil, fmt.Errorf("unsupported source bundle format %q", document.FormatVersion)
	}
	executors := make(map[string]invocation.Descriptor, len(document.Executors))
	for _, item := range document.Executors {
		if _, duplicate := executors[item.Verb]; duplicate {
			return nil, fmt.Errorf("source bundle repeats executor %q", item.Verb)
		}
		executors[item.Verb] = item.Descriptor
	}
	metadata := make(map[string]string, len(document.Metadata))
	for _, item := range document.Metadata {
		if _, duplicate := metadata[item.Name]; duplicate {
			return nil, fmt.Errorf("source bundle repeats metadata %q", item.Name)
		}
		metadata[item.Name] = item.Value
	}
	return New(Spec{
		Name: document.Name, Version: document.Version, Sources: document.Sources,
		Environment: document.Environment, Executors: executors, Metadata: metadata,
	})
}

// Bytes returns the canonical portable file encoding.
func (bundle *SourceBundle) Bytes() ([]byte, error) {
	if bundle == nil {
		return nil, fmt.Errorf("source bundle is nil")
	}
	return json.Marshal(bundle.document)
}

// Digest returns the semantic digest. Optional metadata is deliberately excluded.
func (bundle *SourceBundle) Digest() (string, error) {
	if bundle == nil {
		return "", fmt.Errorf("source bundle is nil")
	}
	data, err := json.Marshal(semanticDocument{
		FormatVersion: bundle.document.FormatVersion, Name: bundle.document.Name,
		Version: bundle.document.Version, Sources: bundle.document.Sources,
		Environment: bundle.document.Environment, Executors: bundle.document.Executors,
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

// OCIBytes returns a deterministic tar layer containing the canonical bundle.
func (bundle *SourceBundle) OCIBytes() ([]byte, error) {
	data, err := bundle.Bytes()
	if err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	header := &tar.Header{Name: ociManifestPath, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg, Format: tar.FormatPAX}
	if err := writer.WriteHeader(header); err != nil {
		return nil, err
	}
	if _, err := writer.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// ParseOCI decodes the deterministic OCI tar layer and rejects all unexpected entries.
func ParseOCI(data []byte) (*SourceBundle, error) {
	reader := tar.NewReader(bytes.NewReader(data))
	var manifest []byte
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decode source bundle OCI layer: %w", err)
		}
		if header.Name != ociManifestPath || header.Typeflag != tar.TypeReg || manifest != nil {
			return nil, fmt.Errorf("decode source bundle OCI layer: unexpected entry %q", header.Name)
		}
		manifest, err = io.ReadAll(io.LimitReader(reader, header.Size+1))
		if err != nil || int64(len(manifest)) != header.Size {
			return nil, fmt.Errorf("decode source bundle OCI layer: invalid manifest")
		}
	}
	if manifest == nil {
		return nil, fmt.Errorf("decode source bundle OCI layer: manifest is missing")
	}
	return Parse(manifest)
}

// Name returns the bundle name.
func (bundle *SourceBundle) Name() string {
	if bundle == nil {
		return ""
	}
	return bundle.document.Name
}

// Version returns the bundle version.
func (bundle *SourceBundle) Version() string {
	if bundle == nil {
		return ""
	}
	return bundle.document.Version
}

// Sources returns a copy of normalized source files.
func (bundle *SourceBundle) Sources() []Source {
	if bundle == nil {
		return nil
	}
	return append([]Source(nil), bundle.document.Sources...)
}

// Environment returns a deep copy of checked declarations.
func (bundle *SourceBundle) Environment() ir.Environment {
	if bundle == nil {
		return ir.Environment{}
	}
	return cloneEnvironment(bundle.document.Environment)
}

// Executors returns a copy of canonical descriptors keyed by verb.
func (bundle *SourceBundle) Executors() map[string]invocation.Descriptor {
	result := make(map[string]invocation.Descriptor)
	if bundle != nil {
		for _, item := range bundle.document.Executors {
			result[item.Verb] = item.Descriptor
		}
	}
	return result
}

// Metadata returns a copy of non-semantic metadata.
func (bundle *SourceBundle) Metadata() map[string]string {
	result := make(map[string]string)
	if bundle != nil {
		for _, item := range bundle.document.Metadata {
			result[item.Name] = item.Value
		}
	}
	return result
}

func cloneEnvironment(environment ir.Environment) ir.Environment {
	data, _ := json.Marshal(environment)
	var clone ir.Environment
	_ = json.Unmarshal(data, &clone)
	return clone
}

func rejectDuplicateNames(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := scanValue(decoder); err != nil {
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

func scanValue(decoder *json.Decoder) error {
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
			if err := scanValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := scanValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
}
