package loader

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/effectus/effectus-go/internal/safetar"
	"github.com/effectus/effectus-go/invocation"
	"github.com/effectus/effectus-go/schema/verb"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/segmentio/kafka-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/structpb"
)

// ExtensionManager provides a unified way to extend Effectus with verbs and schemas
type ExtensionManager struct {
	mu      sync.RWMutex
	loaders []Loader
}

// NewExtensionManager creates a new extension manager
func NewExtensionManager() *ExtensionManager {
	return &ExtensionManager{
		loaders: make([]Loader, 0),
	}
}

// LoadExtensions loads all registered extensions into the provided registries
func (em *ExtensionManager) LoadExtensions(ctx context.Context, target LoadTarget) error {
	if em == nil || target == nil {
		return fmt.Errorf("extension manager and load target are required")
	}
	em.mu.RLock()
	loaders := append([]Loader(nil), em.loaders...)
	em.mu.RUnlock()
	for _, loader := range loaders {
		if err := ctx.Err(); err != nil {
			return err
		}
		if loader == nil {
			return fmt.Errorf("extension loader is nil")
		}
		if err := loader.Load(ctx, target); err != nil {
			return fmt.Errorf("failed to load extension %s: %w", loader.Name(), err)
		}
	}
	return nil
}

// AddLoader registers a loader for static or dynamic extensions
func (em *ExtensionManager) AddLoader(loader Loader) {
	em.mu.Lock()
	defer em.mu.Unlock()
	em.loaders = append(em.loaders, loader)
}

// GetLoaders returns a copy of the registered loader list.
func (em *ExtensionManager) GetLoaders() []Loader {
	em.mu.RLock()
	defer em.mu.RUnlock()
	return append([]Loader(nil), em.loaders...)
}

// === Core Interfaces ===

// LoadTarget defines what can be loaded into
type LoadTarget interface {
	RegisterVerb(spec VerbSpec, executor VerbExecutor) error
	RegisterFunction(name string, fn interface{}) error
	LoadData(path string, value interface{}) error
	RegisterType(name string, typeDef TypeDefinition) error
}

// SourceLoadTarget receives checked-compiler source files from extensions.
type SourceLoadTarget interface {
	RegisterSource(SourceFile) error
}

// SourceFile is one immutable .eff or .effx compiler input.
type SourceFile struct {
	Path string
	Data []byte
}

// Loader defines the interface for extension loaders
type Loader interface {
	Name() string
	Load(ctx context.Context, target LoadTarget) error
}

// VerbSpec defines a verb specification interface
type VerbSpec interface {
	GetName() string
	GetDescription() string
	GetCapabilities() []string
	GetResources() []ResourceSpec
	GetArgTypes() map[string]string
	GetRequiredArgs() []string
	GetReturnType() string
	GetInverseVerb() string
}

// ResourceSpec defines resource requirements
type ResourceSpec interface {
	GetResource() string
	GetCapabilities() []string
}

// VerbExecutor is an alias for the core verb executor interface.
type VerbExecutor = verb.Executor

// TypeDefinition defines a type for the schema system
type TypeDefinition struct {
	Name        string      `json:"name"`
	Type        string      `json:"type"` // "object", "array", "string", etc.
	Properties  interface{} `json:"properties,omitempty"`
	Required    []string    `json:"required,omitempty"`
	Description string      `json:"description,omitempty"`
}

const maxExtensionManifestBytes = 4 << 20

func readBoundedManifest(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxExtensionManifestBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxExtensionManifestBytes {
		return nil, fmt.Errorf("manifest exceeds %d bytes", maxExtensionManifestBytes)
	}
	return data, nil
}

func decodeStrictJSON(data []byte, target interface{}) error {
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := scanJSONValue(decoder); err != nil {
		return err
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
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
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("JSON object key is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate JSON object key %q", key)
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim('}') {
			return fmt.Errorf("invalid JSON object terminator")
		}
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim(']') {
			return fmt.Errorf("invalid JSON array terminator")
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
	return nil
}

// === Static Loaders ===

// StaticVerbLoader loads verbs from code (compile-time registration)
type StaticVerbLoader struct {
	name  string
	verbs []VerbDefinition
}

// VerbDefinition defines a verb that can be registered
type VerbDefinition struct {
	Spec     VerbSpec
	Executor VerbExecutor
}

// NewStaticVerbLoader creates a static verb loader
func NewStaticVerbLoader(name string, verbs []VerbDefinition) *StaticVerbLoader {
	return &StaticVerbLoader{
		name:  name,
		verbs: verbs,
	}
}

func (svl *StaticVerbLoader) Name() string {
	return fmt.Sprintf("StaticVerbs:%s", svl.name)
}

func (svl *StaticVerbLoader) Load(ctx context.Context, target LoadTarget) error {
	for _, verbDef := range svl.verbs {
		if err := target.RegisterVerb(verbDef.Spec, verbDef.Executor); err != nil {
			return fmt.Errorf("registering verb %s: %w", verbDef.Spec.GetName(), err)
		}
	}
	return nil
}

// StaticSourceLoader loads one in-memory .eff or .effx source file.
type StaticSourceLoader struct {
	name   string
	source SourceFile
}

func NewStaticSourceLoader(name, path string, data []byte) *StaticSourceLoader {
	return &StaticSourceLoader{name: name, source: SourceFile{Path: path, Data: append([]byte(nil), data...)}}
}

func (loader *StaticSourceLoader) Name() string {
	return fmt.Sprintf("StaticSource:%s", loader.name)
}

func (loader *StaticSourceLoader) Load(_ context.Context, target LoadTarget) error {
	sourceTarget, ok := target.(SourceLoadTarget)
	if !ok {
		return fmt.Errorf("target does not support extension sources")
	}
	return sourceTarget.RegisterSource(SourceFile{Path: loader.source.Path, Data: append([]byte(nil), loader.source.Data...)})
}

// StaticSchemaLoader loads schemas and functions from code
type StaticSchemaLoader struct {
	name      string
	functions map[string]interface{}
	data      map[string]interface{}
	types     map[string]TypeDefinition
}

// NewStaticSchemaLoader creates a static schema loader
func NewStaticSchemaLoader(name string) *StaticSchemaLoader {
	return &StaticSchemaLoader{
		name:      name,
		functions: make(map[string]interface{}),
		data:      make(map[string]interface{}),
		types:     make(map[string]TypeDefinition),
	}
}

// AddFunction registers a function for expressions
func (ssl *StaticSchemaLoader) AddFunction(name string, fn interface{}) *StaticSchemaLoader {
	ssl.functions[name] = fn
	return ssl
}

// AddData registers data for fact access
func (ssl *StaticSchemaLoader) AddData(path string, value interface{}) *StaticSchemaLoader {
	ssl.data[path] = value
	return ssl
}

// AddType registers a type definition
func (ssl *StaticSchemaLoader) AddType(name string, typeDef TypeDefinition) *StaticSchemaLoader {
	ssl.types[name] = typeDef
	return ssl
}

func (ssl *StaticSchemaLoader) Name() string {
	return fmt.Sprintf("StaticSchema:%s", ssl.name)
}

func (ssl *StaticSchemaLoader) Load(ctx context.Context, target LoadTarget) error {
	// Load functions
	for name, fn := range ssl.functions {
		if err := target.RegisterFunction(name, fn); err != nil {
			return fmt.Errorf("registering function %s: %w", name, err)
		}
	}

	// Load data
	for path, value := range ssl.data {
		if err := target.LoadData(path, value); err != nil {
			return fmt.Errorf("loading data %s: %w", path, err)
		}
	}

	// Load types
	for name, typeDef := range ssl.types {
		if err := target.RegisterType(name, typeDef); err != nil {
			return fmt.Errorf("registering type %s: %w", name, err)
		}
	}

	return nil
}

// === Dynamic Loaders ===

// JSONVerbLoader loads verbs from JSON files
type JSONVerbLoader struct {
	name     string
	filePath string
}

// VerbManifest defines the structure for dynamic verb loading
type VerbManifest struct {
	Name        string                 `json:"name"`
	Version     string                 `json:"version"`
	Description string                 `json:"description"`
	Verbs       []JSONVerbSpec         `json:"verbs"`
	Executors   map[string]interface{} `json:"executors,omitempty"`
}

// VerbTarget defines how a verb should be executed.
type VerbTarget struct {
	Type   string                 `json:"type"`
	Ref    string                 `json:"ref,omitempty"`
	Config map[string]interface{} `json:"config,omitempty"`
}

// JSONVerbSpec defines a verb specification in JSON
type JSONVerbSpec struct {
	Name         string             `json:"name"`
	Description  string             `json:"description"`
	Capabilities []string           `json:"capabilities"`
	Resources    []JSONResourceSpec `json:"resources"`
	ArgTypes     map[string]string  `json:"argTypes"`
	RequiredArgs []string           `json:"requiredArgs"`
	ReturnType   string             `json:"returnType"`
	InverseVerb  string             `json:"inverseVerb,omitempty"`
	Target       *VerbTarget        `json:"target,omitempty"`
}

func (jvs *JSONVerbSpec) GetName() string           { return jvs.Name }
func (jvs *JSONVerbSpec) GetDescription() string    { return jvs.Description }
func (jvs *JSONVerbSpec) GetCapabilities() []string { return jvs.Capabilities }
func (jvs *JSONVerbSpec) GetResources() []ResourceSpec {
	specs := make([]ResourceSpec, len(jvs.Resources))
	for i, r := range jvs.Resources {
		specs[i] = &r
	}
	return specs
}
func (jvs *JSONVerbSpec) GetArgTypes() map[string]string { return jvs.ArgTypes }
func (jvs *JSONVerbSpec) GetRequiredArgs() []string      { return jvs.RequiredArgs }
func (jvs *JSONVerbSpec) GetReturnType() string          { return jvs.ReturnType }
func (jvs *JSONVerbSpec) GetInverseVerb() string         { return jvs.InverseVerb }

// JSONResourceSpec defines resource requirements in JSON
type JSONResourceSpec struct {
	Resource     string   `json:"resource"`
	Capabilities []string `json:"capabilities"`
}

func (jrs *JSONResourceSpec) GetResource() string       { return jrs.Resource }
func (jrs *JSONResourceSpec) GetCapabilities() []string { return jrs.Capabilities }

// NewJSONVerbLoader creates a JSON verb loader from file
func NewJSONVerbLoader(name, filePath string) *JSONVerbLoader {
	return &JSONVerbLoader{
		name:     name,
		filePath: filePath,
	}
}

func (jvl *JSONVerbLoader) Name() string {
	return fmt.Sprintf("JSONVerbs:%s", jvl.name)
}

func (jvl *JSONVerbLoader) Load(ctx context.Context, target LoadTarget) error {
	data, err := readBoundedManifest(jvl.filePath)
	if err != nil {
		return fmt.Errorf("reading verb manifest: %w", err)
	}

	var manifest VerbManifest
	if err := decodeStrictJSON(data, &manifest); err != nil {
		return fmt.Errorf("parsing verb manifest: %w", err)
	}

	for _, verbSpec := range manifest.Verbs {
		targetType, targetConfig := resolveVerbTarget(&verbSpec)
		executor, err := jvl.createExecutor(targetType, targetConfig, verbSpec.Name)
		if err != nil {
			return fmt.Errorf("creating executor for %s: %w", verbSpec.Name, err)
		}

		if err := target.RegisterVerb(&verbSpec, executor); err != nil {
			return fmt.Errorf("registering verb %s: %w", verbSpec.Name, err)
		}
	}

	return nil
}

func (jvl *JSONVerbLoader) createExecutor(targetType string, config map[string]interface{}, verbName string) (VerbExecutor, error) {
	switch strings.ToLower(strings.TrimSpace(targetType)) {
	case "mock":
		return &MockExecutor{Name: fmt.Sprintf("Mock:%s", jvl.name)}, nil
	case "noop":
		return &NoOpExecutor{}, nil
	case "http":
		return NewHTTPExecutor(config)
	case "grpc":
		return NewGRPCExecutor(config)
	case "stream", "message":
		return NewStreamExecutor(config)
	case "local":
		return nil, fmt.Errorf("local target requires in-process executor; use static loader for %s", verbName)
	case "oci":
		return NewOCIExecutor(verbName, config)
	default:
		return nil, fmt.Errorf("unsupported executor target: %s", targetType)
	}
}

func resolveVerbTarget(spec *JSONVerbSpec) (string, map[string]interface{}) {
	if spec == nil || spec.Target == nil {
		return "stream", map[string]interface{}{"publisher": "stdout"}
	}

	targetType := strings.TrimSpace(spec.Target.Type)
	config := map[string]interface{}{}
	for key, value := range spec.Target.Config {
		config[key] = value
	}
	if spec.Target.Ref != "" {
		config["ref"] = spec.Target.Ref
	}

	if targetType == "" {
		targetType = "stream"
	}
	if targetType == "stream" {
		if _, ok := config["publisher"]; !ok && len(config) == 0 {
			config["publisher"] = "stdout"
		}
	}

	return targetType, config
}

// JSONSchemaLoader loads schemas from JSON Schema files
type JSONSchemaLoader struct {
	name     string
	filePath string
}

// SchemaManifest defines the structure for dynamic schema loading
type SchemaManifest struct {
	Name        string                    `json:"name"`
	Version     string                    `json:"version"`
	Description string                    `json:"description"`
	Types       map[string]TypeDefinition `json:"types"`
	Functions   map[string]FunctionDef    `json:"functions"`
	InitialData map[string]interface{}    `json:"initialData,omitempty"`
}

// FunctionDef defines a function for dynamic loading
type FunctionDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Type        string                 `json:"type"` // "builtin", "expression", etc.
	Config      map[string]interface{} `json:"config,omitempty"`
}

// NewJSONSchemaLoader creates a JSON schema loader from file
func NewJSONSchemaLoader(name, filePath string) *JSONSchemaLoader {
	return &JSONSchemaLoader{
		name:     name,
		filePath: filePath,
	}
}

func (jsl *JSONSchemaLoader) Name() string {
	return fmt.Sprintf("JSONSchema:%s", jsl.name)
}

func (jsl *JSONSchemaLoader) Load(ctx context.Context, target LoadTarget) error {
	data, err := readBoundedManifest(jsl.filePath)
	if err != nil {
		return fmt.Errorf("reading schema manifest: %w", err)
	}

	var manifest SchemaManifest
	if err := decodeStrictJSON(data, &manifest); err != nil {
		return fmt.Errorf("parsing schema manifest: %w", err)
	}

	// Load initial data
	for path, value := range manifest.InitialData {
		if err := target.LoadData(path, value); err != nil {
			return fmt.Errorf("loading initial data %s: %w", path, err)
		}
	}

	// Load functions
	for _, funcDef := range manifest.Functions {
		if err := jsl.loadFunction(funcDef, target); err != nil {
			return fmt.Errorf("loading function %s: %w", funcDef.Name, err)
		}
	}

	// Load types
	for name, typeDef := range manifest.Types {
		if err := target.RegisterType(name, typeDef); err != nil {
			return fmt.Errorf("registering type %s: %w", name, err)
		}
	}

	return nil
}

func (jsl *JSONSchemaLoader) loadFunction(funcDef FunctionDef, target LoadTarget) error {
	switch funcDef.Type {
	case "builtin":
		return jsl.loadBuiltinFunction(funcDef, target)
	default:
		return fmt.Errorf("unsupported function type: %s", funcDef.Type)
	}
}

func (jsl *JSONSchemaLoader) loadBuiltinFunction(funcDef FunctionDef, target LoadTarget) error {
	switch funcDef.Name {
	case "length":
		return target.RegisterFunction("length", func(s string) int { return len(s) })
	case "upper":
		return target.RegisterFunction("upper", strings.ToUpper)
	case "lower":
		return target.RegisterFunction("lower", strings.ToLower)
	default:
		return fmt.Errorf("unknown builtin function: %s", funcDef.Name)
	}
}

// === Protocol Buffer Loaders ===

// ProtoVerbLoader loads verbs from Protocol Buffer messages
type ProtoVerbLoader struct {
	name    string
	message proto.Message
}

// NewProtoVerbLoader creates a protobuf verb loader
func NewProtoVerbLoader(name string, message proto.Message) *ProtoVerbLoader {
	return &ProtoVerbLoader{
		name:    name,
		message: message,
	}
}

func (pvl *ProtoVerbLoader) Name() string {
	return fmt.Sprintf("ProtoVerbs:%s", pvl.name)
}

func (pvl *ProtoVerbLoader) Load(ctx context.Context, target LoadTarget) error {
	// Convert protobuf to JSON and delegate to JSON loader
	marshaler := protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: false,
	}

	data, err := marshaler.Marshal(pvl.message)
	if err != nil {
		return fmt.Errorf("marshaling proto message: %w", err)
	}

	// Create temporary JSON loader
	tempFile, err := os.CreateTemp("", "verbs-*.json")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	if _, err := tempFile.Write(data); err != nil {
		return fmt.Errorf("writing temp file: %w", err)
	}

	jsonLoader := NewJSONVerbLoader(pvl.name, tempFile.Name())
	return jsonLoader.Load(ctx, target)
}

// ProtoSchemaLoader loads schemas from Protocol Buffer messages
type ProtoSchemaLoader struct {
	name    string
	message proto.Message
}

// NewProtoSchemaLoader creates a protobuf schema loader
func NewProtoSchemaLoader(name string, message proto.Message) *ProtoSchemaLoader {
	return &ProtoSchemaLoader{
		name:    name,
		message: message,
	}
}

func (psl *ProtoSchemaLoader) Name() string {
	return fmt.Sprintf("ProtoSchema:%s", psl.name)
}

func (psl *ProtoSchemaLoader) Load(ctx context.Context, target LoadTarget) error {
	// Convert protobuf to JSON and delegate to JSON loader
	marshaler := protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: false,
	}

	data, err := marshaler.Marshal(psl.message)
	if err != nil {
		return fmt.Errorf("marshaling proto message: %w", err)
	}

	// Create temporary JSON loader
	tempFile, err := os.CreateTemp("", "schema-*.json")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	if _, err := tempFile.Write(data); err != nil {
		return fmt.Errorf("writing temp file: %w", err)
	}

	jsonLoader := NewJSONSchemaLoader(psl.name, tempFile.Name())
	return jsonLoader.Load(ctx, target)
}

// === OCI Bundle Loader ===

// OCIBundleLoader loads extensions from OCI registry bundles
type OCISignatureVerifier interface {
	Verify(context.Context, string, string) error
}
type OCIVerificationPolicy struct {
	RequireSignature bool
	Verifier         OCISignatureVerifier
}

type OCIBundleLoader struct {
	name   string
	ref    string
	policy OCIVerificationPolicy
}

// NewOCIBundleLoader creates an OCI bundle loader
func NewOCIBundleLoader(name, ref string) *OCIBundleLoader {
	return NewOCIBundleLoaderWithPolicy(name, ref, OCIVerificationPolicy{})
}
func NewOCIBundleLoaderWithPolicy(name, ref string, policy OCIVerificationPolicy) *OCIBundleLoader {
	return &OCIBundleLoader{name: name, ref: ref, policy: policy}
}

func (obl *OCIBundleLoader) Name() string {
	return fmt.Sprintf("OCIBundle:%s", obl.name)
}

func (obl *OCIBundleLoader) Load(ctx context.Context, target LoadTarget) error {
	loaders, cleanup, err := loadOCIBundleLoaders(ctx, obl.ref, obl.policy)
	if err != nil {
		return err
	}
	defer cleanup()

	for _, loader := range loaders {
		if err := loader.Load(ctx, target); err != nil {
			return err
		}
	}
	return nil
}

func loadOCIBundleLoaders(ctx context.Context, ref string, policy OCIVerificationPolicy) ([]Loader, func(), error) {
	if strings.TrimSpace(ref) == "" {
		return nil, func() {}, fmt.Errorf("oci ref is required")
	}
	dir, err := os.MkdirTemp("", "effectus-oci-*")
	if err != nil {
		return nil, func() {}, err
	}
	cleanup := func() {
		_ = os.RemoveAll(dir)
	}

	if err := pullOCIExtensionBundle(ctx, ref, dir, policy); err != nil {
		cleanup()
		return nil, func() {}, err
	}

	loaders, err := LoadFromDirectory(dir)
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	return loaders, cleanup, nil
}

func pullOCIExtensionBundle(ctx context.Context, ref string, outputDir string, policy OCIVerificationPolicy) error {
	parsed, err := name.ParseReference(ref)
	if err != nil {
		return fmt.Errorf("parsing oci ref: %w", err)
	}
	if _, pinned := parsed.(name.Digest); !pinned {
		return fmt.Errorf("OCI extension reference must be pinned by digest")
	}

	image, err := remote.Image(parsed, remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return fmt.Errorf("pulling image: %w", err)
	}
	actualDigest, err := image.Digest()
	if err != nil {
		return fmt.Errorf("read OCI image digest: %w", err)
	}
	if err := verifyOCIIdentity(ctx, parsed.Name(), parsed.Identifier(), actualDigest.String(), policy); err != nil {
		return err
	}

	layers, err := image.Layers()
	if err != nil {
		return fmt.Errorf("getting layers: %w", err)
	}
	if len(layers) == 0 {
		return fmt.Errorf("image has no layers")
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}

	// Extract content layers. The unified bundle format uses a final JSON
	// metadata layer, while ORAS directory artifacts can contain one tar layer.
	for i, layer := range layers {
		rc, err := layer.Uncompressed()
		if err != nil {
			return fmt.Errorf("getting layer %d: %w", i, err)
		}
		if i == len(layers)-1 {
			payload, readErr := io.ReadAll(io.LimitReader(rc, (64<<20)+1))
			rc.Close()
			if readErr != nil {
				return fmt.Errorf("reading final OCI layer: %w", readErr)
			}
			if len(payload) > 64<<20 {
				return fmt.Errorf("final OCI extension layer exceeds %d bytes", 64<<20)
			}
			if json.Valid(payload) {
				continue
			}
			if err := extractTarLayer(bytes.NewReader(payload), outputDir); err != nil {
				return fmt.Errorf("extracting final layer %d: %w", i, err)
			}
			continue
		}
		if err := extractTarLayer(rc, outputDir); err != nil {
			rc.Close()
			return fmt.Errorf("extracting layer %d: %w", i, err)
		}
		rc.Close()
	}
	return nil
}

func verifyOCIIdentity(ctx context.Context, reference, expectedDigest, actualDigest string, policy OCIVerificationPolicy) error {
	if actualDigest != expectedDigest {
		return fmt.Errorf("OCI image digest mismatch: expected %s, got %s", expectedDigest, actualDigest)
	}
	if policy.RequireSignature && policy.Verifier == nil {
		return fmt.Errorf("OCI signature verification is required but no verifier is configured")
	}
	if policy.Verifier != nil {
		if err := policy.Verifier.Verify(ctx, reference, actualDigest); err != nil {
			return fmt.Errorf("verify OCI signature: %w", err)
		}
	}
	return nil
}

type captureTarget struct {
	verbs map[string]VerbExecutor
}

func newCaptureTarget() *captureTarget {
	return &captureTarget{verbs: make(map[string]VerbExecutor)}
}

func (ct *captureTarget) RegisterVerb(spec VerbSpec, executor VerbExecutor) error {
	if spec == nil || executor == nil {
		return nil
	}
	ct.verbs[spec.GetName()] = executor
	return nil
}

func (ct *captureTarget) RegisterFunction(name string, fn interface{}) error {
	return nil
}

func (ct *captureTarget) LoadData(path string, value interface{}) error {
	return nil
}

func (ct *captureTarget) RegisterType(name string, typeDef TypeDefinition) error {
	return nil
}

// === Utility Executors ===

// MockExecutor provides a simple mock executor for testing
type MockExecutor struct {
	Name string
}

func (me *MockExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return map[string]interface{}{
		"executor": me.Name,
		"args":     args,
		"result":   "mock_success",
	}, nil
}

func (me *MockExecutor) SourceInfo() verb.SourceInfo {
	return verb.SourceInfo{Type: verb.SourceMock, Detail: me.Name}
}

// NoOpExecutor provides a no-operation executor
type NoOpExecutor struct{}

func (noe *NoOpExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return nil, nil
}

func (noe *NoOpExecutor) SourceInfo() verb.SourceInfo {
	return verb.SourceInfo{Type: verb.SourceNoop}
}

// HTTPExecutor executes verbs via HTTP calls
type HTTPExecutor struct {
	URL     string
	Method  string
	Headers map[string]string
	Timeout time.Duration
	Policy  OutboundNetworkPolicy
	client  *http.Client
}

func NewHTTPExecutor(config map[string]interface{}) (*HTTPExecutor, error) {
	url, ok := config["url"].(string)
	if !ok {
		return nil, fmt.Errorf("http executor requires 'url' config")
	}

	method := "POST"
	if m, ok := config["method"].(string); ok {
		method = m
	}

	headers := make(map[string]string)
	if h, ok := config["headers"].(map[string]interface{}); ok {
		for k, v := range h {
			if isReservedInvocationHeader(k) {
				return nil, fmt.Errorf("http executor header %q is reserved", k)
			}
			if str, ok := v.(string); ok {
				headers[k] = str
			}
		}
	}
	policy := OutboundNetworkPolicy{}
	if allowed, ok := config["allowPrivateNetwork"].(bool); ok {
		policy.AllowPrivate = allowed
	}
	if _, err := policy.ValidateURL(url); err != nil {
		return nil, fmt.Errorf("http executor URL: %w", err)
	}

	timeout := 5 * time.Second
	if raw, ok := config["timeout"].(string); ok {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("http executor timeout must be a positive duration")
		}
		timeout = parsed
	}

	return &HTTPExecutor{
		URL:     url,
		Method:  method,
		Headers: headers,
		Timeout: timeout,
		Policy:  policy,
		client:  policy.HTTPClient(timeout, headers),
	}, nil
}

func (he *HTTPExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return he.execute(ctx, args, nil)
}

func (he *HTTPExecutor) Invoke(ctx context.Context, request invocation.Request) invocation.Outcome {
	result, err := he.execute(ctx, request.Arguments, invocationHeaders(request))
	if err != nil {
		var classified *httpInvocationError
		if errors.As(err, &classified) {
			return invocation.Outcome{Class: classified.class, Err: classified}
		}
		return invocation.Outcome{Class: invocation.OutcomeUnknown, Err: err}
	}
	return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: result}
}

type httpInvocationError struct {
	class   invocation.OutcomeClass
	status  int
	message string
}

func (failure *httpInvocationError) Error() string {
	return fmt.Sprintf("http status %d: %s", failure.status, failure.message)
}
func classifyHTTPOutcome(value string) invocation.OutcomeClass {
	class := invocation.OutcomeClass(strings.TrimSpace(value))
	switch class {
	case invocation.OutcomeRetryableKnownNotCommitted, invocation.OutcomePermanentFailure, invocation.OutcomeUnknown, invocation.OutcomeStaleFence:
		return class
	default:
		return invocation.OutcomeUnknown
	}
}

func (he *HTTPExecutor) execute(ctx context.Context, args map[string]interface{}, systemHeaders map[string]string) (interface{}, error) {
	if he.Timeout <= 0 {
		return nil, fmt.Errorf("http executor timeout must be positive")
	}
	callContext, cancel := context.WithTimeout(ctx, he.Timeout)
	defer cancel()
	payload, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("marshal args: %w", err)
	}

	req, err := http.NewRequestWithContext(callContext, he.Method, he.URL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range he.Headers {
		req.Header.Set(key, value)
	}
	for key, value := range systemHeaders {
		req.Header.Set(key, value)
	}

	client := he.client
	if client == nil {
		return nil, fmt.Errorf("http executor client is not initialized")
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	const maxExtensionHTTPResponse = 1 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxExtensionHTTPResponse+1))
	if err != nil {
		return nil, fmt.Errorf("read HTTP response: %w", err)
	}
	if len(body) > maxExtensionHTTPResponse {
		return nil, fmt.Errorf("HTTP response exceeds %d bytes", maxExtensionHTTPResponse)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, &httpInvocationError{class: classifyHTTPOutcome(resp.Header.Get("X-Effectus-Outcome")), status: resp.StatusCode, message: strings.TrimSpace(string(body))}
	}

	if len(body) == 0 {
		return true, nil
	}

	var decoded interface{}
	if err := json.Unmarshal(body, &decoded); err == nil {
		return decoded, nil
	}

	return strings.TrimSpace(string(body)), nil
}

func (he *HTTPExecutor) Close() error {
	if he == nil || he.client == nil {
		return nil
	}
	he.client.CloseIdleConnections()
	return nil
}

func (he *HTTPExecutor) InvocationResolverDescriptor() any {
	return map[string]any{"type": "http", "url": he.URL, "method": he.Method, "headers": he.Headers, "timeout": he.Timeout.String(), "allow_private_network": he.Policy.AllowPrivate}
}

func (he *HTTPExecutor) SourceInfo() verb.SourceInfo {
	method := strings.ToUpper(strings.TrimSpace(he.Method))
	if method == "" {
		method = "POST"
	}
	return verb.SourceInfo{Type: verb.SourceHTTP, Ref: he.URL, Detail: method}
}

// GRPCExecutor executes verbs via gRPC calls.
type GRPCExecutor struct {
	Address            string
	Method             string
	Timeout            time.Duration
	Metadata           map[string]string
	UseTLS             bool
	Insecure           bool
	ServerName         string
	requestDescriptor  protoreflect.MessageDescriptor
	responseDescriptor protoreflect.MessageDescriptor
	descriptorDigest   string
	connMu             sync.Mutex
	conn               *grpc.ClientConn
}

func NewGRPCExecutor(config map[string]interface{}) (*GRPCExecutor, error) {
	address, _ := config["address"].(string)
	if address == "" {
		return nil, fmt.Errorf("grpc executor requires 'address' config")
	}
	method, _ := config["method"].(string)
	if method == "" {
		return nil, fmt.Errorf("grpc executor requires 'method' config")
	}
	timeout := 10 * time.Second
	if raw, ok := config["timeout"].(string); ok {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("grpc executor timeout must be a positive duration")
		}
		timeout = parsed
	}
	metadata := make(map[string]string)
	if raw, ok := config["metadata"].(map[string]interface{}); ok {
		for k, v := range raw {
			if s, ok := v.(string); ok {
				metadata[k] = s
			}
		}
	}
	insecureTransport, _ := config["insecure"].(bool)
	useTLS := !insecureTransport
	if raw, ok := config["useTLS"].(bool); ok {
		if !raw && !insecureTransport {
			return nil, fmt.Errorf("grpc plaintext transport requires insecure: true")
		}
		useTLS = raw
	}
	serverName, _ := config["serverName"].(string)
	requestDescriptor, responseDescriptor, descriptorDigest, err := loadGRPCMethodDescriptors(config)
	if err != nil {
		return nil, err
	}

	return &GRPCExecutor{
		Address:           address,
		Method:            method,
		Timeout:           timeout,
		Metadata:          metadata,
		UseTLS:            useTLS,
		Insecure:          insecureTransport,
		ServerName:        serverName,
		requestDescriptor: requestDescriptor, responseDescriptor: responseDescriptor, descriptorDigest: descriptorDigest,
	}, nil
}

func loadGRPCMethodDescriptors(config map[string]interface{}) (protoreflect.MessageDescriptor, protoreflect.MessageDescriptor, string, error) {
	path, _ := config["descriptorSet"].(string)
	if strings.TrimSpace(path) == "" {
		return nil, nil, "", nil
	}
	requestType, _ := config["requestType"].(string)
	responseType, _ := config["responseType"].(string)
	if requestType == "" || responseType == "" {
		return nil, nil, "", fmt.Errorf("grpc descriptorSet requires requestType and responseType")
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, "", fmt.Errorf("read grpc descriptor set: %w", err)
	}
	if len(payload) > 4<<20 {
		return nil, nil, "", fmt.Errorf("grpc descriptor set exceeds %d bytes", 4<<20)
	}
	set := new(descriptorpb.FileDescriptorSet)
	if err := proto.Unmarshal(payload, set); err != nil {
		return nil, nil, "", fmt.Errorf("decode grpc descriptor set: %w", err)
	}
	files, err := protodesc.NewFiles(set)
	if err != nil {
		return nil, nil, "", fmt.Errorf("validate grpc descriptor set: %w", err)
	}
	request, err := files.FindDescriptorByName(protoreflect.FullName(requestType))
	if err != nil {
		return nil, nil, "", fmt.Errorf("find grpc request type: %w", err)
	}
	response, err := files.FindDescriptorByName(protoreflect.FullName(responseType))
	if err != nil {
		return nil, nil, "", fmt.Errorf("find grpc response type: %w", err)
	}
	requestMessage, requestOK := request.(protoreflect.MessageDescriptor)
	responseMessage, responseOK := response.(protoreflect.MessageDescriptor)
	if !requestOK || !responseOK {
		return nil, nil, "", fmt.Errorf("grpc descriptor types must be messages")
	}
	methodPath, _ := config["method"].(string)
	parts := strings.Split(strings.TrimPrefix(methodPath, "/"), "/")
	if len(parts) != 2 {
		return nil, nil, "", fmt.Errorf("grpc method must be /package.Service/Method")
	}
	serviceValue, err := files.FindDescriptorByName(protoreflect.FullName(parts[0]))
	if err != nil {
		return nil, nil, "", fmt.Errorf("find grpc service: %w", err)
	}
	service, ok := serviceValue.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, nil, "", fmt.Errorf("grpc method service descriptor is invalid")
	}
	method := service.Methods().ByName(protoreflect.Name(parts[1]))
	if method == nil || method.IsStreamingClient() || method.IsStreamingServer() {
		return nil, nil, "", fmt.Errorf("grpc descriptor method must be unary")
	}
	if method.Input().FullName() != requestMessage.FullName() || method.Output().FullName() != responseMessage.FullName() {
		return nil, nil, "", fmt.Errorf("grpc descriptor method types do not match requestType and responseType")
	}
	digest := sha256.Sum256(payload)
	return requestMessage, responseMessage, fmt.Sprintf("sha256:%x", digest[:]), nil
}

func (ge *GRPCExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return ge.execute(ctx, args, nil)
}

func (ge *GRPCExecutor) Invoke(ctx context.Context, request invocation.Request) invocation.Outcome {
	result, err := ge.execute(ctx, request.Arguments, invocationHeaders(request))
	if err != nil {
		return invocation.Outcome{Class: invocation.OutcomeUnknown, Err: err}
	}
	return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: result}
}

func (ge *GRPCExecutor) execute(ctx context.Context, args map[string]interface{}, systemMetadata map[string]string) (interface{}, error) {
	conn, err := ge.connection(ctx)
	if err != nil {
		return nil, err
	}

	var req proto.Message
	if ge.requestDescriptor != nil {
		message := dynamicpb.NewMessage(ge.requestDescriptor)
		payload, marshalErr := json.Marshal(args)
		if marshalErr != nil {
			return nil, fmt.Errorf("grpc encode args: %w", marshalErr)
		}
		if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(payload, message); err != nil {
			return nil, fmt.Errorf("grpc typed request: %w", err)
		}
		req = message
	} else {
		message, err := structpb.NewStruct(args)
		if err != nil {
			return nil, fmt.Errorf("grpc encode args: %w", err)
		}
		req = message
	}

	callCtx := ctx
	if ge.Timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, ge.Timeout)
		defer cancel()
	}

	if len(ge.Metadata) > 0 || len(systemMetadata) > 0 {
		values := make(map[string]string, len(ge.Metadata)+len(systemMetadata))
		for key, value := range ge.Metadata {
			values[key] = value
		}
		for key, value := range systemMetadata {
			values[strings.ToLower(key)] = value
		}
		callCtx = metadata.NewOutgoingContext(callCtx, metadataFromMap(values))
	}

	var resp proto.Message
	if ge.responseDescriptor != nil {
		resp = dynamicpb.NewMessage(ge.responseDescriptor)
	} else {
		resp = new(structpb.Struct)
	}
	if err := grpc.Invoke(callCtx, ge.Method, req, resp, conn); err != nil {
		return nil, err
	}
	if value, ok := resp.(*structpb.Struct); ok {
		return value.AsMap(), nil
	}
	payload, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("grpc typed response: %w", err)
	}
	var result map[string]any
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("grpc decode response: %w", err)
	}
	return result, nil
}

func (ge *GRPCExecutor) connection(ctx context.Context) (*grpc.ClientConn, error) {
	ge.connMu.Lock()
	defer ge.connMu.Unlock()
	if ge.conn != nil && ge.conn.GetState() == connectivity.Shutdown {
		_ = ge.conn.Close()
		ge.conn = nil
	}
	if ge.conn != nil {
		return ge.conn, nil
	}
	var transport credentials.TransportCredentials
	if ge.UseTLS {
		serverName := ge.ServerName
		if serverName == "" {
			serverName, _, _ = net.SplitHostPort(ge.Address)
		}
		transport = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName})
	} else if ge.Insecure {
		transport = insecure.NewCredentials()
	} else {
		return nil, fmt.Errorf("grpc transport security is not configured")
	}
	dialCtx := ctx
	if ge.Timeout > 0 {
		var cancel context.CancelFunc
		dialCtx, cancel = context.WithTimeout(ctx, ge.Timeout)
		defer cancel()
	}
	conn, err := grpc.DialContext(dialCtx, ge.Address, grpc.WithTransportCredentials(transport), grpc.WithBlock())
	if err != nil {
		return nil, fmt.Errorf("grpc dial: %w", err)
	}
	ge.conn = conn
	return conn, nil
}

func (ge *GRPCExecutor) Close() error {
	ge.connMu.Lock()
	defer ge.connMu.Unlock()
	if ge.conn == nil {
		return nil
	}
	err := ge.conn.Close()
	ge.conn = nil
	return err
}

func (ge *GRPCExecutor) InvocationResolverDescriptor() any {
	return map[string]any{"type": "grpc", "address": ge.Address, "method": ge.Method, "metadata": ge.Metadata, "timeout": ge.Timeout.String(), "tls": ge.UseTLS, "insecure": ge.Insecure, "server_name": ge.ServerName, "descriptor_digest": ge.descriptorDigest}
}

func (ge *GRPCExecutor) SourceInfo() verb.SourceInfo {
	method := strings.TrimSpace(ge.Method)
	return verb.SourceInfo{Type: verb.SourceGRPC, Ref: ge.Address, Detail: method}
}

func isReservedInvocationHeader(name string) bool {
	normalized := http.CanonicalHeaderKey(strings.TrimSpace(name))
	return normalized == "Idempotency-Key" || normalized == "X-Effectus-Outcome" || strings.HasPrefix(normalized, "X-Effectus-")
}

func invocationHeaders(request invocation.Request) map[string]string {
	headers := map[string]string{
		"X-Effectus-Request-ID":      request.Metadata.RequestID,
		"X-Effectus-Execution-ID":    request.Metadata.ExecutionID,
		"X-Effectus-Saga-ID":         request.Metadata.Saga.SagaID,
		"X-Effectus-Effect-ID":       request.Metadata.Saga.EffectID,
		"X-Effectus-Attempt":         strconv.FormatUint(request.Metadata.Saga.Attempt, 10),
		"X-Effectus-Direction":       string(request.Metadata.Saga.Direction),
		"Idempotency-Key":            request.Metadata.Saga.IdempotencyKey,
		"X-Effectus-Idempotency-Key": request.Metadata.Saga.IdempotencyKey,
		"X-Effectus-Argument-Hash":   request.ArgumentHash,
		"X-Effectus-Contract-Hash":   request.ContractHash,
		"X-Effectus-Deadline":        request.Metadata.Deadline.UTC().Format(time.RFC3339Nano),
	}
	if grants, err := json.Marshal(request.Metadata.FencingGrants); err == nil {
		headers["X-Effectus-Fencing-Grants"] = string(grants)
	}
	return headers
}

func metadataFromMap(values map[string]string) metadata.MD {
	md := metadata.MD{}
	for key, value := range values {
		md[key] = []string{value}
	}
	return md
}

// StreamExecutor emits verbs to a stream publisher.
type StreamExecutor struct {
	publisher  streamPublisher
	source     verb.SourceInfo
	descriptor map[string]any
}

type streamPublisher interface {
	Publish(ctx context.Context, payload []byte) error
}

type invocationStreamPublisher interface {
	PublishInvocation(context.Context, []byte, invocation.Request) error
}

type stdoutPublisher struct{}

func (sp *stdoutPublisher) Publish(ctx context.Context, payload []byte) error {
	_, _ = fmt.Printf("stream.emit %s\n", string(payload))
	return nil
}

type httpStreamPublisher struct {
	url     string
	headers map[string]string
	client  *http.Client
}

func (hp *httpStreamPublisher) Close() error {
	if hp != nil && hp.client != nil {
		hp.client.CloseIdleConnections()
	}
	return nil
}

func (hp *httpStreamPublisher) Publish(ctx context.Context, payload []byte) error {
	return hp.publish(ctx, payload, nil)
}
func (hp *httpStreamPublisher) PublishInvocation(ctx context.Context, payload []byte, request invocation.Request) error {
	return hp.publish(ctx, payload, invocationHeaders(request))
}
func (hp *httpStreamPublisher) publish(ctx context.Context, payload []byte, systemHeaders map[string]string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hp.url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range hp.headers {
		req.Header.Set(key, value)
	}
	for key, value := range systemHeaders {
		req.Header.Set(key, value)
	}
	resp, err := hp.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
		if len(body) > 1<<20 {
			return fmt.Errorf("stream http status %d response exceeds %d bytes", resp.StatusCode, 1<<20)
		}
		return fmt.Errorf("stream http status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

type kafkaStreamPublisher struct {
	writer *kafka.Writer
}

func (kp *kafkaStreamPublisher) Publish(ctx context.Context, payload []byte) error {
	return kp.writer.WriteMessages(ctx, kafka.Message{Value: payload, Time: time.Now()})
}
func (kp *kafkaStreamPublisher) PublishInvocation(ctx context.Context, payload []byte, request invocation.Request) error {
	return kp.writer.WriteMessages(ctx, kafka.Message{Value: payload, Time: time.Now(), Headers: kafkaInvocationHeaders(request)})
}
func kafkaInvocationHeaders(request invocation.Request) []kafka.Header {
	headers := invocationHeaders(request)
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]kafka.Header, 0, len(names))
	for _, name := range names {
		result = append(result, kafka.Header{Key: name, Value: []byte(headers[name])})
	}
	return result
}

func cloneStringAnyMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	return cloneSnapshotValue(input).(map[string]any)
}

func NewStreamExecutor(config map[string]interface{}) (*StreamExecutor, error) {
	descriptor := cloneStringAnyMap(config)
	publisher := "stdout"
	if raw, ok := config["publisher"].(string); ok && raw != "" {
		publisher = raw
	}
	switch strings.ToLower(publisher) {
	case "stdout":
		return &StreamExecutor{
			publisher: &stdoutPublisher{},
			source:    verb.SourceInfo{Type: verb.SourceStream, Detail: "stdout"}, descriptor: descriptor,
		}, nil
	case "http":
		url, _ := config["url"].(string)
		if url == "" {
			return nil, fmt.Errorf("stream http publisher requires url")
		}
		headers := map[string]string{}
		if raw, ok := config["headers"].(map[string]interface{}); ok {
			for k, v := range raw {
				if isReservedInvocationHeader(k) {
					return nil, fmt.Errorf("stream HTTP header %q is reserved", k)
				}
				if s, ok := v.(string); ok {
					headers[k] = s
				}
			}
		}
		policy := OutboundNetworkPolicy{}
		if allowed, ok := config["allowPrivateNetwork"].(bool); ok {
			policy.AllowPrivate = allowed
		}
		if _, err := policy.ValidateURL(url); err != nil {
			return nil, fmt.Errorf("stream http URL: %w", err)
		}
		timeout := 5 * time.Second
		if raw, ok := config["timeout"].(string); ok {
			parsed, err := time.ParseDuration(raw)
			if err != nil || parsed <= 0 {
				return nil, fmt.Errorf("stream http timeout must be positive")
			}
			timeout = parsed
		}
		client := policy.HTTPClient(timeout, headers)
		return &StreamExecutor{
			publisher: &httpStreamPublisher{url: url, headers: headers, client: client},
			source:    verb.SourceInfo{Type: verb.SourceStream, Ref: url, Detail: "http"}, descriptor: descriptor,
		}, nil
	case "kafka":
		var brokers []string
		if raw, ok := config["brokers"].([]interface{}); ok {
			for _, entry := range raw {
				if s, ok := entry.(string); ok {
					brokers = append(brokers, s)
				}
			}
		}
		if raw, ok := config["brokers"].([]string); ok {
			brokers = append(brokers, raw...)
		}
		if len(brokers) == 0 {
			return nil, fmt.Errorf("stream kafka publisher requires brokers")
		}
		topic, _ := config["topic"].(string)
		if topic == "" {
			return nil, fmt.Errorf("stream kafka publisher requires topic")
		}
		detail := "kafka"
		if len(brokers) > 0 {
			detail = fmt.Sprintf("kafka:%s", strings.Join(brokers, ","))
		}
		writer := &kafka.Writer{
			Addr:         kafka.TCP(brokers...),
			Topic:        topic,
			Balancer:     &kafka.LeastBytes{},
			RequiredAcks: kafka.RequireOne,
		}
		return &StreamExecutor{
			publisher: &kafkaStreamPublisher{writer: writer},
			source:    verb.SourceInfo{Type: verb.SourceStream, Ref: topic, Detail: detail}, descriptor: descriptor,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported stream publisher: %s", publisher)
	}
}

func (se *StreamExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return se.publish(ctx, args, nil)
}

func (se *StreamExecutor) Invoke(ctx context.Context, request invocation.Request) invocation.Outcome {
	result, err := se.publish(ctx, request.Arguments, &request)
	if err != nil {
		return invocation.Outcome{Class: invocation.OutcomeUnknown, Err: err}
	}
	return invocation.Outcome{Class: invocation.OutcomeSuccess, Result: result}
}

func (se *StreamExecutor) publish(ctx context.Context, args map[string]interface{}, request *invocation.Request) (interface{}, error) {
	payload, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("marshal args: %w", err)
	}
	if request != nil {
		publisher, ok := se.publisher.(invocationStreamPublisher)
		if !ok {
			return nil, fmt.Errorf("stream publisher does not propagate invocation metadata")
		}
		if err := publisher.PublishInvocation(ctx, payload, *request); err != nil {
			return nil, err
		}
	} else if err := se.publisher.Publish(ctx, payload); err != nil {
		return nil, err
	}
	return map[string]interface{}{"status": "queued"}, nil
}

func (se *StreamExecutor) Close() error {
	if se == nil {
		return nil
	}
	if closer, ok := se.publisher.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func (se *StreamExecutor) InvocationResolverDescriptor() any {
	return map[string]any{"type": "stream", "config": cloneStringAnyMap(se.descriptor), "source_type": se.source.Type, "reference": se.source.Ref, "detail": se.source.Detail}
}

func (se *StreamExecutor) SourceInfo() verb.SourceInfo {
	return se.source
}

// OCIExecutor resolves a verb executor from an OCI extension bundle.
type OCIExecutor struct {
	ref          string
	verbName     string
	verification OCIVerificationPolicy
	verifierPath string
	once         sync.Once
	executor     VerbExecutor
	err          error
}

func NewOCIExecutor(verbName string, config map[string]interface{}) (*OCIExecutor, error) {
	ref, _ := config["ref"].(string)
	if ref == "" {
		return nil, fmt.Errorf("oci target requires ref")
	}
	if raw, ok := config["verb"].(string); ok && raw != "" {
		verbName = raw
	}
	verifierPath, _ := config["signatureVerifier"].(string)
	if strings.TrimSpace(verifierPath) == "" {
		return nil, fmt.Errorf("oci target requires signatureVerifier")
	}
	return &OCIExecutor{ref: ref, verbName: verbName, verifierPath: verifierPath, verification: OCIVerificationPolicy{RequireSignature: true, Verifier: CommandOCISignatureVerifier{Path: verifierPath}}}, nil
}

func (oe *OCIExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if err := oe.resolve(ctx); err != nil {
		return nil, err
	}
	return oe.executor.Execute(ctx, args)
}

func (oe *OCIExecutor) Invoke(ctx context.Context, request invocation.Request) invocation.Outcome {
	if err := oe.resolve(ctx); err != nil {
		return invocation.Outcome{Class: invocation.OutcomePermanentFailure, Err: err}
	}
	aware, ok := any(oe.executor).(invocation.Executor)
	if !ok {
		return invocation.Outcome{Class: invocation.OutcomePermanentFailure, Err: fmt.Errorf("OCI-resolved executor is not invocation-aware")}
	}
	return aware.Invoke(ctx, request)
}

func (oe *OCIExecutor) Warmup(ctx context.Context) error {
	return oe.resolve(ctx)
}

func (oe *OCIExecutor) Close() error {
	oe.once.Do(func() { oe.err = fmt.Errorf("OCI executor was closed before resolution") })
	if closer, ok := oe.executor.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func (oe *OCIExecutor) InvocationResolverDescriptor() any {
	return map[string]any{"type": "oci", "reference": oe.ref, "verb": oe.verbName, "signature_verification": "required", "signature_verifier": oe.verifierPath}
}

func (oe *OCIExecutor) SourceInfo() verb.SourceInfo {
	return verb.SourceInfo{Type: verb.SourceOCI, Ref: oe.ref, Detail: oe.verbName}
}

func (oe *OCIExecutor) resolve(ctx context.Context) error {
	oe.once.Do(func() {
		executors, err := loadOCIBundleExecutors(ctx, oe.ref, oe.verification)
		if err != nil {
			oe.err = err
			return
		}
		executor, ok := executors[oe.verbName]
		if !ok {
			oe.err = fmt.Errorf("verb %s not found in oci bundle", oe.verbName)
			return
		}
		oe.executor = executor
	})
	if oe.err != nil {
		return oe.err
	}
	if oe.executor == nil {
		return fmt.Errorf("oci executor not initialized")
	}
	return nil
}

func loadOCIBundleExecutors(ctx context.Context, ref string, policy OCIVerificationPolicy) (map[string]VerbExecutor, error) {
	loaders, cleanup, err := loadOCIBundleLoaders(ctx, ref, policy)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	target := newCaptureTarget()
	for _, loader := range loaders {
		if err := loader.Load(ctx, target); err != nil {
			return nil, err
		}
	}
	return target.verbs, nil
}

// === Directory Loaders ===

// LoadFromDirectory scans a directory for extension files and creates loaders
func LoadFromDirectory(dirPath string) ([]Loader, error) {
	var loaders []Loader

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		switch {
		case strings.HasSuffix(path, ".verbs.json"):
			name := filepath.Base(path[:len(path)-11]) // Remove .verbs.json
			loaders = append(loaders, NewJSONVerbLoader(name, path))
		case strings.HasSuffix(path, ".schema.json"):
			name := filepath.Base(path[:len(path)-12]) // Remove .schema.json
			loaders = append(loaders, NewJSONSchemaLoader(name, path))
		case filepath.Ext(path) == ".eff" || filepath.Ext(path) == ".effx":
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			relative, relativeErr := filepath.Rel(dirPath, path)
			if relativeErr != nil {
				return relativeErr
			}
			loaders = append(loaders, NewStaticSourceLoader(filepath.Base(path), filepath.ToSlash(relative), data))
		}

		return nil
	})

	return loaders, err
}

// === Helper Functions ===

// LoadExtensionsFromReader loads extensions from an io.Reader (for testing)
func LoadExtensionsFromReader(r io.Reader, extension string) (Loader, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	// Write to temp file and create loader
	tempFile, err := os.CreateTemp("", "*"+extension)
	if err != nil {
		return nil, err
	}
	defer tempFile.Close()

	if _, err := tempFile.Write(data); err != nil {
		return nil, err
	}

	switch extension {
	case ".verbs.json":
		return NewJSONVerbLoader("temp", tempFile.Name()), nil
	case ".schema.json":
		return NewJSONSchemaLoader("temp", tempFile.Name()), nil
	default:
		return nil, fmt.Errorf("unsupported extension: %s", extension)
	}
}

// extractTarLayer extracts a bounded tar stream beneath targetDir.
func extractTarLayer(r io.Reader, targetDir string) error {
	if err := safetar.Extract(r, targetDir, safetar.DefaultLimits()); err != nil {
		return fmt.Errorf("extracting safe tar layer: %w", err)
	}
	return nil
}
